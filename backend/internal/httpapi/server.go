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

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/content"
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
	Provisioner      SiteProvisioner
	Edge             EdgeManager
	Content          ContentStore
	MediaTasks       *store.MediaTaskStore
	ReleasesRoot     string
}

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

		// One-time CLI-issued code redemption (initialization and per-login).
		{http.MethodPost, "/api/v1/auth/exchange", "exchangeAuthCode", a.handleExchangeCode},

		{http.MethodGet, "/api/v1/session", "getSession", a.handleSession},
		{http.MethodPost, "/api/v1/auth/logout", "logout", a.requireAuth(a.withCSRF(a.handleLogout))},

		// Sites.
		{http.MethodGet, "/api/v1/sites", "listSites", a.requireAuth(a.handleListSites)},
		{http.MethodPost, "/api/v1/sites", "createSite", a.requireAuth(a.withCSRF(a.handleCreateSite))},
		{http.MethodGet, "/api/v1/sites/{siteId}", "getSite", a.requireAuth(a.handleGetSite)},
		{http.MethodPost, "/api/v1/sites/{siteId}/provision", "provisionSite", a.requireAuth(a.withCSRF(a.handleProvisionSite))},
		{http.MethodPut, "/api/v1/sites/{siteId}/visibility", "putSiteVisibility", a.requireAuth(a.withCSRF(a.handlePutVisibility))},
		{http.MethodGet, "/api/v1/sites/{siteId}/settings", "getSiteSettings", a.requireAuth(a.handleGetSettings)},
		{http.MethodPatch, "/api/v1/sites/{siteId}/settings", "updateSiteSettings", a.requireAuth(a.withCSRF(a.handlePatchSettings))},

		// Theme catalog and per-site theme selection. Registration itself is
		// operator-driven (fuyao-admin register-theme); the HTTP surface only
		// reads the catalog and saves selections.
		{http.MethodGet, "/api/v1/themes", "listThemes", a.requireAuth(a.handleListThemes)},
		{http.MethodGet, "/api/v1/sites/{siteId}/theme", "getSiteTheme", a.requireAuth(a.handleGetSiteTheme)},
		{http.MethodPut, "/api/v1/sites/{siteId}/theme", "putSiteTheme", a.requireAuth(a.withCSRF(a.handlePutSiteTheme))},

		// Per-site content (single authority for posts, friends, social
		// links, and the visual site configuration the builds consume).
		{http.MethodGet, "/api/v1/sites/{siteId}/posts", "listPosts", a.requireAuth(a.contentRoute(a.handleListPosts))},
		{http.MethodPost, "/api/v1/sites/{siteId}/posts", "createPost", a.requireAuth(a.withCSRF(a.contentRoute(a.handleCreatePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/{postPath}", "getPost", a.requireAuth(a.contentRoute(a.handleGetPost))},
		{http.MethodPut, "/api/v1/sites/{siteId}/posts/{postPath}", "putPost", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutPost)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/posts/{postPath}", "deletePost", a.requireAuth(a.withCSRF(a.contentRoute(a.handleDeletePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/friends", "getFriends", a.requireAuth(a.contentRoute(a.handleGetFriends))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/friends", "putFriends", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutFriends)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/social-links", "getSocialLinks", a.requireAuth(a.contentRoute(a.handleGetSocialLinks))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/social-links", "putSocialLinks", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutSocialLinks)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/site-config", "getSiteConfig", a.requireAuth(a.contentRoute(a.handleGetSiteConfig))},
		{http.MethodPut, "/api/v1/sites/{siteId}/site-config", "putSiteConfig", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutSiteConfig)))},

		// Albums and footprints: uploads with async processing tasks,
		// metadata editing, reference-checked deletion, and the footprint
		// point document.
		{http.MethodGet, "/api/v1/sites/{siteId}/albums", "getAlbums", a.requireAuth(a.contentRoute(a.handleGetAlbums))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/photos", "uploadAlbumPhoto", a.requireAuth(a.withCSRF(a.contentRoute(a.handleUploadAlbumPhoto)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/process", "processAlbums", a.requireAuth(a.withCSRF(a.contentRoute(a.handleEnqueueAlbumTask)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/tasks", "listMediaTasks", a.requireAuth(a.contentRoute(a.handleListMediaTasks))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/{year}", "getAlbumYear", a.requireAuth(a.contentRoute(a.handleGetAlbumYear))},
		{http.MethodPut, "/api/v1/sites/{siteId}/albums/{year}", "putAlbumYear", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutAlbumYear)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/albums/{year}/{filename}", "deleteAlbumPhoto", a.requireAuth(a.withCSRF(a.contentRoute(a.handleDeleteAlbumPhoto)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/footprints", "getFootprints", a.requireAuth(a.contentRoute(a.handleGetFootprints))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/footprints", "putFootprints", a.requireAuth(a.withCSRF(a.contentRoute(a.handlePutFootprints)))},

		// Build-preview tasks: creation freezes the version view; the runner
		// executes and persists state transitions that stay queryable here.
		{http.MethodPost, "/api/v1/sites/{siteId}/builds", "createBuild", a.requireAuth(a.withCSRF(a.handleCreateBuild))},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds", "listBuilds", a.requireAuth(a.handleListBuilds)},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds/{buildId}", "getBuild", a.requireAuth(a.handleGetBuild)},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-links", "createPreviewLink", a.requireAuth(a.withCSRF(a.handleCreatePreviewLink))},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-close", "closePreviewLink", a.requireAuth(a.withCSRF(a.handleClosePreviewLink))},

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
		provisioner:      cfg.Provisioner,
		edge:             cfg.Edge,
		content:          cfg.Content,
		mediaTasks:       cfg.MediaTasks,
		releasesRoot:     cfg.ReleasesRoot,
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
