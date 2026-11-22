// Package capm derives the per-asset CAPM cost of equity used as the default
// discount rate: risk-free rate (stored daily) + adjusted beta x equity risk
// premium (editable settings), never below rf + the minimum premium.
package capm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// CostOfEquity is a cost of equity with its inputs.
type CostOfEquity struct {
	Rate         money.Decimal `json:"rate" doc:"Cost of equity, annual fraction"`
	RiskFree     money.Decimal `json:"risk_free"`
	RiskFreeDate string        `json:"risk_free_date" doc:"Date of the risk-free observation"`
	RawBeta      money.Decimal `json:"raw_beta"`
	AdjBeta      money.Decimal `json:"adj_beta" doc:"0.67 x raw beta + 0.33"`
	BetaDate     string        `json:"beta_date" doc:"Day the beta was computed"`
	BetaIndex    string        `json:"beta_index" doc:"Benchmark the beta was computed against"`
	ERP          money.Decimal `json:"erp" doc:"Equity risk premium"`
	MinPremium   money.Decimal `json:"min_premium" doc:"Floor of the premium over the risk-free rate"`
	Floored      bool          `json:"floored" doc:"True when the floor rf + min premium set the rate"`
}

// Params supplies the editable ERP and floor.
type Params interface {
	Valuation(ctx context.Context) (settings.CAPMInputs, error)
}

// Service computes the cost of equity of assets.
type Service struct {
	q  *db.Queries
	ps Params
}

// New builds a Service.
func New(conn db.DBTX, ps Params) *Service { return &Service{q: db.New(conn), ps: ps} }

// RiskFreeCode is the risk_free_rates code of a market ('US' or 'B3').
func RiskFreeCode(market string) string {
	if market == assets.MarketUS {
		return "US"
	}
	return "B3"
}

// ForAsset returns the asset's CAPM cost of equity. ok is false (and err nil)
// when the asset has no stored beta or its market has no stored risk-free rate.
func (s *Service) ForAsset(ctx context.Context, a db.Asset) (b CostOfEquity, ok bool, err error) {
	rf, err := s.q.LatestRiskFreeRate(ctx, RiskFreeCode(a.Market))
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, nil
	}
	if err != nil {
		return b, false, fmt.Errorf("risk-free rate: %w", err)
	}
	beta, err := s.q.GetAssetBeta(ctx, a.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, nil
	}
	if err != nil {
		return b, false, fmt.Errorf("asset beta: %w", err)
	}
	rfv, err1 := decimal.NewFromString(rf.Rate)
	bv, err2 := decimal.NewFromString(beta.Beta)
	if err1 != nil || err2 != nil {
		return b, false, nil
	}
	v, err := s.ps.Valuation(ctx)
	if err != nil {
		return b, false, err
	}
	erp := v.ERPB3.Decimal
	if a.Market == assets.MarketUS {
		erp = v.ERPUS.Decimal
	}
	r := engine.CostOfEquity(rfv, bv, erp, v.MinEquityPremium.Decimal)
	return CostOfEquity{
		Rate: money.New(r.Rate), RiskFree: money.New(r.RiskFree), RiskFreeDate: rf.RefDate,
		RawBeta: money.New(r.RawBeta), AdjBeta: money.New(r.AdjBeta), BetaDate: beta.ComputedOn, BetaIndex: beta.IndexCode,
		ERP: money.New(r.ERP), MinPremium: v.MinEquityPremium, Floored: r.Floored,
	}, true, nil
}
