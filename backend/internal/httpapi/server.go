// Package httpapi implements the management HTTP API: WebAuthn ceremonies,
// cookie sessions with CSRF/Origin enforcement, and site settings with
// optimistic concurrency.
package httpapi

import (
	"context"
	"crypto/sha256"
	"io"

	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
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
	RegistrationTTL  time.Duration
	ThemeContracts   ThemeConfigValidator
	BuildEnqueuer    BuildEnqueuer
	Previews         PreviewIssuer
	Publisher        Publisher
	PublishedOrigins PublishedOrigins
	Provisioner      SiteProvisioner
	Edge             EdgeManager
	Content          ContentStore
	MediaTasks       *store.MediaTaskStore
	ReleasesRoot     string
	// DeploymentEnabled gates the publish, rollback, visibility, and edge
	// reapply APIs: false (the run-isolation default) answers them with
	// 503 deployment_disabled, while reads keep working.
	DeploymentEnabled bool
	// PortMin/PortMax bound the platform port allocation used at site
	// provisioning; they mirror the deployment-wide range.
	PortMin int
	PortMax int
}

var _ = edge.State{} // keep the edge import next to the interface above

// SiteProvisioner provisions the per-site on-disk tree at site creation.
// baseURL is the site's real managed entry used to seed site.yaml.
type SiteProvisioner interface {
	Provision(site model.Site, settings model.SiteSettings, baseURL string) error
}

// EdgeManager renders published-site entry URLs and re-applies the managed
// reverse-proxy configuration. edge.Manager implements it.
type EdgeManager interface {
	EntryURL(visibility string, port int) string
	Reconcile(ctx context.Context) error
	// State reports the desired-vs-applied health of the managed entries.
	State(ctx context.Context) edge.State
}

// ContentStore is the single authority for per-site editable content that
// the frozen-snapshot builds consume. content.Store implements it; nil
// disables the content endpoints (503).
type ContentStore interface {
	ListPosts(siteID uuid.UUID, query string, page, pageSize int) ([]content.Post, int, error)
	GetPost(siteID uuid.UUID, rel string) (content.Post, error)
	CreatePost(siteID uuid.UUID, post content.Post) (content.Post, error)
	SavePost(siteID uuid.UUID, post content.Post, expectedDigest string) (content.Post, error)
	DeletePost(siteID uuid.UUID, rel, expectedDigest string) error
	GetFriends(siteID uuid.UUID) (content.FriendsDoc, string, error)
	SaveFriends(siteID uuid.UUID, doc content.FriendsDoc, expectedDigest string) (content.FriendsDoc, string, error)
	GetSocialLinks(siteID uuid.UUID) ([]content.SocialLink, string, error)
	SaveSocialLinks(siteID uuid.UUID, links []content.SocialLink, expectedDigest string) ([]content.SocialLink, string, error)
	GetSiteConfig(siteID uuid.UUID) (doc content.SiteConfigDoc, securitySet bool, digest string, err error)
	SaveSiteConfig(siteID uuid.UUID, doc content.SiteConfigDoc, securityCode *string, expectedDigest string) (content.SiteConfigDoc, string, error)
	UpdateSiteBaseURL(siteID uuid.UUID, url string) error
	GetAlbumIndex(siteID uuid.UUID) (content.AlbumIndex, string, error)
	GetAlbumYear(siteID uuid.UUID, year string) (content.AlbumYear, string, error)
	SaveAlbumYear(siteID uuid.UUID, year string, expectedDigest string, edit func(*content.AlbumYear) error) (content.AlbumYear, string, error)
	StoreUpload(siteID uuid.UUID, reader io.Reader, sizeHint int64) (string, error)
	DeleteAlbumPhoto(siteID uuid.UUID, year, filename string, releaseJSONs [][]byte, expectedDigest string) error
	DetachAlbumPhoto(siteID uuid.UUID, year, filename string, expectedDigest string) error
	GetFootprints(siteID uuid.UUID) (content.FootprintsDoc, string, error)
	SaveFootprints(siteID uuid.UUID, doc content.FootprintsDoc, expectedDigest string) (content.FootprintsDoc, string, error)
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
	provisioner      SiteProvisioner
	edge             EdgeManager
	content          ContentStore
	mediaTasks       *store.MediaTaskStore
	releasesRoot     string
	deployEnabled    bool
	portMin          int
	portMax          int
	exchangeLimiter  *exchangeLimiter
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
		// challenge, and Origin checks). On IP-literal deployments the RP ID
		// cannot be a domain string, so the relying party is unavailable and
		// these routes answer 503; one-time-code login is unaffected.
		{http.MethodPost, "/api/v1/auth/register/options", "startPasskeyEnrollment", a.passkeyRoute(a.handleRegisterOptions)},
		{http.MethodPost, "/api/v1/auth/register/verify", "verifyPasskeyEnrollment", a.passkeyRoute(a.handleRegisterVerify)},
		{http.MethodPost, "/api/v1/auth/login/options", "startPasskeyLogin", a.passkeyRoute(a.handleLoginOptions)},
		{http.MethodPost, "/api/v1/auth/login/verify", "verifyPasskeyLogin", a.passkeyRoute(a.handleLoginVerify)},

		// One-time CLI-issued code redemption (initialization and recovery).
		{http.MethodPost, "/api/v1/auth/exchange", "exchangeAuthCode", a.handleExchangeCode},

		// Session-bound passkey binding: the only registration path once the
		// platform has an active administrator. The restricted registration
		// session (issued when the account has no credential for the current
		// RP) may drive exactly these two endpoints plus session read/logout.
		{http.MethodPost, "/api/v1/auth/passkey/register/options", "startPasskeyBinding", a.passkeyRoute(a.requireAuth(a.withCSRF(a.handleBindOptions)))},
		{http.MethodPost, "/api/v1/auth/passkey/register/verify", "verifyPasskeyBinding", a.passkeyRoute(a.requireAuth(a.withCSRF(a.handleBindVerify)))},

		{http.MethodGet, "/api/v1/session", "getSession", a.handleSession},
		{http.MethodPost, "/api/v1/auth/logout", "logout", a.requireAuth(a.withCSRF(a.handleLogout))},

		// Sites.
		{http.MethodGet, "/api/v1/sites", "listSites", a.requireFullScope(a.handleListSites)},
		{http.MethodPost, "/api/v1/sites", "createSite", a.requireFullScope(a.withCSRF(a.handleCreateSite))},
		{http.MethodGet, "/api/v1/sites/{siteId}", "getSite", a.requireFullScope(a.handleGetSite)},
		{http.MethodPost, "/api/v1/sites/{siteId}/provision", "provisionSite", a.requireFullScope(a.withCSRF(a.handleProvisionSite))},
		{http.MethodPut, "/api/v1/sites/{siteId}/visibility", "putSiteVisibility", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handlePutVisibility)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/settings", "getSiteSettings", a.requireFullScope(a.handleGetSettings)},
		{http.MethodPatch, "/api/v1/sites/{siteId}/settings", "updateSiteSettings", a.requireFullScope(a.withCSRF(a.handlePatchSettings))},

		// Theme catalog and per-site theme selection. Registration itself is
		// operator-driven (fuyao-admin register-theme); the HTTP surface only
		// reads the catalog and saves selections.
		{http.MethodGet, "/api/v1/themes", "listThemes", a.requireFullScope(a.handleListThemes)},
		{http.MethodGet, "/api/v1/sites/{siteId}/theme", "getSiteTheme", a.requireFullScope(a.handleGetSiteTheme)},
		{http.MethodPut, "/api/v1/sites/{siteId}/theme", "putSiteTheme", a.requireFullScope(a.withCSRF(a.handlePutSiteTheme))},

		// Per-site content (single authority for posts, friends, social
		// links, and the visual site configuration the builds consume).
		{http.MethodGet, "/api/v1/sites/{siteId}/posts", "listPosts", a.requireFullScope(a.contentRoute(a.handleListPosts))},
		{http.MethodPost, "/api/v1/sites/{siteId}/posts", "createPost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleCreatePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/{postPath}", "getPost", a.requireFullScope(a.contentRoute(a.handleGetPost))},
		{http.MethodPut, "/api/v1/sites/{siteId}/posts/{postPath}", "putPost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutPost)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/posts/{postPath}", "deletePost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleDeletePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/friends", "getFriends", a.requireFullScope(a.contentRoute(a.handleGetFriends))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/friends", "putFriends", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutFriends)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/social-links", "getSocialLinks", a.requireFullScope(a.contentRoute(a.handleGetSocialLinks))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/social-links", "putSocialLinks", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutSocialLinks)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/edge", "getSiteEdge", a.requireFullScope(a.handleGetSiteEdge)},
		{http.MethodPost, "/api/v1/sites/{siteId}/edge/reapply", "reapplySiteEdge", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleReapplySiteEdge)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/site-config", "getSiteConfig", a.requireFullScope(a.contentRoute(a.handleGetSiteConfig))},
		{http.MethodPut, "/api/v1/sites/{siteId}/site-config", "putSiteConfig", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutSiteConfig)))},

		// Albums and footprints: uploads with async processing tasks,
		// metadata editing, reference-checked deletion, and the footprint
		// point document.
		{http.MethodGet, "/api/v1/sites/{siteId}/albums", "getAlbums", a.requireFullScope(a.contentRoute(a.handleGetAlbums))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/photos", "uploadAlbumPhoto", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleUploadAlbumPhoto)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/process", "processAlbums", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleEnqueueAlbumTask)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/tasks", "listMediaTasks", a.requireFullScope(a.contentRoute(a.handleListMediaTasks))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/{year}", "getAlbumYear", a.requireFullScope(a.contentRoute(a.handleGetAlbumYear))},
		{http.MethodPut, "/api/v1/sites/{siteId}/albums/{year}", "putAlbumYear", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutAlbumYear)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/albums/{year}/{filename}", "deleteAlbumPhoto", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleDeleteAlbumPhoto)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/footprints", "getFootprints", a.requireFullScope(a.contentRoute(a.handleGetFootprints))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/footprints", "putFootprints", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutFootprints)))},

		// Build-preview tasks: creation freezes the version view; the runner
		// executes and persists state transitions that stay queryable here.
		{http.MethodPost, "/api/v1/sites/{siteId}/builds", "createBuild", a.requireFullScope(a.withCSRF(a.handleCreateBuild))},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds", "listBuilds", a.requireFullScope(a.handleListBuilds)},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds/{buildId}", "getBuild", a.requireFullScope(a.handleGetBuild)},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-links", "createPreviewLink", a.requireFullScope(a.withCSRF(a.handleCreatePreviewLink))},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-close", "closePreviewLink", a.requireFullScope(a.withCSRF(a.handleClosePreviewLink))},

		// Releases: publish freezes a succeeded build's artifacts into a
		// release directory and atomically switches the site's live version;
		// rollback re-points at an existing release without rebuilding.
		{http.MethodPost, "/api/v1/sites/{siteId}/releases", "createRelease", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleCreateRelease)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases", "listReleases", a.requireFullScope(a.handleListReleases)},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases/{releaseId}", "getRelease", a.requireFullScope(a.handleGetRelease)},
		{http.MethodGet, "/api/v1/sites/{siteId}/release", "getCurrentRelease", a.requireFullScope(a.handleGetCurrentRelease)},
		{http.MethodPost, "/api/v1/sites/{siteId}/releases/{releaseId}/rollback", "rollbackRelease", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleRollbackRelease)))},
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
		provisioner:      cfg.Provisioner,
		edge:             cfg.Edge,
		content:          cfg.Content,
		mediaTasks:       cfg.MediaTasks,
		releasesRoot:     cfg.ReleasesRoot,
		deployEnabled:    cfg.DeploymentEnabled,
		portMin:          cfg.PortMin,
		portMax:          cfg.PortMax,
		exchangeLimiter:  newExchangeLimiter(),
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

// contentRoute gates the content endpoints on the content store being
// configured.
func (a *API) contentRoute(h authedHandler) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		if a.content == nil {
			writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "The content store is not configured on this server.")
			return
		}
		h(w, r, auth)
	}
}

// deploymentRoute gates the deployment-mutating endpoints (publish, rollback,
// visibility, edge reapply) on the deployment switch. In run isolation the
// response names deployment_disabled explicitly; reads stay available.
func (a *API) deploymentRoute(h authedHandler) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		if !a.deployEnabled {
			writeProblem(w, http.StatusServiceUnavailable, "deployment_disabled",
				"Site deployment is disabled on this server: publishing, rollback, visibility changes, and edge applies are inactive. Enable the deployment switch to use this endpoint.")
			return
		}
		h(w, r, auth)
	}
}

// passkeyRoute gates the legacy passkey ceremonies on the WebAuthn relying
// party being configured (domain-string RP ID).
func (a *API) passkeyRoute(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.wa == nil {
			writeProblem(w, http.StatusServiceUnavailable, "passkey_unavailable", "Passkey authentication requires a domain-string relying-party ID; this deployment uses one-time-code login.")
			return
		}
		h(w, r)
	}
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

// requireAuth authenticates the session cookie or rejects with 401. Any live
// session passes, including a restricted registration session; business
// routes must use requireFullScope instead.
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

// requireFullScope authenticates and additionally requires a full session:
// restricted registration sessions only prove the account identity and may
// not reach business APIs.
func (a *API) requireFullScope(h authedHandler) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		if auth.Session.Kind != model.SessionKindFull {
			writeProblem(w, http.StatusForbidden, "passkey_binding_required", "This account must bind a passkey for the current relying party before using the management APIs.")
			return
		}
		h(w, r, auth)
	})
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

// setSessionCookie writes the session cookie; maxAge tracks the actual
// session lifetime so a short restricted registration session does not leave
// a long-lived cookie behind. Secure is omitted only in the all-loopback
// local development configuration; SameSite=Lax is sufficient because the
// admin front end and API share the registrable site.
func (a *API) setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
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
