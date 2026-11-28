// Package httpapi implements the management HTTP API: WebAuthn ceremonies,
// cookie sessions with CSRF/Origin enforcement, and site settings with
// optimistic concurrency.
package httpapi

import (
	"context"
	"crypto/sha256"
	"io"
	"strings"

	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/edge"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeimport"
)

// Cookie and header names.
const (
	sessionCookieName       = "fuyao_admin_session"
	secureSessionCookieName = "__Host-fuyao-admin-session"
	authTxCookieName        = "fuyao_admin_auth_tx"
	secureAuthTxCookieName  = "__Host-fuyao-admin-auth-tx"
	authTxCookiePath        = "/api/v1/auth"
	csrfHeaderName          = "X-CSRF-Token"
)

// Config carries the HTTP-facing runtime configuration.
type Config struct {
	UpdaterSocket          string
	CaddyReadCmd           []string
	CaddyControl           func(context.Context, caddymanage.Request) (caddymanage.Result, error)
	CaddyManagementEnabled bool
	PlatformContent        *content.Store
	PlatformContentThemeID uuid.UUID
	TrafficStatsURL        string
	TrafficStatsSiteID     uuid.UUID
	OpenAPIJSON            []byte
	RPID                   string
	RPOrigins              []string
	CookieSecure           bool
	SessionTTL             time.Duration
	ChallengeTTL           time.Duration
	RegistrationTTL        time.Duration
	ThemeContracts         ThemeConfigValidator
	ThemeImporter          *themeimport.Manager
	BuildEnqueuer          BuildEnqueuer
	Previews               PreviewIssuer
	Publisher              Publisher
	ArtifactCleaner        *publish.ArtifactCleaner
	PublishedOrigins       PublishedOrigins
	Provisioner            SiteProvisioner
	Edge                   EdgeManager
	Content                ContentStore
	MediaTasks             *store.MediaTaskStore
	ReleasesRoot           string
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
	ListPosts(siteID uuid.UUID, query string, page, pageSize int, order ...content.PostSort) ([]content.Post, int, error)
	GetPostTaxonomy(siteID uuid.UUID) (content.PostTaxonomy, error)
	GetPostVocabulary(siteID uuid.UUID) (content.PostVocabulary, string, error)
	SavePostVocabulary(siteID uuid.UUID, doc content.PostVocabulary, expectedDigest string) (content.PostVocabulary, string, error)
	RenamePostTag(siteID uuid.UUID, from string, to *string, expectedDigest string) (int, content.PostVocabulary, string, error)
	GetPostDirectory(siteID uuid.UUID, slug string) (content.PostDirectoryDocument, string, error)
	SavePostDirectory(siteID uuid.UUID, slug, title, expectedDigest string) (content.PostDirectoryDocument, string, error)
	DeletePostCategory(siteID uuid.UUID, slug, expectedDigest string) error
	ListPostMedia(siteID uuid.UUID) ([]content.PostMedia, error)
	ReadPostMedia(siteID uuid.UUID, path string) ([]byte, string, error)
	StorePostMedia(siteID uuid.UUID, path string, reader io.Reader, size int64) (content.PostMedia, error)
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
	ReadAlbumThumbnail(siteID uuid.UUID, name string) ([]byte, string, error)
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
	themeImporter    *themeimport.Manager
	buildEnqueuer    BuildEnqueuer
	previews         PreviewIssuer
	publisher        Publisher
	artifactCleaner  *publish.ArtifactCleaner
	publishedOrigins PublishedOrigins
	provisioner      SiteProvisioner
	edge             EdgeManager
	content          ContentStore
	mediaTasks       *store.MediaTaskStore
	releasesRoot     string
	deployEnabled    bool
	platformScope    bool
	portMin          int
	portMax          int
	exchangeLimiter  *exchangeLimiter
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
		themeImporter:    cfg.ThemeImporter,
		buildEnqueuer:    cfg.BuildEnqueuer,
		previews:         cfg.Previews,
		publisher:        cfg.Publisher,
		artifactCleaner:  cfg.ArtifactCleaner,
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
		mux.HandleFunc(route.Method+" "+route.Pattern, api.auditRoute(route))
	}

	var h http.Handler = mux
	h = api.previewHostRoutes(h)
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
			if id, err := uuid.Parse(r.PathValue("siteId")); err == nil && strings.HasPrefix(r.URL.Path, "/api/v1/sites/"+id.String()+"/releases") {
				if site, readErr := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID); readErr == nil && a.canPublishManaged(r.Context(), site, auth) {
					h(w, r, auth)
					return
				}
			}
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
// JSON bodies. Specialized UI and preview document handlers supply their CSP.
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
		cookie, err := uniqueCookie(r, a.sessionCookieName())
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
		setAuditActor(r, auth.User)
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
		if r.PathValue("siteId") != "" && strings.HasPrefix(r.URL.Path, "/api/v1/sites/") {
			id, ok := parseSiteID(w, r)
			if !ok {
				return
			}
			site, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
			if err != nil {
				writeSiteLookupError(w, err)
				return
			}
			base := "/api/v1/sites/" + id.String()
			selectable := site.ManagementMode == "platform" && site.Kind == "static"
			tail := strings.TrimPrefix(r.URL.Path, base)
			genericContent := strings.HasPrefix(tail, "/posts") || strings.HasPrefix(tail, "/albums") || tail == "/content/friends" || tail == "/content/footprints"
			publicationRead := r.Method == http.MethodGet && (tail == "/release" || strings.HasPrefix(tail, "/releases"))
			if !site.ThemeEnabled && site.Kind != "theme" && r.URL.Path != base && r.URL.Path != base+"/caddy" && !strings.HasPrefix(r.URL.Path, base+"/caddy/") && r.URL.Path != base+"/settings" && r.URL.Path != base+"/overview" && r.URL.Path != base+"/content-sources" && !(selectable && (tail == "/theme" || genericContent || publicationRead)) {
				writeProblem(w, 409, "site_kind_mismatch", "This route is only available for theme-managed sites.")
				return
			}
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
		Name:     a.sessionCookieName(),
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
		Name:     a.sessionCookieName(),
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
		Name:     a.authTxCookieName(),
		Value:    id.String(),
		Path:     a.authTxCookiePath(),
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.cfg.ChallengeTTL.Seconds()),
	})
}

func (a *API) clearAuthTxCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.authTxCookieName(),
		Value:    "",
		Path:     a.authTxCookiePath(),
		HttpOnly: true,
		Secure:   a.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// userDTO is the public representation of an administrator.
type userDTO struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	PlatformRole string `json:"platformRole"`
}

func toUserDTO(u model.User) userDTO {
	return userDTO{ID: u.ID.String(), Username: u.Username, DisplayName: u.DisplayName, PlatformRole: u.PlatformRole}
}
