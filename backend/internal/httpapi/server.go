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
	RPID             string
	RPOrigins        []string
	CookieSecure     bool
	SessionTTL       time.Duration
	ChallengeTTL     time.Duration
	ThemeContracts   ThemeConfigValidator
	BuildEnqueuer    BuildEnqueuer
	Previews         PreviewIssuer
	Publisher        Publisher
	PublishedOrigins PublishedOrigins
}

// API wires the stores, WebAuthn instance, and HTTP handlers.
type API struct {
	store            *store.Store
	wa               *webauthnlib.WebAuthn
	cfg              Config
	logger           *slog.Logger
	origins          map[string]struct{}
	themeContracts   ThemeConfigValidator
	buildEnqueuer    BuildEnqueuer
	previews         PreviewIssuer
	publisher        Publisher
	publishedOrigins PublishedOrigins
}

// routeSpec is one registered management route with the OpenAPI operation it
// implements. The table is the single Go-side source of truth: registration
// loops over it and contract tests compare it against
// contracts/openapi/management.yaml.
type routeSpec struct {
	Method      string
	Pattern     string
	OperationID string
	Handler     http.HandlerFunc
}

// routes returns every management route. Middleware per route class:
// WebAuthn ceremonies carry their own permit/challenge protection; mutating
// authenticated routes additionally require the CSRF header.
func (a *API) routes() []routeSpec {
	return []routeSpec{
		// WebAuthn ceremonies (no prior session; protected by permit,
		// challenge, and Origin checks).
		{http.MethodPost, "/api/v1/auth/register/options", "startPasskeyEnrollment", a.handleRegisterOptions},
		{http.MethodPost, "/api/v1/auth/register/verify", "verifyPasskeyEnrollment", a.handleRegisterVerify},
		{http.MethodPost, "/api/v1/auth/login/options", "startPasskeyLogin", a.handleLoginOptions},
		{http.MethodPost, "/api/v1/auth/login/verify", "verifyPasskeyLogin", a.handleLoginVerify},

		{http.MethodGet, "/api/v1/session", "getSession", a.handleSession},
		{http.MethodPost, "/api/v1/auth/logout", "logout", a.requireAuth(a.withCSRF(a.handleLogout))},

		// Sites.
		{http.MethodGet, "/api/v1/sites", "listSites", a.requireAuth(a.handleListSites)},
		{http.MethodGet, "/api/v1/sites/{siteId}", "getSite", a.requireAuth(a.handleGetSite)},
		{http.MethodGet, "/api/v1/sites/{siteId}/settings", "getSiteSettings", a.requireAuth(a.handleGetSettings)},
		{http.MethodPatch, "/api/v1/sites/{siteId}/settings", "updateSiteSettings", a.requireAuth(a.withCSRF(a.handlePatchSettings))},

		// Theme catalog and per-site theme selection. Registration itself is
		// operator-driven (fuyao-admin register-theme); the HTTP surface only
		// reads the catalog and saves selections.
		{http.MethodGet, "/api/v1/themes", "listThemes", a.requireAuth(a.handleListThemes)},
		{http.MethodGet, "/api/v1/sites/{siteId}/theme", "getSiteTheme", a.requireAuth(a.handleGetSiteTheme)},
		{http.MethodPut, "/api/v1/sites/{siteId}/theme", "putSiteTheme", a.requireAuth(a.withCSRF(a.handlePutSiteTheme))},

		// Build-preview tasks: creation freezes the version view; the runner
		// executes and persists state transitions that stay queryable here.
		{http.MethodPost, "/api/v1/sites/{siteId}/builds", "createBuild", a.requireAuth(a.withCSRF(a.handleCreateBuild))},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds", "listBuilds", a.requireAuth(a.handleListBuilds)},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds/{buildId}", "getBuild", a.requireAuth(a.handleGetBuild)},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-links", "createPreviewLink", a.requireAuth(a.withCSRF(a.handleCreatePreviewLink))},

		// Releases: publish freezes a succeeded build's artifacts into a
		// release directory and atomically switches the site's live version;
		// rollback re-points at an existing release without rebuilding.
		{http.MethodPost, "/api/v1/sites/{siteId}/releases", "createRelease", a.requireAuth(a.withCSRF(a.handleCreateRelease))},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases", "listReleases", a.requireAuth(a.handleListReleases)},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases/{releaseId}", "getRelease", a.requireAuth(a.handleGetRelease)},
		{http.MethodGet, "/api/v1/sites/{siteId}/release", "getCurrentRelease", a.requireAuth(a.handleGetCurrentRelease)},
		{http.MethodPost, "/api/v1/sites/{siteId}/releases/{releaseId}/rollback", "rollbackRelease", a.requireAuth(a.withCSRF(a.handleRollbackRelease))},
	}
}

// New assembles the handler with its middleware chain.
func New(st *store.Store, wa *webauthnlib.WebAuthn, cfg Config, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	api := &API{
		store:            st,
		wa:               wa,
		cfg:              cfg,
		logger:           logger,
		themeContracts:   cfg.ThemeContracts,
		buildEnqueuer:    cfg.BuildEnqueuer,
		previews:         cfg.Previews,
		publisher:        cfg.Publisher,
		publishedOrigins: cfg.PublishedOrigins,
	}
	api.origins = make(map[string]struct{}, len(cfg.RPOrigins))
	for _, o := range cfg.RPOrigins {
		api.origins[o] = struct{}{}
	}

	mux := http.NewServeMux()
	for _, route := range api.routes() {
		mux.HandleFunc(route.Method+" "+route.Pattern, route.Handler)
	}

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
