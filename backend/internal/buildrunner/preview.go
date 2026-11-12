package buildrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// PreviewTarget resolves what a preview origin serves: the succeeded build's
// artifact directory and the theme's declared SPA fallback file. store.
// BuildStore implements it.
type PreviewTarget interface {
	GetPreviewTarget(ctx context.Context, buildID uuid.UUID) (artifactPath, spaFallback string, err error)
}

// PortSource allocates and releases platform port assignments for preview
// origins. store.NetworkEntryStore implements it.
type PortSource interface {
	EnsurePreviewEntry(ctx context.Context, siteID, buildID uuid.UUID, minPort, maxPort int) (store.NetworkEntry, bool, error)
	ReleasePreview(ctx context.Context, buildID uuid.UUID) error
}

// MediaRoots resolves a site's album media directories for preview
// serving: photos originals and thumbnail variants under the site's
// content tree. The frozen build references these through the theme's
// external media convention (/media/albums/...), so the preview origin
// serves them from the same authority the published site uses.
type MediaRoots func(siteID uuid.UUID) (photosRoot, thumbnailsRoot string, err error)

// PreviewConfig assembles the private preview service. BuildsRoot is the
// per-task root; artifact paths must resolve inside it. BindAddr is the
// address preview listeners bind (loopback for local development, the
// private-network address for remote access); ExternalBase is the URL base
// handed to browsers (never a 127.0.0.1 literal when remote clients exist).
// Ports, PortMin/PortMax enable persistent platform port allocation; without
// Ports the origins use OS-assigned ports on the bind address. MediaRoots
// enables /media/albums serving for previews; without it media paths 404.
type PreviewConfig struct {
	Target       PreviewTarget
	BuildsRoot   string
	TTL          time.Duration
	MaxOrigins   int
	BindAddr     string
	ExternalBase string // e.g. "http://100.117.154.115"
	Ports        PortSource
	PortMin      int
	PortMax      int
	MediaRoots   MediaRoots
	// SweepInterval controls the expiry sweeper (diagnostics and tests);
	// the default is TTL/4 bounded to [250ms, 30s].
	SweepInterval time.Duration
}

// previewCookiePrefix starts the per-origin authorization cookie name.
// Browser cookies are scoped by host and path only — never by port — and
// every preview origin shares 127.0.0.1 with Path "/", so one fixed name
// would let a second preview overwrite the first's authorization in the same
// cookie jar. The build id suffix keeps concurrent previews coexisting.
const previewCookiePrefix = "fuyao_preview_"

// cookieName is this origin's unique authorization cookie name.
func (o *previewOrigin) cookieName() string {
	return previewCookiePrefix + o.buildID.String()
}

// previewOrigin is one build's private loopback origin: its own listener,
// its own token set, and its own instance id. Because each build gets its
// own port, the theme's root-absolute asset, data, and deep-route URLs
// behave exactly as in production, and two builds can never serve each
// other's files. The instance id distinguishes consecutive incarnations of
// the same build (or a reused port): a page opened by an earlier incarnation
// cannot ride a later one's cookie past its probe.
type previewOrigin struct {
	server      *PreviewServer
	siteID      uuid.UUID
	buildID     uuid.UUID
	instanceID  string
	mu          sync.Mutex
	tokens      map[string]time.Time
	ln          net.Listener
	port        int
	httpSrv     *http.Server
	artifactDir string
	spaFallback string
	mediaRoots  MediaRoots
}

// PreviewServer issues short-lived per-build preview links on per-build
// private origins. Origins are reclaimed when they expire: the sweeper (and
// the lazy sweep on every Issue) drop origins whose tokens have all expired,
// revoking credentials, stopping the listener, and releasing the persistent
// port assignment — so an expired preview never pins capacity.
type PreviewServer struct {
	cfg     PreviewConfig
	mu      sync.Mutex
	origins map[uuid.UUID]*previewOrigin
	closed  bool
	stop    chan struct{}
	done    chan struct{}
}

// NewPreviewServer assembles the preview service and starts the expiry
// sweeper; Close stops it and tears every origin down.
func NewPreviewServer(cfg PreviewConfig) *PreviewServer {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Minute
	}
	if cfg.MaxOrigins <= 0 {
		cfg.MaxOrigins = 100
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = "127.0.0.1"
	}
	if cfg.SweepInterval <= 0 {
		interval := cfg.TTL / 4
		if interval < 250*time.Millisecond {
			interval = 250 * time.Millisecond
		}
		if interval > 30*time.Second {
			interval = 30 * time.Second
		}
		cfg.SweepInterval = interval
	}
	cfg.ExternalBase = strings.TrimRight(cfg.ExternalBase, "/")
	p := &PreviewServer{
		cfg:     cfg,
		origins: make(map[uuid.UUID]*previewOrigin),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go p.sweepLoop()
	return p
}

// sweepLoop periodically reclaims expired origins. Test knobs live in
// SweepInterval; production defaults keep the loop under one call per TTL
// quarter and at most one per 30s.
func (p *PreviewServer) sweepLoop() {
	defer close(p.done)
	ticker := time.NewTicker(p.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.sweepExpired()
		}
	}
}

// sweepExpired removes every origin whose tokens have all expired. Media
// access dies with the listener; the persistent port assignment is released
// so other builds can allocate it.
func (p *PreviewServer) sweepExpired() {
	p.mu.Lock()
	defer p.mu.Unlock()
	var expired []*previewOrigin
	for id, origin := range p.origins {
		if origin.expired() {
			expired = append(expired, origin)
			delete(p.origins, id)
		}
	}
	for _, origin := range expired {
		origin.revoke()
		_ = origin.httpSrv.Close()
		if p.cfg.Ports != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = p.cfg.Ports.ReleasePreview(ctx, origin.buildID)
			cancel()
		}
	}
}

// expired reports whether the origin has no unexpired token left. Tokens
// are minted immediately when an origin is created, so "no live token"
// unambiguously means the preview's lifetime is over.
func (o *previewOrigin) expired() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	for _, expiry := range o.tokens {
		if now.Before(expiry) {
			return false
		}
	}
	return true
}

// revoke clears every token, invalidating all issued credentials at once.
func (o *previewOrigin) revoke() {
	o.mu.Lock()
	o.tokens = make(map[string]time.Time)
	o.mu.Unlock()
}

// ErrPreviewNotReady marks issue requests for builds that have no servable
// artifacts.
var ErrPreviewNotReady = errors.New("preview target is not ready")

// Issue mints a fresh token for one build and returns the entry URL on that
// build's own private origin. Visiting the URL exchanges the token for an
// HttpOnly cookie scoped to the origin; everything else on the origin
// requires the cookie. Tokens are unguessable and expire; there is no
// anonymous directory access. Expired origins are reclaimed lazily first,
// so a full capacity never stays pinned by stale previews.
func (p *PreviewServer) Issue(siteID, buildID uuid.UUID) (url string, expiresAt time.Time, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	artifactPath, spaFallback, err := p.cfg.Target.GetPreviewTarget(ctx, buildID)
	if err != nil {
		return "", time.Time{}, ErrPreviewNotReady
	}
	resolvedRoot, err := filepath.EvalSymlinks(p.cfg.BuildsRoot)
	if err != nil {
		return "", time.Time{}, ErrPreviewNotReady
	}
	resolvedArtifact, err := filepath.EvalSymlinks(artifactPath)
	if err != nil {
		return "", time.Time{}, ErrPreviewNotReady
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedArtifact)
	if err != nil || rel == "." || rel == "" || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", time.Time{}, ErrPreviewNotReady
	}

	// Lazy reclaim: drop expired origins before the capacity check so a
	// long-expired preview cannot pin MaxOrigins.
	p.sweepExpired()

	p.mu.Lock()
	defer p.mu.Unlock()
	origin, err := p.originForLocked(siteID, buildID, resolvedArtifact, spaFallback)
	if err != nil {
		return "", time.Time{}, err
	}
	token, err := NewPreviewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(p.cfg.TTL)
	origin.mu.Lock()
	origin.tokens[token] = expiresAt
	origin.mu.Unlock()
	// The URL always carries the externally reachable base; the listener's
	// own address (loopback or private bind) is never leaked to browsers.
	base := p.cfg.ExternalBase
	if base == "" {
		base = "http://" + p.cfg.BindAddr
	}
	return base + ":" + strconv.Itoa(origin.port) + "/__preview__/" + token, expiresAt, nil
}

// CloseOrigin tears down one build's preview: every issued token is revoked,
// the listener stops, and the platform port assignment is released. It is
// idempotent — closing an already-closed (or never-opened) preview succeeds —
// and never touches other builds' or sites' previews.
func (p *PreviewServer) CloseOrigin(siteID, buildID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	origin, ok := p.origins[buildID]
	if ok && origin.siteID != siteID {
		return ErrPreviewNotReady
	}
	if ok {
		delete(p.origins, buildID)
	}
	if ok {
		origin.revoke()
		_ = origin.httpSrv.Close()
	}
	if p.cfg.Ports != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// ReleasePreview deletes only this build's preview entry; releasing
		// an already-released (or never-allocated) entry is a no-op.
		return p.cfg.Ports.ReleasePreview(ctx, buildID)
	}
	return nil
}

// IsActive reports whether this process currently serves the build's
// private preview origin with at least one unexpired token. It reflects
// live in-process state only — never a stale platform port assignment —
// so the management API can label a task with a truthful previewActive.
func (p *PreviewServer) IsActive(siteID, buildID uuid.UUID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	o := p.origins[buildID]
	return o != nil && o.siteID == siteID && !o.expired()
}

// Close stops the sweeper and tears every origin down. Listeners and
// in-memory state die with the process; persistent port assignments of
// still-live previews are released so a restart cannot leak them (fresh
// issues allocate anew).
func (p *PreviewServer) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	origins := make([]*previewOrigin, 0, len(p.origins))
	for id, origin := range p.origins {
		origins = append(origins, origin)
		delete(p.origins, id)
	}
	p.mu.Unlock()

	close(p.stop)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
	}
	for _, origin := range origins {
		origin.revoke()
		_ = origin.httpSrv.Close()
	}
	if p.cfg.Ports != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, origin := range origins {
			_ = p.cfg.Ports.ReleasePreview(ctx, origin.buildID)
		}
	}
}

// originFor returns (creating on first use) the per-build origin listener.
func (p *PreviewServer) originForLocked(siteID, buildID uuid.UUID, artifactDir, spaFallback string) (*previewOrigin, error) {
	if p.closed {
		return nil, ErrPreviewNotReady
	}
	if origin, ok := p.origins[buildID]; ok {
		return origin, nil
	}
	if len(p.origins) >= p.cfg.MaxOrigins {
		return nil, errors.New("preview origin limit reached; restart the service to clear")
	}
	// Fixed loopback bind: never an empty host, never a non-loopback address.
	port := 0
	bindAddr := p.cfg.BindAddr
	if p.cfg.Ports != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		entry, _, err := p.cfg.Ports.EnsurePreviewEntry(ctx, siteID, buildID, p.cfg.PortMin, p.cfg.PortMax)
		cancel()
		if err != nil {
			return nil, err
		}
		port = entry.Port
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bindAddr, strconv.Itoa(port)))
	if err != nil {
		// Port 0 asks the OS for any free port; a persistent allocation binds
		// its exact platform port.
		if port != 0 {
			return nil, fmt.Errorf("bind preview port %d on %s: %w", port, bindAddr, err)
		}
		return nil, err
	}
	if port == 0 {
		port = ln.Addr().(*net.TCPAddr).Port
	}
	origin := &previewOrigin{
		server:      p,
		siteID:      siteID,
		buildID:     buildID,
		instanceID:  uuid.NewString(),
		tokens:      make(map[string]time.Time),
		ln:          ln,
		port:        port,
		artifactDir: artifactDir,
		spaFallback: spaFallback,
		mediaRoots:  p.cfg.MediaRoots,
	}
	origin.httpSrv = &http.Server{Handler: origin, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = origin.httpSrv.Serve(ln) }()
	p.origins[buildID] = origin
	return origin, nil
}

// ServeHTTP implements the origin: the entry path exchanges a token for a
// cookie, everything else requires the cookie and serves that build's frozen
// artifacts with real root-path semantics.
func (o *previewOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CSP allows the first theme's real runtime: the inline SvelteKit
	// bootstrap, the AMap JS SDK (webapi.amap.com) plus its WebGL blob
	// workers, external avatars and wallpaper/tile images, and the theme's
	// data requests (wallpaper API, AMap REST, configured proxies) — while
	// remaining a distinct origin from the management API, so cross-origin
	// management responses stay unreadable to theme scripts. frame-ancestors
	// 'none' keeps admin UIs from embedding it.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'unsafe-inline' https://webapi.amap.com; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; "+
			"font-src 'self' data:; connect-src 'self' data: blob: https:; "+
			"worker-src 'self' blob:; base-uri 'self'; form-action 'self'; "+
			"frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Match the production front end's referrer behavior (browser default:
	// strict-origin-when-cross-origin). Domain-whitelisted services such as
	// the AMap key see the same class of Referer they would in production;
	// no-referrer here would unconditionally break them in previews.
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Cache-Control", "private, no-store")
	// A preview must not install a service worker that could keep serving a
	// closed origin from its offline cache. The registration fetch is the
	// only request the browser marks with "Service-Worker: script", so
	// rejection is path-independent instead of guessing file names; ordinary
	// script resources at the same paths still serve.
	if strings.EqualFold(r.Header.Get("Service-Worker"), "script") {
		http.NotFound(w, r)
		return
	}

	// Token entry: exchange and redirect to the origin root.
	if strings.HasPrefix(r.URL.Path, "/__preview__/") {
		token := strings.TrimPrefix(r.URL.Path, "/__preview__/")
		if !o.validToken(token) {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     o.cookieName(),
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(o.server.cfg.TTL.Seconds()),
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	token := ""
	if c, err := r.Cookie(o.cookieName()); err == nil {
		token = c.Value
	}
	if r.URL.Path == previewStatusPath {
		// The probe must carry this origin's instance id on top of a valid
		// cookie: after a close-and-reopen on the same port (same build or a
		// reused one), a stale page must die even when the fresh incarnation
		// shares its cookie name. The fresh instance's cookies are untouched.
		if r.URL.Query().Get(previewInstanceParam) != o.instanceID {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Method != http.MethodGet || !o.validToken(token) {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !o.validToken(token) {
		http.NotFound(w, r)
		return
	}
	// External album media: serve from the site's content tree (the same
	// authority the published site maps through the edge), path-jailed and
	// authorized by this origin's cookie. A missing file is a real 404 —
	// never the SPA fallback, which would fake a successful HTML response.
	if strings.HasPrefix(r.URL.Path, "/media/albums/photos/") ||
		strings.HasPrefix(r.URL.Path, "/media/albums/thumbnails/") {
		o.serveMedia(w, r)
		return
	}
	o.serveArtifact(w, r)
}

// serveMedia serves one album media file from the site's content directories
// with traversal protection. Only the two fixed prefixes are reachable, and
// the remainder must resolve inside the matching root.
func (o *previewOrigin) serveMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if o.mediaRoots == nil {
		http.NotFound(w, r)
		return
	}
	photosRoot, thumbsRoot, err := o.mediaRoots(o.siteID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rel := ""
	root := ""
	switch {
	case strings.HasPrefix(r.URL.Path, "/media/albums/photos/"):
		root = photosRoot
		rel = strings.TrimPrefix(r.URL.Path, "/media/albums/photos/")
	case strings.HasPrefix(r.URL.Path, "/media/albums/thumbnails/"):
		root = thumbsRoot
		rel = strings.TrimPrefix(r.URL.Path, "/media/albums/thumbnails/")
	}
	if root == "" || rel == "" {
		http.NotFound(w, r)
		return
	}
	name := path.Clean("/" + rel)
	if name == "/" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	rel = strings.TrimPrefix(name, "/")
	if !fs.ValidPath(rel) {
		http.NotFound(w, r)
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(resolved)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	// Only image content types are served from the media roots.
	switch strings.ToLower(filepath.Ext(resolved)) {
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".webp":
		w.Header().Set("Content-Type", "image/webp")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	default:
		http.NotFound(w, r)
		return
	}
	// *os.File implements io.ReadSeeker, giving range support for images.
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// validToken checks the origin's own token set; tokens from another build's
// origin are invalid here, which is what keeps builds from cross-serving.
func (o *previewOrigin) validToken(token string) bool {
	if token == "" || len(token) != 64 || !isHex(token) {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	for t, expiry := range o.tokens {
		if now.After(expiry) {
			delete(o.tokens, t)
		}
	}
	expiry, ok := o.tokens[token]
	if !ok || now.After(expiry) {
		return false
	}
	return true
}

// serveArtifact serves the frozen tree with production URL semantics: root
// maps to index.html, directories try their index.html, misses fall back to
// the theme's declared SPA fallback file, everything else 404s.
func (o *previewOrigin) serveArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	root := os.DirFS(o.artifactDir)
	name := strings.Trim(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	// fs.ValidPath rejects trailing slashes and dot segments; clean the
	// request path into a plain relative form first.
	if name != filepath.ToSlash(filepath.Clean(name)) || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if stat, err := fs.Stat(root, name); err == nil && stat.IsDir() {
		name = strings.TrimSuffix(name, "/") + "/index.html"
	}
	if _, err := fs.Stat(root, name); err != nil {
		if o.spaFallback != "" && fs.ValidPath(o.spaFallback) {
			if _, fbErr := fs.Stat(root, o.spaFallback); fbErr != nil {
				http.NotFound(w, r)
				return
			}
			name = o.spaFallback
		} else {
			http.NotFound(w, r)
			return
		}
	}
	// Serve the resolved file directly. http.FileServerFS would canonicalize
	// "/index.html" into a "./" redirect and loop with our own root mapping.
	o.serveFile(w, r, root, name)
}

// serveFile writes one resolved artifact file with range support.
func (o *previewOrigin) serveFile(w http.ResponseWriter, r *http.Request, root fs.FS, name string) {
	file, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if seeker, ok := file.(io.ReadSeeker); ok {
		if strings.EqualFold(filepath.Ext(name), ".html") {
			servePreviewHTML(w, r, info, seeker, o.instanceID)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), seeker)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
	if _, err := io.Copy(w, file); err != nil {
		// body already partially written; nothing more to do
		return
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
