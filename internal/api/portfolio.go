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

// OrderUpdate is the body of POST /api/contributions/{id}/orders/{seq}.
type OrderUpdate struct {
	Status  string `json:"status" enum:"open,done,skipped"`
	TradeID *int64 `json:"trade_id,omitempty" doc:"The buy trade that executed this order"`
}

type orderIn struct {
	IDPath
	Seq  int64 `path:"seq" minimum:"1"`
	Body OrderUpdate
}

type contributionListIn struct {
	Limit int `query:"limit" minimum:"0" maximum:"200" doc:"Default 50"`
}

// WithPortfolio mounts the snapshot, planner and trim routes.
func WithPortfolio(svc *portfolio.Service) Option {
	return func(s *server) {
		const tag = "portfolio"
		huma.Register(s.api, op(http.MethodGet, "/api/portfolio", "getPortfolio", "Portfolio snapshot with targets, bands and cash", tag),
			func(ctx context.Context, _ *struct{}) (*Body[portfolio.Snapshot], error) {
				return ok(svc.Snapshot(ctx))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/plans/contribution", "planContribution", "Preview a contribution plan without storing it", tag),
			func(ctx context.Context, in *Body[Amount]) (*Body[portfolio.ContributionPlan], error) {
				return ok(svc.PlanContribution(ctx, in.Body.Amount.Decimal))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/contributions", "createContribution", "Plan a contribution and store it for execution", tag, http.StatusCreated),
			func(ctx context.Context, in *Body[Amount]) (*Body[portfolio.Contribution], error) {
				return ok(svc.CreateContribution(ctx, in.Body.Amount.Decimal))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/contributions", "listContributions", "Stored contribution plans, newest first", tag),
			func(ctx context.Context, in *contributionListIn) (*Body[[]portfolio.Contribution], error) {
				return ok(svc.ListContributions(ctx, in.Limit))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/contributions/{id}", "getContribution", "A stored contribution plan", tag),
			func(ctx context.Context, in *IDPath) (*Body[portfolio.Contribution], error) {
				return ok(svc.GetContribution(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/contributions/{id}/orders/{seq}", "setContributionOrder", "Mark a plan order done, skipped or open", tag),
			func(ctx context.Context, in *orderIn) (*Body[portfolio.Contribution], error) {
				return ok(svc.SetOrder(ctx, in.ID, in.Seq, in.Body.Status, in.Body.TradeID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/contributions/{id}/discard", "discardContribution", "Discard a stored plan", tag),
			func(ctx context.Context, in *IDPath) (*Body[portfolio.Contribution], error) {
				return ok(svc.DiscardContribution(ctx, in.ID))
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
