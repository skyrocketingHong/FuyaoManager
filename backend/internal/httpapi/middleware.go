package httpapi

import (
	"net/http"
	"time"
)

// recoverer converts panics into generic 500 problems without leaking stack
// traces or SQL details.
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.logger.Error("handler panic", "method", r.Method, "path", r.URL.Path, "panic", rec)
				writeProblem(w, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger emits one structured log line per request without bodies,
// cookies, or credentials.
func (a *API) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// cors answers preflights and decorates responses for the configured admin
// origins only.
func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowed := false
		if origin != "" {
			_, allowed = a.origins[origin]
		}
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			// ETag must be readable on normal responses too, not just
			// preflights, or cross-origin clients lose concurrency control.
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if allowed {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, If-Match")
				w.Header().Set("Access-Control-Expose-Headers", "ETag")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeProblem(w, http.StatusForbidden, "origin_rejected", "The request origin is not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originGuard rejects unsafe-method requests whose Origin is not an allowed
// admin origin. Requests without an Origin header (non-browser clients) are
// accepted only when the browser-provided Sec-Fetch-Site marker declares the
// request same-origin; browser responses to cross-site requests always carry
// a mismatching Origin.
func (a *API) originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := a.origins[origin]; !ok {
				writeProblem(w, http.StatusForbidden, "origin_rejected", "The request origin is not allowed.")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
			next.ServeHTTP(w, r)
			return
		}
		writeProblem(w, http.StatusForbidden, "origin_rejected", "Requests without an Origin header must declare Sec-Fetch-Site: same-origin.")
	})
}
