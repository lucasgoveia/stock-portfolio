// Package api holds HTTP handlers.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
)

// NewRouter builds the HTTP router. assetSvc may be nil only in tests that do
// not touch asset routes.
func NewRouter(assetSvc *assets.Service) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/health", health)
	if assetSvc != nil {
		mountAssets(r, assetSvc)
	}
	return r
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
