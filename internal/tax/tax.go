// Package tax computes the B3 monthly tax (spec §11.2, §11.4) from recorded
// trades using the pure rules in the engine.
package tax

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// Service reads trades and applies the tax rules.
type Service struct {
	assets    *assets.Service
	trades    *trades.Service
	dividends *dividends.Service
	params    engine.TaxParams
	now       func() time.Time
}

// NewService builds a Service with default rules. d (optional) supplies the
// dividends of the annual report. A nil clock uses time.Now.
func NewService(a *assets.Service, t *trades.Service, d *dividends.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{assets: a, trades: t, dividends: d, params: engine.DefaultTaxParams(), now: clock}
}

// Params returns the tax rules in use.
func (s *Service) Params() engine.TaxParams { return s.params }

// Sales returns every realized B3 sale, oldest first. The part of a sale matched against
// same-day buys is a day trade at those buys' cost (spec §11.2); the rest is
// swing at the average cost.
func (s *Service) Sales(ctx context.Context) ([]engine.TaxSale, error) {
	list, err := s.assets.List(ctx, assets.Filter{Market: "B3"})
	if err != nil {
		return nil, err
	}
	var out []engine.TaxSale
	for _, a := range list {
		pos, err := s.trades.PositionAt(ctx, a.ID, "")
		if err != nil {
			return nil, err
		}
		tactical := a.Sleeve == assets.SleeveTactical
		id := strconv.FormatInt(a.ID, 10)
		for _, sl := range pos.Sales {
			if sl.DayTradeQuantity.IsPositive() {
				out = append(out, engine.TaxSale{
					AssetID: id, Date: sl.Date, Kind: a.Kind, Proceeds: sl.DayTradeProceeds,
					Gain: sl.DayTradeGainBRL, DayTrade: true, Tactical: tactical,
				})
			}
			if sl.Quantity.GreaterThan(sl.DayTradeQuantity) {
				out = append(out, engine.TaxSale{
					AssetID: id, Date: sl.Date, Kind: a.Kind, Proceeds: sl.Proceeds.Sub(sl.DayTradeProceeds),
					Gain: sl.GainBRL.Sub(sl.DayTradeGainBRL), Tactical: tactical,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// Months returns the monthly calculation for every month with B3 sales.
func (s *Service) Months(ctx context.Context) ([]engine.TaxMonth, error) {
	sales, err := s.Sales(ctx)
	if err != nil {
		return nil, err
	}
	return s.params.TaxMonths(sales), nil
}

// MonthToDate sums this month's swing-trade stock sales: proceeds, gains,
// and the proceeds from tactical positions.
type MonthToDate struct {
	Month         string
	StockSales    decimal.Decimal
	StockGains    decimal.Decimal
	TacticalSales decimal.Decimal
}

// Current returns this month's totals so far.
func (s *Service) Current(ctx context.Context) (MonthToDate, error) {
	m := MonthToDate{Month: s.now().Format("2006-01")}
	sales, err := s.Sales(ctx)
	if err != nil {
		return m, err
	}
	for _, sl := range sales {
		if sl.DayTrade || sl.Kind != "stock" || len(sl.Date) < 7 || sl.Date[:7] != m.Month {
			continue
		}
		m.StockSales = m.StockSales.Add(sl.Proceeds)
		m.StockGains = m.StockGains.Add(sl.Gain)
		if sl.Tactical {
			m.TacticalSales = m.TacticalSales.Add(sl.Proceeds)
		}
	}
	return m, nil
}
