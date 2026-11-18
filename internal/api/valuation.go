package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// FairValueCreate is the body of POST /api/assets/{id}/fair-values.
type FairValueCreate struct {
	Method        string           `json:"method" doc:"scenario_dcf, bazin, graham, manual or target_price"`
	ValuePerShare *money.Decimal   `json:"value_per_share,omitempty" doc:"Required unless method is scenario_dcf"`
	DCF           *engine.DCFInput `json:"dcf,omitempty" doc:"Required when method is scenario_dcf"`
	Currency      string           `json:"currency" required:"false" doc:"Defaults to the asset's currency"`
	AsOf          string           `json:"as_of" required:"false" doc:"YYYY-MM-DD; defaults to today"`
	Sources       []string         `json:"sources,omitempty"`
	Note          string           `json:"note" required:"false"`
	Inputs        map[string]any   `json:"inputs,omitempty" doc:"How a bazin or graham value was computed (stored with the version)"`
}

// WithValuation mounts fair value routes.
func WithValuation(svc *valuation.Service) Option {
	return func(s *server) {
		const tag = "valuation"
		huma.Register(s.api, op(http.MethodPost, "/api/valuation/dcf", "previewDCF", "Compute a scenario DCF without saving", tag),
			func(_ context.Context, in *Body[engine.DCFInput]) (*Body[engine.DCFResult], error) {
				return ok(valuation.Preview(in.Body))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/fair-values", "createFairValue", "Add a fair value version", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[FairValueCreate]) (*Body[valuation.FairValue], error) {
				return ok(svc.Create(ctx, in.ID, valuation.CreateInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/fair-values", "listFairValues", "Fair value versions, newest first", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]valuation.FairValue], error) {
				return ok(svc.List(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/fair-values/latest", "latestFairValue", "Current fair value", tag),
			func(ctx context.Context, in *IDPath) (*Body[valuation.FairValue], error) {
				return ok(svc.Latest(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/fair-values/{id}", "deleteFairValue", "Delete a fair value version", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
	}
}
