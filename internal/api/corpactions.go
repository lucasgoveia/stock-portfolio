package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// ActionCreate is the body of POST /api/assets/{id}/corporate-actions.
type ActionCreate struct {
	Kind         string         `json:"kind" enum:"split,reverse_split,bonus"`
	ExDate       string         `json:"ex_date" doc:"First trading day without the right, YYYY-MM-DD"`
	Factor       money.Decimal  `json:"factor" doc:"Shares multiplier: 2 for 2-for-1, 0.1 for 1-for-10, 1.1 for a 10% bonus"`
	CostPerShare *money.Decimal `json:"cost_per_share,omitempty" doc:"Bonus only: cost per new share declared by the company"`
	Note         string         `json:"note,omitempty"`
}

// ActionConfirm is the body of POST /api/corporate-actions/{id}/confirm.
type ActionConfirm struct {
	CostPerShare *money.Decimal `json:"cost_per_share,omitempty" doc:"Required for a bonus"`
}

type actionListIn struct {
	Status  string `query:"status" enum:"detected,confirmed,rejected,"`
	AssetID int64  `query:"asset_id" minimum:"0"`
}

// WithCorporateActions mounts the corporate action routes.
func WithCorporateActions(svc *corpactions.Service) Option {
	return func(s *server) {
		const tag = "corporate-actions"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/corporate-actions", "recordCorporateAction", "Enter a split, reverse split or bonus by hand", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[ActionCreate]) (*Body[corpactions.Action], error) {
				return ok(svc.Record(ctx, in.ID, corpactions.Input(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/corporate-actions", "listCorporateActions", "Corporate actions, newest first", tag),
			func(ctx context.Context, in *actionListIn) (*Body[[]corpactions.Action], error) {
				return ok(svc.List(ctx, in.Status, in.AssetID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/corporate-actions/{id}/confirm", "confirmCorporateAction", "Apply a detected action to the position", tag),
			func(ctx context.Context, in *BodyIn[ActionConfirm]) (*Body[corpactions.Action], error) {
				return ok(svc.Confirm(ctx, in.ID, in.Body.CostPerShare))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/corporate-actions/{id}/reject", "rejectCorporateAction", "Dismiss a detected action", tag),
			func(ctx context.Context, in *IDPath) (*Body[corpactions.Action], error) {
				return ok(svc.Reject(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/corporate-actions/{id}", "deleteCorporateAction", "Delete an action (refused when later sales need it)", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
	}
}
