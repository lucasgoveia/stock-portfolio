package engine

import "github.com/shopspring/decimal"

// DividendParams are the withholding rules (spec §8.5). All are settings and
// should be re-verified against current law (docs/data-sources.md §8).
type DividendParams struct {
	JCPRate         decimal.Decimal // 17.5% IRRF on JCP (LC 224/2025)
	DividendRate    decimal.Decimal // 10% IRRF on dividends ...
	DividendMonthly decimal.Decimal // ... when one payer pays more than R$50,000 in a month (Lei 15.270/2025)
	USRate          decimal.Decimal // 30% US withholding for Brazilian residents (no treaty)
}

// DefaultDividendParams returns the defaults.
func DefaultDividendParams() DividendParams {
	return DividendParams{
		JCPRate:         mustDec("0.175"),
		DividendRate:    mustDec("0.10"),
		DividendMonthly: mustDec("50000"),
		USRate:          mustDec("0.30"),
	}
}

// Dividend kinds.
const (
	DividendCash = "dividend"
	DividendJCP  = "jcp"
)

// Withholding returns the default tax withheld on a payment of gross (in
// its own currency). payerMonthBefore is what the same payer already paid
// this month in BRL; the 10% dividend rule applies to the whole month's
// amount once it passes the limit, so the shortfall on earlier payments of
// the month is added here.
func (p DividendParams) Withholding(kind, currency string, gross, payerMonthBefore decimal.Decimal) decimal.Decimal {
	if currency == "USD" {
		return gross.Mul(p.USRate).Round(2)
	}
	if kind == DividendJCP {
		return gross.Mul(p.JCPRate).Round(2)
	}
	total := payerMonthBefore.Add(gross)
	if total.LessThanOrEqual(p.DividendMonthly) {
		return zero
	}
	if payerMonthBefore.GreaterThan(p.DividendMonthly) {
		return gross.Mul(p.DividendRate).Round(2) // earlier payments were already withheld
	}
	return total.Mul(p.DividendRate).Round(2)
}

// YieldOnCost is net dividends received over the cost basis (0 when no cost).
func YieldOnCost(dividends, cost decimal.Decimal) decimal.Decimal {
	if !cost.IsPositive() {
		return zero
	}
	return dividends.Div(cost).Round(6)
}
