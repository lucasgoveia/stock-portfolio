package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// DividendCreate is the body of POST /api/assets/{id}/dividends.
type DividendCreate struct {
	Kind         string         `json:"kind" enum:"dividend,jcp"`
	RecordDate   string         `json:"record_date" doc:"Data-com, YYYY-MM-DD"`
	PayDate      string         `json:"pay_date" doc:"YYYY-MM-DD"`
	Quantity     *money.Decimal `json:"quantity,omitempty" doc:"Defaults to the shares held on the record date"`
	PerShare     money.Decimal  `json:"per_share" doc:"Gross per share, asset currency"`
	Withholding  *money.Decimal `json:"withholding,omitempty" doc:"Defaults to the rule (JCP 17.5%, dividends over R$50k/month 10%, US 30%)"`
	FXRate       *money.Decimal `json:"fx_rate,omitempty" doc:"BRL per USD on the payment date (USD only)"`
	InstrumentID int64          `json:"instrument_id" doc:"Cash instrument credited with the net amount"`
}

// DividendConfirm is the body of POST /api/dividends/{id}/confirm.
type DividendConfirm struct {
	InstrumentID int64          `json:"instrument_id"`
	PayDate      string         `json:"pay_date,omitempty"`
	Withholding  *money.Decimal `json:"withholding,omitempty"`
	FXRate       *money.Decimal `json:"fx_rate,omitempty"`
}

type dividendListIn struct {
	Status  string `query:"status" enum:"detected,confirmed,rejected,"`
	AssetID int64  `query:"asset_id" minimum:"0"`
}

// WithDividends mounts dividend routes.
func WithDividends(svc *dividends.Service) Option {
	return func(s *server) {
		const tag = "dividends"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/dividends", "recordDividend", "Record a received dividend or JCP and credit cash", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[DividendCreate]) (*Body[dividends.Dividend], error) {
				return ok(svc.Record(ctx, in.ID, dividends.RecordInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/dividends", "listDividends", "Dividends, newest first", tag),
			func(ctx context.Context, in *dividendListIn) (*Body[[]dividends.Dividend], error) {
				return ok(svc.List(ctx, dividends.Filter{Status: in.Status, AssetID: in.AssetID}))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/dividends/{id}/confirm", "confirmDividend", "Confirm a detected payment and credit cash", tag),
			func(ctx context.Context, in *BodyIn[DividendConfirm]) (*Body[dividends.Dividend], error) {
				return ok(svc.Confirm(ctx, in.ID, dividends.ConfirmInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/dividends/{id}/reject", "rejectDividend", "Dismiss a detected payment", tag),
			func(ctx context.Context, in *IDPath) (*Body[dividends.Dividend], error) {
				return ok(svc.Reject(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/dividends/{id}", "deleteDividend", "Delete a payment and its cash credit", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
	}
}
