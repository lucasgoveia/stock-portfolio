package engine

import "github.com/shopspring/decimal"

// CAPM constants: the Blume adjustment pulls a raw beta a third of the way to 1.
var (
	blumeWeight = decimal.RequireFromString("0.67")
	blumeAnchor = decimal.RequireFromString("0.33")
)

// CostOfEquityPlaces is the rounding of the rate and the adjusted beta.
const CostOfEquityPlaces = 4

// CostOfEquityResult is a CAPM cost of equity with its breakdown.
type CostOfEquityResult struct {
	Rate     decimal.Decimal // annual, as a fraction, rounded to 4 places
	RiskFree decimal.Decimal
	RawBeta  decimal.Decimal
	AdjBeta  decimal.Decimal // 0.67 x raw + 0.33
	ERP      decimal.Decimal
	// Floored is true when rf + adjusted beta x ERP was below rf + minPremium
	// and the floor became the rate.
	Floored bool
}

// AdjustBeta applies the Blume adjustment: 0.67 x raw + 0.33.
func AdjustBeta(raw decimal.Decimal) decimal.Decimal {
	return raw.Mul(blumeWeight).Add(blumeAnchor)
}

// CostOfEquity returns rf + adjusted beta x erp, but never less than
// rf + minPremium. All values are fractions except beta.
func CostOfEquity(rf, beta, erp, minPremium decimal.Decimal) CostOfEquityResult {
	adj := AdjustBeta(beta)
	rate := rf.Add(adj.Mul(erp))
	floor := rf.Add(minPremium)
	floored := rate.LessThan(floor)
	if floored {
		rate = floor
	}
	return CostOfEquityResult{
		Rate: rate.Round(CostOfEquityPlaces), RiskFree: rf, RawBeta: beta,
		AdjBeta: adj.Round(CostOfEquityPlaces), ERP: erp, Floored: floored,
	}
}
