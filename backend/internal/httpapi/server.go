// Package httpapi implements the management HTTP API: WebAuthn ceremonies,
// cookie sessions with CSRF/Origin enforcement, and site settings with
// optimistic concurrency.
package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// Cookie and header names.
const (
	sessionCookieName = "fuyao_admin_session"
	authTxCookieName  = "fuyao_admin_auth_tx"
	authTxCookiePath  = "/api/v1/auth"
	csrfHeaderName    = "X-CSRF-Token"
)

// Config carries the HTTP-facing runtime configuration.
type Config struct {
	RPID           string
	RPOrigins      []string
	CookieSecure   bool
	SessionTTL     time.Duration
	ChallengeTTL   time.Duration
	ThemeContracts ThemeConfigValidator
	BuildEnqueuer  BuildEnqueuer
	Previews       PreviewIssuer
}

// API wires the stores, WebAuthn instance, and HTTP handlers.
type API struct {
	store          *store.Store
	wa             *webauthnlib.WebAuthn
	cfg            Config
	logger         *slog.Logger
	origins        map[string]struct{}
	themeContracts ThemeConfigValidator
	buildEnqueuer  BuildEnqueuer
	previews       PreviewIssuer
}

// New assembles the handler with its middleware chain.
func New(st *store.Store, wa *webauthnlib.WebAuthn, cfg Config, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	api := &API{
		store:          st,
		wa:             wa,
		cfg:            cfg,
		logger:         logger,
		themeContracts: cfg.ThemeContracts,
		buildEnqueuer:  cfg.BuildEnqueuer,
		previews:       cfg.Previews,
	}
	api.origins = make(map[string]struct{}, len(cfg.RPOrigins))
	for _, o := range cfg.RPOrigins {
		api.origins[o] = struct{}{}
	}

	mux := http.NewServeMux()
	// WebAuthn ceremonies (no prior session; protected by permit, challenge,
	// and Origin checks).
	mux.HandleFunc("POST /api/v1/auth/register/options", api.handleRegisterOptions)
	mux.HandleFunc("POST /api/v1/auth/register/verify", api.handleRegisterVerify)
	mux.HandleFunc("POST /api/v1/auth/login/options", api.handleLoginOptions)
	mux.HandleFunc("POST /api/v1/auth/login/verify", api.handleLoginVerify)

	mux.HandleFunc("GET /api/v1/session", api.handleSession)
	mux.HandleFunc("POST /api/v1/auth/logout", api.requireAuth(api.withCSRF(api.handleLogout)))

	// Sites.
	mux.HandleFunc("GET /api/v1/sites", api.requireAuth(api.handleListSites))
	mux.HandleFunc("GET /api/v1/sites/{siteId}", api.requireAuth(api.handleGetSite))
	mux.HandleFunc("GET /api/v1/sites/{siteId}/settings", api.requireAuth(api.handleGetSettings))
	mux.HandleFunc("PATCH /api/v1/sites/{siteId}/settings", api.requireAuth(api.withCSRF(api.handlePatchSettings)))

	// Theme catalog and per-site theme selection. Registration itself is
	// operator-driven (fuyao-admin register-theme); the HTTP surface only
	// reads the catalog and saves selections.
	mux.HandleFunc("GET /api/v1/themes", api.requireAuth(api.handleListThemes))
	mux.HandleFunc("GET /api/v1/sites/{siteId}/theme", api.requireAuth(api.handleGetSiteTheme))
	mux.HandleFunc("PUT /api/v1/sites/{siteId}/theme", api.requireAuth(api.withCSRF(api.handlePutSiteTheme)))

	// Build-preview tasks: creation freezes the version view; the runner
	// executes and persists state transitions that stay queryable here.
	mux.HandleFunc("POST /api/v1/sites/{siteId}/builds", api.requireAuth(api.withCSRF(api.handleCreateBuild)))
	mux.HandleFunc("GET /api/v1/sites/{siteId}/builds", api.requireAuth(api.handleListBuilds))
	mux.HandleFunc("GET /api/v1/sites/{siteId}/builds/{buildId}", api.requireAuth(api.handleGetBuild))
	mux.HandleFunc("POST /api/v1/sites/{siteId}/builds/{buildId}/preview-links", api.requireAuth(api.withCSRF(api.handleCreatePreviewLink)))

	var h http.Handler = mux
	h = api.originGuard(h)
	h = api.cors(h)
	h = api.requestLogger(h)
	h = api.recoverer(h)
	h = api.securityHeaders(h)
	return h
}

// securityHeaders stamps every management API response — success, auth
// failure, or any error — as non-cacheable, and stops MIME sniffing on the
// JSON bodies. Deliberately minimal for the local HTTP topology: no HSTS
// (would break plain-loopback access) and no CSP (no HTML is served).
func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// authedHandler receives the resolved session and user.
type authedHandler func(w http.ResponseWriter, r *http.Request, auth store.AuthSession)

// requireAuth authenticates the session cookie or rejects with 401.
func (a *API) requireAuth(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeProblem(w, http.StatusUnauthorized, "unauthenticated", "A valid session cookie is required.")
			return
		}
		tokenHash := sha256Sum64(cookie.Value)
		auth, err := a.store.Sessions.GetAndTouch(r.Context(), tokenHash)
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "unauthenticated", "The session is invalid or expired.")
			return
		}
		h(w, r, auth)
	}
}

// withCSRF validates the X-CSRF-Token header against the session's stable
// stored token. It must wrap a handler already authenticated by requireAuth.
func (a *API) withCSRF(h authedHandler) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		token := r.Header.Get(csrfHeaderName)
		ok := token != "" &&
			subtle.ConstantTimeCompare([]byte(token), []byte(auth.Session.CSRFToken)) == 1
		if !ok {
			writeProblem(w, http.StatusForbidden, "csrf_token_invalid", "A valid X-CSRF-Token header is required for this request.")
			return
		}
		h(w, r, auth)
	}
}

func sha256Sum64(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// setSessionCookie writes the session cookie. Secure is omitted only in the
// all-loopback local development configuration; SameSite=Lax is sufficient
// because the admin front end and API share the localhost registrable site.
func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (a *API) setAuthTxCookie(w http.ResponseWriter, id uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     authTxCookieName,
		Value:    id.String(),
		Path:     authTxCookiePath,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.cfg.ChallengeTTL.Seconds()),
	})
}

func (a *API) clearAuthTxCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authTxCookieName,
		Value:    "",
		Path:     authTxCookiePath,
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// userDTO is the public representation of an administrator.
type userDTO struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

func toUserDTO(u model.User) userDTO {
	return userDTO{ID: u.ID.String(), Username: u.Username, DisplayName: u.DisplayName}
}
