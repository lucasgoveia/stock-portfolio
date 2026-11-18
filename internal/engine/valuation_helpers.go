package engine

import (
	"github.com/shopspring/decimal"
)

// Valuation helpers (spec §12.1): reverse DCF, Bazin and Graham. They are
// informational or seed a manual fair value version; none of them replaces the
// scenario DCF.

// DefaultBazinYield is the required dividend yield of the Bazin method (6%).
var DefaultBazinYield = decimal.RequireFromString("0.06")

// grahamMultiplier is Graham's 22.5 (P/E 15 times P/B 1.5).
var grahamMultiplier = decimal.RequireFromString("22.5")

// Bazin returns trailing-12-month dividends per share divided by the required
// yield. ok is false when dividends or the yield are not positive.
func Bazin(dividendsPerShare, requiredYield decimal.Decimal) (decimal.Decimal, bool) {
	if !dividendsPerShare.IsPositive() || !requiredYield.IsPositive() {
		return zero, false
	}
	return dividendsPerShare.Div(requiredYield).Round(DCFValuePrecision), true
}

// Graham returns sqrt(22.5 x EPS x BVPS). ok is false (the value is undefined)
// when EPS or book value per share is zero or negative.
func Graham(eps, bvps decimal.Decimal) (decimal.Decimal, bool) {
	if !eps.IsPositive() || !bvps.IsPositive() {
		return zero, false
	}
	return sqrt(grahamMultiplier.Mul(eps).Mul(bvps)).Round(DCFValuePrecision), true
}

// sqrt is Newton's method on decimals, for x > 0.
func sqrt(x decimal.Decimal) decimal.Decimal {
	if !x.IsPositive() {
		return zero
	}
	two := decimal.NewFromInt(2)
	g := x
	if g.GreaterThan(decimal.NewFromInt(1)) {
		g = x.Div(two)
	}
	for i := 0; i < 100; i++ {
		next := g.Add(x.Div(g)).Div(two)
		if next.Sub(g).Abs().LessThan(decimal.New(1, -20)) {
			return next
		}
		g = next
	}
	return g
}

// ReverseDCFInput describes the market-implied growth problem: which constant
// yearly revenue growth over 5 years makes the scenario DCF formula equal the
// current price, given a terminal margin, an exit multiple and a discount rate.
type ReverseDCFInput struct {
	Price          decimal.Decimal `json:"price"`
	BaseRevenue    decimal.Decimal `json:"base_revenue"`
	NetDebt        decimal.Decimal `json:"net_debt"`
	Shares         decimal.Decimal `json:"shares"`
	TerminalMargin decimal.Decimal `json:"terminal_margin"`
	ExitMultiple   decimal.Decimal `json:"exit_multiple"`
	DiscountRate   decimal.Decimal `json:"discount_rate"`
}

// Reverse DCF outcomes.
const (
	ReverseOK         = "ok"
	ReverseBelowRange = "below_range" // price is below what the lowest growth explains
	ReverseAboveRange = "above_range" // price is above what the highest growth explains
)

// Search bounds for the yearly growth rate.
var (
	reverseLow  = decimal.RequireFromString("-0.5")
	reverseHigh = decimal.RequireFromString("1.5")
	reverseTol  = decimal.RequireFromString("0.000001")
)

// ReverseDCFResult is the implied growth. ImpliedGrowth is a fraction
// (0.08 = 8% a year) and is only meaningful when Status is ReverseOK.
type ReverseDCFResult struct {
	Status        string          `json:"status" enum:"ok,below_range,above_range"`
	ImpliedGrowth decimal.Decimal `json:"implied_growth"`
	// ValuePerShare is the formula's value at ImpliedGrowth (close to Price).
	ValuePerShare decimal.Decimal `json:"value_per_share"`
}

// Validate checks the inputs.
func (in ReverseDCFInput) Validate() error {
	switch {
	case !in.Price.IsPositive():
		return invalidf("price must be positive")
	case !in.BaseRevenue.IsPositive():
		return invalidf("base_revenue must be positive")
	case !in.Shares.IsPositive():
		return invalidf("shares must be positive")
	case !in.TerminalMargin.IsPositive() || in.TerminalMargin.GreaterThan(decimal.NewFromInt(1)):
		return invalidf("terminal_margin must be in (0, 1]")
	case !in.ExitMultiple.IsPositive():
		return invalidf("exit_multiple must be positive")
	case !in.DiscountRate.IsPositive():
		return invalidf("discount_rate must be positive")
	}
	return nil
}

// ReverseDCF solves, by bisection, the constant 5-year revenue growth at which
// the scenario DCF value per share (same formula as ScenarioDCF) equals the
// price. The value rises with growth, so the root is unique.
func ReverseDCF(in ReverseDCFInput) (ReverseDCFResult, error) {
	if err := in.Validate(); err != nil {
		return ReverseDCFResult{}, err
	}
	value := func(g decimal.Decimal) decimal.Decimal {
		s := Scenario{TerminalMargin: in.TerminalMargin, ExitMultiple: in.ExitMultiple, DiscountRate: in.DiscountRate}
		for i := range s.RevenueGrowth {
			s.RevenueGrowth[i] = g
		}
		_, _, _, vps := scenarioValue(in.BaseRevenue, in.NetDebt, in.Shares, s)
		return vps
	}
	lo, hi := reverseLow, reverseHigh
	if vlo := value(lo); in.Price.LessThan(vlo) {
		return ReverseDCFResult{Status: ReverseBelowRange, ImpliedGrowth: lo, ValuePerShare: vlo.Round(DCFValuePrecision)}, nil
	}
	if vhi := value(hi); in.Price.GreaterThan(vhi) {
		return ReverseDCFResult{Status: ReverseAboveRange, ImpliedGrowth: hi, ValuePerShare: vhi.Round(DCFValuePrecision)}, nil
	}
	two := decimal.NewFromInt(2)
	mid := lo
	var v decimal.Decimal
	for i := 0; i < 100; i++ {
		mid = lo.Add(hi).Div(two)
		v = value(mid)
		diff := v.Sub(in.Price)
		if diff.Abs().LessThan(reverseTol) || hi.Sub(lo).LessThan(decimal.New(1, -12)) {
			break
		}
		if diff.IsNegative() {
			lo = mid
		} else {
			hi = mid
		}
	}
	return ReverseDCFResult{Status: ReverseOK, ImpliedGrowth: mid.Round(6), ValuePerShare: v.Round(DCFValuePrecision)}, nil
}
