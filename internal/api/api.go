// Package api holds HTTP handlers. Operations are registered with huma, which
// generates the OpenAPI document served at /api/openapi.json.
package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// Title and Version describe the OpenAPI document.
const (
	Title   = "Stock Portfolio API"
	Version = "0.2.0"
)

// Health is the body of GET /api/health.
type Health struct {
	Status string `json:"status" example:"ok"`
}

// server is what an Option configures: the chi router for raw routes and the
// huma API for documented operations.
type server struct {
	r   chi.Router
	api huma.API
}

// Option configures optional router dependencies.
type Option func(*server)

// NewRouter builds the HTTP router. Unknown paths return a JSON 404 unless
// WithUI is given.
func NewRouter(opts ...Option) http.Handler {
	r, _ := build(opts...)
	return r
}

// OpenAPI returns the OpenAPI document for the given options. Services may be
// nil: registration never calls them.
func OpenAPI(opts ...Option) *huma.OpenAPI {
	_, a := build(opts...)
	return a.OpenAPI()
}

// AllRoutes is every documented route group with nil services, for generating
// the OpenAPI document without a database.
func AllRoutes() []Option {
	return []Option{
		WithAssets(nil), WithCash(nil), WithRatings(nil), WithResearch(nil), WithTrades(nil),
		WithAlerts(nil), WithThesis(nil), WithValuation(nil), WithPortfolio(nil), WithJobs(nil), WithTactical(nil, nil), WithTaxes(nil), WithDividends(nil), WithCorporateActions(nil), WithPerformance(nil), WithSettings(nil), WithFundamentals(nil), WithAnalyst(nil), WithExpectedVsActual(nil),
	}
}

func build(opts ...Option) (chi.Router, huma.API) {
	r := chi.NewRouter()
	cfg := huma.DefaultConfig(Title, Version)
	cfg.CreateHooks = nil // no $schema field or Link header in responses
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = ""
	cfg.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", schemaName)
	a := humachi.New(r, cfg)

	huma.Register(a, op(http.MethodGet, "/api/health", "health", "Health check", "system"),
		func(context.Context, *struct{}) (*Body[Health], error) {
			return &Body[Health]{Body: Health{Status: "ok"}}, nil
		})

	r.NotFound(jsonNotFound)
	s := &server{r: r, api: a}
	for _, o := range opts {
		o(s)
	}
	return r, a
}

// ambiguousNames are type names used by more than one domain package; their
// schemas are prefixed with the package name (portfolio.Position becomes
// PortfolioPosition).
var ambiguousNames = map[string]bool{"Position": true, "Run": true, "Usage": true}

func schemaName(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	if ambiguousNames[name] {
		pkg := path.Base(t.PkgPath())
		return strings.ToUpper(pkg[:1]) + pkg[1:] + name
	}
	return name
}

// op builds an operation with an explicit ID (it becomes the TS client name).
func op(method, path, id, summary, tag string) huma.Operation {
	return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Tags: []string{tag}}
}

// opStatus is op with a non-200 success status.
func opStatus(method, path, id, summary, tag string, status int) huma.Operation {
	o := op(method, path, id, summary, tag)
	o.DefaultStatus = status
	return o
}

func jsonNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}

// WithUI serves the built web app for every non-/api path, with SPA fallback
// to index.html. A nil ui is ignored.
func WithUI(ui fs.FS) Option {
	return func(s *server) {
		if ui == nil {
			return
		}
		s.r.NotFound(func(w http.ResponseWriter, req *http.Request) {
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
