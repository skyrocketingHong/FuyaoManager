package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/wa"
)

// Session-bound passkey binding. Unlike the legacy first-administrator
// permit ceremony, these endpoints derive the binding target exclusively
// from the authenticated session: the request body can never name a
// different account. A restricted registration session (issued when a code
// confirmed the identity of an account without a credential for the current
// relying party) may call exactly these endpoints, the session read, and
// logout — nothing else.

// handleBindOptions starts a registration ceremony for the session's own
// account. The challenge is stored bound to both the user and the exact
// session, so a challenge cannot be replayed against another session.
func (a *API) handleBindOptions(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	var req struct{}
	if !decodeJSON(w, r, &req, true) {
		return
	}
	passkeys, err := a.store.Passkeys.ListByUser(r.Context(), auth.User.ID)
	if err != nil {
		a.logger.Error("list passkeys", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	credentials := make([]webauthnlib.Credential, 0, len(passkeys))
	for _, pk := range passkeys {
		credentials = append(credentials, wa.ToCredential(pk))
	}
	waUser := &wa.User{Profile: auth.User, Credentials: credentials}

	a.store.AuthStates.PurgeExpired(r.Context())
	creation, sessionData, err := a.wa.BeginRegistration(waUser)
	if err != nil {
		a.logger.Error("begin passkey binding", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	data, err := json.Marshal(sessionData)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	userID := auth.User.ID
	sessionID := auth.Session.ID
	stateID, err := a.store.AuthStates.Create(r.Context(), "bind", &userID, nil, &sessionID, data, a.cfg.ChallengeTTL)
	if err != nil {
		a.logger.Error("store auth state", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	a.setAuthTxCookie(w, stateID)
	writeJSON(w, http.StatusOK, creation)
}

// handleBindVerify verifies the attestation and, in one transaction, stores
// the credential tagged with the current relying-party ID, mints the full
// session, and revokes the session that carried the ceremony. A cancelled or
// rejected ceremony consumes only the challenge: the restricted session
// stays alive and the user may request fresh options and retry while it is
// valid.
func (a *API) handleBindVerify(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if !readStrictBody(w, r) {
		return
	}
	state, ok := a.consumeAuthState(w, r, "bind")
	if !ok {
		return
	}
	if state.UserID == nil || state.SessionID == nil ||
		*state.UserID != auth.User.ID || *state.SessionID != auth.Session.ID {
		// A challenge minted for another account or session must never
		// complete here; report the indistinguishable challenge failure.
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}

	passkeys, err := a.store.Passkeys.ListByUser(r.Context(), auth.User.ID)
	if err != nil {
		a.logger.Error("list passkeys", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete registration.")
		return
	}
	credentials := make([]webauthnlib.Credential, 0, len(passkeys))
	for _, pk := range passkeys {
		credentials = append(credentials, wa.ToCredential(pk))
	}

	var sessionData webauthnlib.SessionData
	if err := json.Unmarshal(state.Data, &sessionData); err != nil {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}
	credential, err := a.wa.FinishRegistration(&wa.User{Profile: auth.User, Credentials: credentials}, sessionData, r)
	if err != nil {
		a.logger.Warn("passkey binding rejected", "error", err)
		writeProblem(w, http.StatusUnauthorized, "credential_rejected", "The passkey registration response failed verification.")
		return
	}

	pk := wa.FromCredential(auth.User.ID, credential)
	pk.RPID = a.cfg.RPID
	result, err := a.store.CompletePasskeyBind(r.Context(), auth.Session.ID, auth.User.ID, pk, a.cfg.SessionTTL)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrDuplicateCredential):
		writeProblem(w, http.StatusConflict, "credential_duplicate", "This passkey is already registered.")
		return
	case errors.Is(err, store.ErrSessionNotEligible):
		writeProblem(w, http.StatusConflict, "session_rotated", "The session that requested this registration is no longer active; sign in again to retry.")
		return
	default:
		a.logger.Error("complete passkey binding", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete registration.")
		return
	}

	a.setSessionCookie(w, result.CookieToken, a.cfg.SessionTTL)
	a.clearAuthTxCookie(w)
	writeJSON(w, http.StatusOK, ceremonyDone{
		User:         toUserDTO(result.User),
		CSRFToken:    result.CSRFToken,
		SessionScope: model.SessionKindFull,
		PasskeyBound: true,
	})
}
