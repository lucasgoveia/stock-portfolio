// Package api holds HTTP handlers.
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
)

// Option configures optional router dependencies.
type Option func(chi.Router)

// NewRouter builds the HTTP router. Unknown paths return a JSON 404 unless
// WithUI is given.
func NewRouter(opts ...Option) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/health", health)
	r.NotFound(jsonNotFound)
	for _, o := range opts {
		o(r)
	}
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func jsonNotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// WithAssets mounts the asset routes.
func WithAssets(svc *assets.Service) Option {
	return func(r chi.Router) { mountAssets(r, svc) }
}

// WithUI serves the built web app for every non-/api path, with SPA fallback
// to index.html. A nil ui is ignored.
func WithUI(ui fs.FS) Option {
	return func(r chi.Router) {
		if ui == nil {
			return
		}
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/api" || strings.HasPrefix(req.URL.Path, "/api/") {
				jsonNotFound(w, req)
				return
			}
			serveUI(w, req, ui)
		})
	}
}

// serveUI serves a static file if it exists, otherwise index.html so the
// client-side router can handle the path.
func serveUI(w http.ResponseWriter, req *http.Request, ui fs.FS) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		http.NotFound(w, req)
		return
	}
	name := strings.TrimPrefix(path.Clean(req.URL.Path), "/")
	if name != "" {
		if f, err := ui.Open(name); err == nil {
			st, serr := f.Stat()
			_ = f.Close()
			if serr == nil && !st.IsDir() {
				http.ServeFileFS(w, req, ui, name)
				return
			}
		}
	}
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		http.Error(w, "web UI not built; run `make build`", http.StatusNotFound)
		return
	}
	// ServeFileFS redirects "/index.html" to "/", so serve the bytes directly.
	b, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
