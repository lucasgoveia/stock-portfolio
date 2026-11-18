package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// TradeCreate is the body of POST /api/assets/{id}/trades.
type TradeCreate struct {
	Date     string         `json:"date" doc:"YYYY-MM-DD"`
	Side     string         `json:"side" doc:"buy or sell"`
	Quantity money.Decimal  `json:"quantity"`
	Price    money.Decimal  `json:"price"`
	Fees     *money.Decimal `json:"fees,omitempty" doc:"Defaults to 0"`
	Currency string         `json:"currency" required:"false" doc:"Defaults to the asset's currency"`
	FXRate   *money.Decimal `json:"fx_rate,omitempty" doc:"USDBRL; required for USD trades"`
}

// WithTrades mounts trade and position routes.
func WithTrades(svc *trades.Service) Option {
	return func(s *server) {
		const tag = "trades"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/trades", "createTrade", "Record a trade", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[TradeCreate]) (*Body[trades.Trade], error) {
				return ok(svc.Create(ctx, in.ID, trades.CreateInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/trades", "listTrades", "List an asset's trades", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]trades.Trade], error) {
				return ok(svc.List(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/position", "getPosition", "Position derived from trades", tag),
			func(ctx context.Context, in *IDPath) (*Body[trades.Position], error) {
				return ok(svc.Position(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/positions", "listPositions", "All open positions", tag),
			func(ctx context.Context, _ *struct{}) (*Body[[]trades.Position], error) {
				return ok(svc.Positions(ctx))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/trades/{id}", "deleteTrade", "Delete a trade", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
	}
}
