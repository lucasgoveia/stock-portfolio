package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/tracking"
)

// WithExpectedVsActual mounts the expected-versus-actual route.
func WithExpectedVsActual(svc *tracking.Service) Option {
	return func(s *server) {
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/expected-vs-actual", "getExpectedVsActual",
			"Reported quarters against the base scenario of the fair value version that preceded each", "fundamentals"),
			func(ctx context.Context, in *IDPath) (*Body[tracking.Comparison], error) {
				return ok(svc.Get(ctx, in.ID))
			})
	}
}
