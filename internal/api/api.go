// Package api holds HTTP handlers.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
)

// Option configures optional router dependencies.
type Option func(chi.Router)

// NewRouter builds the HTTP router.
func NewRouter(opts ...Option) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/health", health)
	for _, o := range opts {
		o(r)
	}
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// WithAssets mounts the asset routes.
func WithAssets(svc *assets.Service) Option {
	return func(r chi.Router) { mountAssets(r, svc) }
}
