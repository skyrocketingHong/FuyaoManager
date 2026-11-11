package httpapi

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// exchangeRateLimit bounds code-redemption attempts per client address. The
// code itself carries 80 bits of entropy and a single-use transactional
// consume; this limiter adds per-source pacing so a compromised client inside
// the allowed network cannot hammer the endpoint.
const (
	exchangeWindow  = time.Minute
	exchangeMaxHits = 10
)

type exchangeLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newExchangeLimiter() *exchangeLimiter {
	return &exchangeLimiter{hits: make(map[string][]time.Time)}
}

// allow records one attempt for the address and reports whether it fits the
// window. The address is the transport peer (loopback or a private-network
// client), never a forwarded header.
func (l *exchangeLimiter) allow(addr string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.hits[addr][:0]
	for _, t := range l.hits[addr] {
		if now.Sub(t) < exchangeWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= exchangeMaxHits {
		l.hits[addr] = recent
		return false
	}
	recent = append(recent, now)
	l.hits[addr] = recent
	if len(l.hits) > 4096 {
		// Bounds the map when addresses churn; the window filter above keeps
		// individual entries short-lived.
		l.hits = make(map[string][]time.Time)
		l.hits[addr] = recent
	}
	return true
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleExchangeCode redeems a CLI-issued one-time code (initialization or
// login) for a fresh session. Codes are hash-stored, expire quickly, burn
// attempts on every presentation, and are consumed transactionally, so a
// concurrent double redemption can never both succeed. The response matches
// the passkey ceremony-done shape: the signed-in user plus the CSRF token.
func (a *API) handleExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req, false) {
		return
	}
	if !a.exchangeLimiter.allow(clientAddr(r)) {
		writeProblem(w, http.StatusTooManyRequests, "rate_limited", "Too many code attempts; wait a minute and try again.")
		return
	}

	result, err := a.store.ExchangeCode(r.Context(), req.Code, a.cfg.SessionTTL)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCodeInvalid):
			writeProblem(w, http.StatusBadRequest, "invalid_code", "The code is unknown.")
		case errors.Is(err, store.ErrCodeUsed):
			writeProblem(w, http.StatusConflict, "code_used", "The code was already used; request a fresh one from the server CLI.")
		case errors.Is(err, store.ErrCodeExpired):
			writeProblem(w, http.StatusConflict, "code_expired", "The code expired; request a fresh one from the server CLI.")
		case errors.Is(err, store.ErrCodeLocked):
			writeProblem(w, http.StatusConflict, "code_locked", "The code was presented too many times and is locked; request a fresh one.")
		case errors.Is(err, store.ErrCodePurpose):
			writeProblem(w, http.StatusConflict, "code_purpose_mismatch", "The code does not match the account state; use an initialization code only for the pending first administrator and login codes for active accounts.")
		case errors.Is(err, store.ErrBootstrapClosed):
			writeProblem(w, http.StatusConflict, "bootstrap_closed", "An administrator is already registered; initialization is closed.")
		default:
			a.logger.Error("exchange code", "error", err)
			writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to redeem the code.")
		}
		return
	}

	// Session rotation: every login (code or passkey) mints a brand-new
	// session; prior cookies of other sessions are unaffected but this
	// response always carries a fresh token.
	a.setSessionCookie(w, result.CookieToken)
	a.clearAuthTxCookie(w)
	a.store.AuthCodes.PurgeExpired(r.Context())
	writeJSON(w, http.StatusOK, ceremonyDone{User: toUserDTO(result.User), CSRFToken: result.CSRFToken})
}
