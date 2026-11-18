package engine

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// TaxParams are the B3 tax rules (spec §11.2). All are settings.
type TaxParams struct {
	ExemptionLimit decimal.Decimal `json:"exemption_limit"` // R$20,000 of monthly stock sales
	SwingRate      decimal.Decimal `json:"swing_rate"`      // 15%
	DayTradeRate   decimal.Decimal `json:"day_trade_rate"`  // 20%
	DARFMin        decimal.Decimal `json:"darf_min"`        // R$10: smaller amounts roll into the next month
	DARFCode       string          `json:"darf_code"`       // 6015 (swing trade)
	// DayTradeDARFCode is the day-trade DARF code, a setting. It defaults to
	// 6015 but 6047 is also cited (docs/data-sources.md §8), so it stays
	// unconfirmed until a contador confirms it.
	DayTradeDARFCode          string `json:"day_trade_darf_code"`
	DayTradeDARFCodeConfirmed bool   `json:"day_trade_darf_code_confirmed"`
}

// DefaultTaxParams returns the spec defaults.
func DefaultTaxParams() TaxParams {
	return TaxParams{
		ExemptionLimit: mustDec("20000"),
		SwingRate:      mustDec("0.15"),
		DayTradeRate:   mustDec("0.20"),
		DARFMin:        mustDec("10"),
		DARFCode:       "6015",

		DayTradeDARFCode: "6015", DayTradeDARFCodeConfirmed: false,
	}
}

// TaxSale is one realized B3 sale in BRL.
type TaxSale struct {
	AssetID string `json:"asset_id"`
	Date    string `json:"date"` // YYYY-MM-DD
	// Kind is the asset kind; only "stock" sales count toward and enjoy the
	// monthly exemption. BDR and ETF gains are always taxable.
	Kind     string          `json:"kind"`
	Proceeds decimal.Decimal `json:"proceeds"` // net of fees
	Gain     decimal.Decimal `json:"gain"`
	DayTrade bool            `json:"day_trade"`
	Tactical bool            `json:"tactical"`
}

// TaxMonth is the monthly calculation (spec §11.4).
type TaxMonth struct {
	Month string `json:"month"` // YYYY-MM
	// StockSales is the swing-trade stock proceeds the exemption tests.
	StockSales decimal.Decimal `json:"stock_sales"`
	Exempt     bool            `json:"exempt"`
	// SwingResult is the month's taxable-nature swing result before losses:
	// non-stock gains plus stock gains, with exempt stock gains left out (an
	// exempt month's net stock loss still counts).
	SwingResult  decimal.Decimal `json:"swing_result"`
	ExemptGain   decimal.Decimal `json:"exempt_gain"`
	SwingLossIn  decimal.Decimal `json:"swing_loss_in"`
	SwingLossOut decimal.Decimal `json:"swing_loss_out"`
	SwingTaxable decimal.Decimal `json:"swing_taxable"`
	DayResult    decimal.Decimal `json:"day_trade_result"`
	DayLossIn    decimal.Decimal `json:"day_trade_loss_in"`
	DayLossOut   decimal.Decimal `json:"day_trade_loss_out"`
	DayTaxable   decimal.Decimal `json:"day_trade_taxable"`
	// Tax is this month's tax; CarriedIn is unpaid tax from earlier months
	// below the DARF minimum.
	Tax       decimal.Decimal `json:"tax"`
	CarriedIn decimal.Decimal `json:"carried_in"`
	// DARF is what to pay (0 when below the minimum, which rolls forward).
	DARF     decimal.Decimal `json:"darf"`
	DARFCode string          `json:"darf_code"`
	DueDate  string          `json:"due_date"`
	Sales    []TaxSale       `json:"sales"`
}

// TaxMonths computes every month with sales, oldest first, carrying swing
// and day-trade losses separately and rolling sub-minimum DARFs forward.
func (t TaxParams) TaxMonths(sales []TaxSale) []TaxMonth {
	byMonth := map[string][]TaxSale{}
	for _, s := range sales {
		if len(s.Date) < 7 {
			continue
		}
		m := s.Date[:7]
		byMonth[m] = append(byMonth[m], s)
	}
	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)

	out := make([]TaxMonth, 0, len(months))
	swingLoss, dayLoss, carried := zero, zero, zero
	for _, m := range months {
		r := TaxMonth{Month: m, DARFCode: t.DARFCode, DueDate: DARFDueDate(m), Sales: byMonth[m],
			StockSales: zero, SwingResult: zero, ExemptGain: zero, DayResult: zero, DARF: zero}
		stockNet, other := zero, zero
		for _, s := range r.Sales {
			switch {
			case s.DayTrade:
				r.DayResult = r.DayResult.Add(s.Gain)
			case s.Kind == "stock":
				r.StockSales = r.StockSales.Add(s.Proceeds)
				stockNet = stockNet.Add(s.Gain)
			default:
				other = other.Add(s.Gain)
			}
		}
		r.Exempt = r.StockSales.LessThanOrEqual(t.ExemptionLimit)
		if r.Exempt && stockNet.IsPositive() {
			r.ExemptGain = stockNet
			stockNet = zero
		}
		r.SwingResult = other.Add(stockNet)
		r.SwingLossIn, r.DayLossIn = swingLoss, dayLoss
		r.SwingTaxable, swingLoss = offset(r.SwingResult, swingLoss)
		r.DayTaxable, dayLoss = offset(r.DayResult, dayLoss)
		r.SwingLossOut, r.DayLossOut = swingLoss, dayLoss
		r.Tax = r.SwingTaxable.Mul(t.SwingRate).Add(r.DayTaxable.Mul(t.DayTradeRate)).Round(2)
		r.CarriedIn = carried
		due := r.Tax.Add(carried)
		if due.GreaterThanOrEqual(t.DARFMin) {
			r.DARF, carried = due, zero
		} else {
			carried = due
		}
		out = append(out, r)
	}
	return out
}

// offset applies a carried loss to a month's result and returns the taxable
// amount and the loss carried forward.
func offset(result, lossIn decimal.Decimal) (taxable, lossOut decimal.Decimal) {
	if result.IsNegative() {
		return zero, lossIn.Add(result.Neg())
	}
	if result.GreaterThan(lossIn) {
		return result.Sub(lossIn), zero
	}
	return zero, lossIn.Sub(result)
}

// DARFDueDate is the last business day (Monday to Friday) of the month after
// month (YYYY-MM): not a weekend and not a national bank holiday.
func DARFDueDate(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return ""
	}
	d := t.AddDate(0, 2, -1) // last day of the following month
	for !IsBusinessDay(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d.Format("2006-01-02")
}

// TrimTax is the tax effect of selling a trim this month.
type TrimTax struct {
	AssetID string `json:"asset_id"`
	// MonthSales is this month's stock sales so far; after adds the trim.
	MonthSales      decimal.Decimal `json:"month_sales"`
	MonthSalesAfter decimal.Decimal `json:"month_sales_after"`
	// ExceedsExemption is true when the trim takes the month over the limit.
	ExceedsExemption bool `json:"exceeds_exemption"`
	// EstimatedTax is the month's swing tax if the trim is sold now (stock
	// gains of the whole month become taxable), before carried losses.
	EstimatedTax decimal.Decimal `json:"estimated_tax"`
	// FitQuantity is how many shares still fit under the limit this month;
	// the rest can wait for next month.
	FitQuantity decimal.Decimal `json:"fit_quantity"`
}

// TrimTaxImpact estimates the tax of selling qty at price (gain per share =
// price - avgCost) given this month's stock sales and stock gains so far.
// Only stock sales use the exemption; other kinds always pay the swing rate.
func (t TaxParams) TrimTaxImpact(assetID, kind string, qty, price, avgCost, monthSales, monthGains decimal.Decimal) TrimTax {
	proceeds := qty.Mul(price)
	gain := price.Sub(avgCost).Mul(qty)
	r := TrimTax{AssetID: assetID, MonthSales: monthSales, MonthSalesAfter: monthSales, EstimatedTax: zero, FitQuantity: qty}
	if kind != "stock" {
		r.EstimatedTax = maxDec(zero, gain).Mul(t.SwingRate).Round(2)
		return r
	}
	r.MonthSalesAfter = monthSales.Add(proceeds)
	r.ExceedsExemption = r.MonthSalesAfter.GreaterThan(t.ExemptionLimit)
	if r.ExceedsExemption {
		r.EstimatedTax = maxDec(zero, monthGains.Add(gain)).Mul(t.SwingRate).Round(2)
		room := t.ExemptionLimit.Sub(monthSales)
		r.FitQuantity = zero
		if room.IsPositive() && price.IsPositive() {
			r.FitQuantity = room.Div(price).Floor()
			if r.FitQuantity.GreaterThan(qty) {
				r.FitQuantity = qty
			}
		}
	}
	return r
}

func maxDec(a, b decimal.Decimal) decimal.Decimal {
	if a.GreaterThan(b) {
		return a
	}
	return b
}
