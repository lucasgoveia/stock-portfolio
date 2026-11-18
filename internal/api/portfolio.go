package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
)

// Amount is a body carrying one BRL amount.
type Amount struct {
	Amount money.Decimal `json:"amount" doc:"BRL"`
}

// WithPortfolio mounts the snapshot, planner and trim routes.
func WithPortfolio(svc *portfolio.Service) Option {
	return func(s *server) {
		const tag = "portfolio"
		huma.Register(s.api, op(http.MethodGet, "/api/portfolio", "getPortfolio", "Portfolio snapshot with targets, bands and cash", tag),
			func(ctx context.Context, _ *struct{}) (*Body[portfolio.Snapshot], error) {
				return ok(svc.Snapshot(ctx))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/plans/contribution", "planContribution", "Plan a contribution", tag),
			func(ctx context.Context, in *Body[Amount]) (*Body[portfolio.ContributionPlan], error) {
				return ok(svc.PlanContribution(ctx, in.Body.Amount.Decimal))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/trims", "listTrims", "Suggested trims", tag),
			func(ctx context.Context, _ *struct{}) (*Body[portfolio.TrimsResult], error) {
				return ok(svc.Trims(ctx))
			})
		huma.Register(s.api, opStatus(http.MethodPut, "/api/portfolio/pending-us", "setPendingUS", "Set the pending US bucket (BRL)", tag, http.StatusNoContent),
			func(ctx context.Context, in *Body[Amount]) (*struct{}, error) {
				return noContent(svc.SetPendingUS(ctx, in.Body.Amount.Decimal))
			})
	}
}
