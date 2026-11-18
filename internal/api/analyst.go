package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
)

// WithAnalyst mounts the AI analyst routes. Starting a run is synchronous:
// the request stays open while the model answers (typically under two
// minutes) and the server sets no write timeout, which keeps the client a
// single request with no polling state. The run itself is detached from the
// request context, so a closed tab does not lose a paid run.
func WithAnalyst(svc *analyst.Service) Option {
	return func(s *server) {
		const tag = "analyst"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/analyst/runs", "startAnalystRun",
			"Run the AI analyst on an asset (refused when the key is missing or the monthly cap is reached)", tag, http.StatusCreated),
			func(ctx context.Context, in *IDPath) (*Body[analyst.Run], error) {
				return ok(svc.Run(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/analyst/runs", "listAnalystRuns", "An asset's analyst runs, newest first", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]analyst.Run], error) {
				return ok(svc.List(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/analyst/runs/{id}", "getAnalystRun", "One analyst run", tag),
			func(ctx context.Context, in *IDPath) (*Body[analyst.Run], error) {
				return ok(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/analyst/runs/{id}/approve", "approveAnalystRun",
			"Apply selected parts of a draft into notes and fair value versions", tag),
			func(ctx context.Context, in *BodyIn[analyst.ApproveInput]) (*Body[analyst.Run], error) {
				return ok(svc.Approve(ctx, in.ID, in.Body))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/analyst/runs/{id}/discard", "discardAnalystRun", "Discard a draft", tag),
			func(ctx context.Context, in *IDPath) (*Body[analyst.Run], error) {
				return ok(svc.Discard(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/analyst/usage", "getAnalystUsage", "Month-to-date AI spend, the cap and whether a run may start", tag),
			func(ctx context.Context, _ *struct{}) (*Body[analyst.Usage], error) {
				return ok(svc.Usage(ctx))
			})
	}
}
