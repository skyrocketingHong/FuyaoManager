package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// uiCSP keeps the console self-contained: same-origin scripts, connections,
// and fonts; antd's runtime styling requires inline styles. The console
// never embeds third-party content.
const uiCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// WithUI serves the built admin console for non-API paths and delegates
// everything under /api/ to the management API. The console is a static SPA:
// unknown paths fall back to index.html so deep links survive reloads.
//
// Headers: the document itself is never cached (redeploys must apply on
// reload); hashed assets under /assets/ may be cached. A restrictive CSP
// keeps the console self-contained (antd requires inline styles).
func WithUI(api http.Handler, uiDir string) http.Handler {
	root, err := filepath.Abs(uiDir)
	if err != nil {
		panic("resolve UI dir: " + err.Error())
	}
	indexFile := filepath.Join(root, "index.html")
	fileServer := http.FileServer(http.Dir(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			api.ServeHTTP(w, r)
			return
		}

		clean := filepath.Clean("/" + r.URL.Path)
		candidate := filepath.Join(root, clean)
		// Path-traversal guard: the cleaned candidate must stay inside root.
		if candidate != root && !strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			serveIndex(w, r, indexFile)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", uiCSP)
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, indexFile string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", uiCSP)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, indexFile)
}
