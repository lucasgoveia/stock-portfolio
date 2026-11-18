package tax

import (
	"context"
	"sort"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
)

// ForeignRate is the annual rate on foreign applications (Lei 14.754/2023).
var ForeignRate = decimal.RequireFromString("0.15")

// YearEndPosition is a holding on 31 December with its cost basis, for the
// DIRPF "bens e direitos" section.
type YearEndPosition struct {
	AssetID  int64           `json:"asset_id"`
	Ticker   string          `json:"ticker"`
	Name     string          `json:"name"`
	Market   string          `json:"market"`
	Currency string          `json:"currency"`
	Quantity decimal.Decimal `json:"quantity"`
	Cost     decimal.Decimal `json:"cost"`     // asset currency
	CostBRL  decimal.Decimal `json:"cost_brl"` // BRL at each purchase's FX
}

// ForeignSale is a realized US sale in BRL.
type ForeignSale struct {
	AssetID  int64           `json:"asset_id"`
	Ticker   string          `json:"ticker"`
	Date     string          `json:"date"`
	Quantity decimal.Decimal `json:"quantity"`
	Proceeds decimal.Decimal `json:"proceeds"` // USD, net of fees
	GainUSD  decimal.Decimal `json:"gain_usd"`
	GainBRL  decimal.Decimal `json:"gain_brl"`
}

// IncomeLine is one dividend or JCP payment of the year.
type IncomeLine struct {
	AssetID     int64           `json:"asset_id"`
	Ticker      string          `json:"ticker"`
	Market      string          `json:"market"`
	Kind        string          `json:"kind"`
	PayDate     string          `json:"pay_date"`
	GrossBRL    decimal.Decimal `json:"gross_brl"`
	WithheldBRL decimal.Decimal `json:"withheld_brl"`
	NetBRL      decimal.Decimal `json:"net_brl"`
}

// AnnualReport is the data for the annual DIRPF (spec §11.3, §11.4).
type AnnualReport struct {
	Year      int                `json:"year"`
	Foreign   engine.ForeignYear `json:"foreign"`
	Sales     []ForeignSale      `json:"sales"`
	Income    []IncomeLine       `json:"income"`
	Positions []YearEndPosition  `json:"positions"`
	// B3 income totals: dividends are exempt; JCP is taxed at source.
	B3Dividends   decimal.Decimal `json:"b3_dividends"`
	B3JCPGross    decimal.Decimal `json:"b3_jcp_gross"`
	B3JCPWithheld decimal.Decimal `json:"b3_jcp_withheld"`
}

type foreignTotals struct{ gains, dividends, withheld decimal.Decimal }

// Annual builds the report for year. Foreign losses carry in from every
// earlier year with US activity.
func (s *Service) Annual(ctx context.Context, year int) (AnnualReport, error) {
	r := AnnualReport{Year: year, Sales: []ForeignSale{}, Income: []IncomeLine{}, Positions: []YearEndPosition{}}
	list, err := s.assets.List(ctx, assets.Filter{})
	if err != nil {
		return r, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Ticker < list[j].Ticker })
	yearEnd := strconv.Itoa(year) + "-12-31"
	byYear := map[int]*foreignTotals{}
	tot := func(y int) *foreignTotals {
		if byYear[y] == nil {
			byYear[y] = &foreignTotals{}
		}
		return byYear[y]
	}
	ticker := map[int64]string{}
	market := map[int64]string{}
	for _, a := range list {
		ticker[a.ID], market[a.ID] = a.Ticker, a.Market
		pos, err := s.trades.PositionAt(ctx, a.ID, yearEnd)
		if err != nil {
			return r, err
		}
		if pos.Quantity.IsPositive() {
			r.Positions = append(r.Positions, YearEndPosition{AssetID: a.ID, Ticker: a.Ticker, Name: a.Name, Market: a.Market,
				Currency: a.Currency, Quantity: pos.Quantity, Cost: pos.TotalCost, CostBRL: pos.TotalCostBRL})
		}
		if a.Market != assets.MarketUS {
			continue
		}
		all, err := s.trades.PositionAt(ctx, a.ID, "")
		if err != nil {
			return r, err
		}
		for _, sl := range all.Sales {
			y, _ := strconv.Atoi(sl.Date[:4])
			tot(y).gains = tot(y).gains.Add(sl.GainBRL)
			if y == year {
				r.Sales = append(r.Sales, ForeignSale{AssetID: a.ID, Ticker: a.Ticker, Date: sl.Date, Quantity: sl.Quantity,
					Proceeds: sl.Proceeds, GainUSD: sl.Gain, GainBRL: sl.GainBRL})
			}
		}
	}
	if s.dividends != nil {
		divs, err := s.dividends.List(ctx, dividends.Filter{Status: dividends.StatusConfirmed})
		if err != nil {
			return r, err
		}
		for _, d := range divs {
			day := d.RecordDate
			if d.PayDate != nil {
				day = *d.PayDate
			}
			y, _ := strconv.Atoi(day[:4])
			fx := d.FXRate.Decimal
			gross, wh := d.Gross.Mul(fx), d.Withholding.Mul(fx)
			if market[d.AssetID] == assets.MarketUS {
				tot(y).dividends = tot(y).dividends.Add(gross)
				tot(y).withheld = tot(y).withheld.Add(wh)
			}
			if y != year {
				continue
			}
			r.Income = append(r.Income, IncomeLine{AssetID: d.AssetID, Ticker: ticker[d.AssetID], Market: market[d.AssetID],
				Kind: d.Kind, PayDate: day, GrossBRL: gross, WithheldBRL: wh, NetBRL: d.NetBRL.Decimal})
			if market[d.AssetID] == assets.MarketB3 {
				if d.Kind == engine.DividendJCP {
					r.B3JCPGross, r.B3JCPWithheld = r.B3JCPGross.Add(gross), r.B3JCPWithheld.Add(wh)
				} else {
					r.B3Dividends = r.B3Dividends.Add(gross)
				}
			}
		}
	}
	sort.Slice(r.Sales, func(i, j int) bool { return r.Sales[i].Date < r.Sales[j].Date })
	sort.Slice(r.Income, func(i, j int) bool { return r.Income[i].PayDate < r.Income[j].PayDate })

	// Carry foreign losses forward year by year up to the report year.
	years := make([]int, 0, len(byYear))
	for y := range byYear {
		if y < year {
			years = append(years, y)
		}
	}
	sort.Ints(years)
	loss := decimal.Zero
	for _, y := range years {
		t := byYear[y]
		loss = engine.ForeignTax(y, ForeignRate, t.gains, t.dividends, t.withheld, loss).LossOut
	}
	t := tot(year)
	r.Foreign = engine.ForeignTax(year, ForeignRate, t.gains, t.dividends, t.withheld, loss)
	return r, nil
}
