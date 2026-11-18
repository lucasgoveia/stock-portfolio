package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// AsOfQuery is the as_of query parameter.
type AsOfQuery struct {
	AsOf string `query:"as_of" doc:"YYYY-MM-DD; defaults to today"`
}

type instrumentValueIn struct {
	IDPath
	AsOfQuery
}

// InstrumentCreate is the body of POST /api/cash/instruments.
type InstrumentCreate struct {
	Kind   string         `json:"kind" doc:"tesouro_selic, cdi or usd_cash"`
	Name   string         `json:"name" required:"false"`
	PctCDI *money.Decimal `json:"pct_cdi,omitempty" doc:"Percent of CDI (cdi kind only, default 100)"`
}

// MovementCreate is the body of POST /api/cash/instruments/{id}/movements.
type MovementCreate struct {
	Date     string         `json:"date" required:"false" doc:"YYYY-MM-DD; defaults to today"`
	Type     string         `json:"type" doc:"deposit, withdraw or yield"`
	Amount   money.Decimal  `json:"amount"`
	Quantity *money.Decimal `json:"quantity,omitempty" doc:"Title units (Tesouro Selic)"`
}

// WithCash mounts the cash instrument routes.
func WithCash(svc *cash.Service) Option {
	return func(s *server) {
		const tag = "cash"
		huma.Register(s.api, op(http.MethodGet, "/api/cash/summary", "cashSummary", "Total cash with the BRL/USD split", tag),
			func(ctx context.Context, in *AsOfQuery) (*Body[cash.Summary], error) {
				return ok(svc.Summary(ctx, in.AsOf))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/cash/instruments", "createInstrument", "Create a cash instrument", tag, http.StatusCreated),
			func(ctx context.Context, in *Body[InstrumentCreate]) (*Body[cash.Instrument], error) {
				return ok(svc.Create(ctx, cash.CreateInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/cash/instruments", "listInstruments", "List cash instruments", tag),
			func(ctx context.Context, _ *struct{}) (*Body[[]cash.Instrument], error) {
				return ok(svc.List(ctx))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/cash/instruments/{id}/value", "instrumentValue", "Value an instrument at a date", tag),
			func(ctx context.Context, in *instrumentValueIn) (*Body[cash.Valuation], error) {
				return ok(svc.Value(ctx, in.ID, in.AsOf))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/cash/instruments/{id}/movements", "listMovements", "List an instrument's ledger", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]cash.Movement], error) {
				return ok(svc.Movements(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/cash/instruments/{id}/movements", "addMovement", "Add a deposit, withdrawal or yield", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[MovementCreate]) (*Body[cash.Movement], error) {
				return ok(svc.AddMovement(ctx, in.ID, cash.MovementInput(in.Body)))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/cash/movements/{id}", "deleteMovement", "Delete a ledger entry", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.DeleteMovement(ctx, in.ID))
			})
	}
}
