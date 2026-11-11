package publish

import (
	"context"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// ServingTarget resolves what a published origin serves right now. store.
// ReleaseStore implements it with GetCurrentTarget.
type ServingTarget interface {
	GetCurrentTarget(ctx context.Context, siteID uuid.UUID) (store.ReleaseServingTarget, error)
}

// Server keeps one loopback origin per site that has a live release. The
// listener resolves the current release per request, so publishing or
// rolling back switches the served content without restarting the origin.
// Origins are loopback-only; this is the local publishing loop, not the
// production edge.
type Server struct {
	target       ServingTarget
	releasesRoot string
	mu           sync.Mutex
	origins      map[uuid.UUID]*publishedOrigin
	closed       bool
}

// NewServer assembles the published-origins server.
func NewServer(target ServingTarget, releasesRoot string) *Server {
	return &Server{target: target, releasesRoot: releasesRoot, origins: make(map[uuid.UUID]*publishedOrigin)}
}

// OriginURL returns the site's published origin URL, starting the listener on
// first use. An empty string means the site has no live release.
func (p *Server) OriginURL(ctx context.Context, siteID uuid.UUID) (string, error) {
	if _, err := p.target.GetCurrentTarget(ctx, siteID); err != nil {
		return "", err
	}
	origin, err := p.originFor(siteID)
	if err != nil {
		return "", err
	}
	return "http://" + origin.ln.Addr().String(), nil
}

// Close shuts every published origin down.
func (p *Server) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for _, origin := range p.origins {
		_ = origin.httpSrv.Close()
	}
}

func (p *Server) originFor(siteID uuid.UUID) (*publishedOrigin, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, os.ErrClosed
	}
	if origin, ok := p.origins[siteID]; ok {
		return origin, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	origin := &publishedOrigin{server: p, siteID: siteID, ln: ln}
	origin.httpSrv = &http.Server{Handler: origin, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = origin.httpSrv.Serve(ln) }()
	p.origins[siteID] = origin
	return origin, nil
}

// publishedOrigin is one site's loopback origin serving the site's current
// release directory with production URL semantics (root mapping, declared SPA
// fallback).
type publishedOrigin struct {
	server  *Server
	siteID  uuid.UUID
	ln      net.Listener
	httpSrv *http.Server
}

func (o *publishedOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Same security posture as the private preview origins: the theme's real
	// runtime (map SDK, external images and data requests, inline bootstrap)
	// stays functional while remaining a distinct loopback origin.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'unsafe-inline' https://webapi.amap.com; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; "+
			"font-src 'self' data:; connect-src 'self' data: blob: https:; "+
			"worker-src 'self' blob:; base-uri 'self'; form-action 'self'; "+
			"frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Cache-Control", "no-cache")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	target, err := o.server.target.GetCurrentTarget(ctx, o.siteID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root := os.DirFS(filepath.Join(o.server.releasesRoot, o.siteID.String(), target.ReleaseDir))
	o.serveRelease(w, r, root, target.SpaFallback)
}

// serveRelease serves the release tree: root maps to index.html, directories
// try their index.html, misses fall back to the release's declared SPA
// fallback, everything else 404s — the same semantics as the preview origin.
func (o *publishedOrigin) serveRelease(w http.ResponseWriter, r *http.Request, root fs.FS, spaFallback string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	name := strings.Trim(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if name != filepath.ToSlash(filepath.Clean(name)) || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if stat, err := fs.Stat(root, name); err == nil && stat.IsDir() {
		name = strings.TrimSuffix(name, "/") + "/index.html"
	}
	if _, err := fs.Stat(root, name); err != nil {
		if spaFallback != "" && fs.ValidPath(spaFallback) {
			if _, fbErr := fs.Stat(root, spaFallback); fbErr != nil {
				http.NotFound(w, r)
				return
			}
			name = spaFallback
		} else {
			http.NotFound(w, r)
			return
		}
	}
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
		http.ServeContent(w, r, info.Name(), info.ModTime(), seeker)
		return
	}
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
	_, _ = io.Copy(w, file)
}
