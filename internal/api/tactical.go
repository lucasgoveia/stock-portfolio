package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
)

// TacticalPut is the body of PUT /api/assets/{id}/tactical.
type TacticalPut struct {
	NoteID      int64         `json:"note_id" doc:"Research note describing the catalyst"`
	EntryPrice  money.Decimal `json:"entry_price"`
	TargetPrice money.Decimal `json:"target_price" doc:"Used as the fair value for the tactical buy gate"`
	StopPrice   money.Decimal `json:"stop_price"`
	ExpiresOn   string        `json:"expires_on" doc:"YYYY-MM-DD; an alert fires the day after"`
}

// WithTactical mounts tactical setup and promotion routes. pf may be nil
// when only setups are needed.
func WithTactical(svc *tactical.Service, pf *portfolio.Service) Option {
	return func(s *server) {
		const tag = "tactical"
		huma.Register(s.api, op(http.MethodPut, "/api/assets/{id}/tactical", "putTacticalSetup", "Create or replace a tactical setup", tag),
			func(ctx context.Context, in *BodyIn[TacticalPut]) (*Body[tactical.Setup], error) {
				return ok(svc.Put(ctx, in.ID, tactical.Input(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/tactical", "getTacticalSetup", "The asset's tactical setup", tag),
			func(ctx context.Context, in *IDPath) (*Body[tactical.Setup], error) {
				return ok(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/assets/{id}/tactical", "deleteTacticalSetup", "Remove a tactical setup", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/assets/{id}/promote", "promoteToCore", "Promote a tactical asset to core (must pass the core buy gate)", tag),
			func(ctx context.Context, in *IDPath) (*Body[Asset], error) {
				return okAsset(pf.Promote(ctx, in.ID))
			})
	}
}
