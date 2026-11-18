package engine

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Scenario names (spec 12.1).
const (
	ScenarioBear = "bear"
	ScenarioBase = "base"
	ScenarioBull = "bull"
)

// DCFYears is the explicit forecast horizon.
const DCFYears = 5

// DCFValuePrecision is the number of decimals per-share values are rounded to.
const DCFValuePrecision = 4

// probabilityTolerance is how far the probabilities may be from summing to 1.
var probabilityTolerance = decimal.RequireFromString("0.0001")

// Scenario is one DCF scenario. Rates are fractions (0.10 = 10%).
type Scenario struct {
	Name string `json:"name"`
	// RevenueGrowth holds the revenue growth of each of the 5 forecast years.
	RevenueGrowth [DCFYears]decimal.Decimal `json:"revenue_growth"`
	// TerminalMargin is the earnings (or FCF) margin on year-5 revenue.
	TerminalMargin decimal.Decimal `json:"terminal_margin"`
	// ExitMultiple is applied to year-5 earnings to get the terminal value.
	ExitMultiple decimal.Decimal `json:"exit_multiple"`
	// DiscountRate is the annual rate used to discount the terminal value.
	DiscountRate decimal.Decimal `json:"discount_rate"`
	// Probability is the scenario weight; the three must sum to 1.
	Probability decimal.Decimal `json:"probability"`
}

// DCFInput is a scenario DCF.
type DCFInput struct {
	// BaseRevenue is the latest annual revenue (year 0).
	BaseRevenue decimal.Decimal `json:"base_revenue"`
	// NetDebt is subtracted from the discounted terminal value (default 0).
	NetDebt decimal.Decimal `json:"net_debt"`
	// Shares outstanding.
	Shares    decimal.Decimal `json:"shares"`
	Scenarios []Scenario      `json:"scenarios"`
}

// ScenarioValue is the result for one scenario.
type ScenarioValue struct {
	Name        string          `json:"name"`
	Probability decimal.Decimal `json:"probability"`
	// Revenue5 is year-5 revenue; TerminalValue is the undiscounted exit value.
	Revenue5      decimal.Decimal `json:"revenue_year5"`
	TerminalValue decimal.Decimal `json:"terminal_value"`
	// PresentValue is the discounted terminal value (enterprise value today).
	PresentValue  decimal.Decimal `json:"present_value"`
	ValuePerShare decimal.Decimal `json:"value_per_share"`
}

// DCFResult is the probability-weighted outcome.
type DCFResult struct {
	Scenarios []ScenarioValue `json:"scenarios"`
	// ValuePerShare is sum(probability * scenario value per share).
	ValuePerShare decimal.Decimal `json:"value_per_share"`
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// Validate checks DCF inputs: exactly bear, base and bull scenarios, positive
// revenue, shares, multiples and discount rates, margins in [0, 1], and
// probabilities in [0, 1] summing to 1 (within 0.0001).
func (in DCFInput) Validate() error {
	if !in.BaseRevenue.IsPositive() {
		return invalidf("base_revenue must be positive")
	}
	if !in.Shares.IsPositive() {
		return invalidf("shares must be positive")
	}
	if len(in.Scenarios) != 3 {
		return invalidf("need exactly the bear, base and bull scenarios")
	}
	seen := map[string]bool{}
	sum := decimal.Zero
	one := decimal.NewFromInt(1)
	for _, s := range in.Scenarios {
		if s.Name != ScenarioBear && s.Name != ScenarioBase && s.Name != ScenarioBull {
			return invalidf("unknown scenario %q", s.Name)
		}
		if seen[s.Name] {
			return invalidf("duplicate scenario %q", s.Name)
		}
		seen[s.Name] = true
		for _, g := range s.RevenueGrowth {
			if g.LessThanOrEqual(one.Neg()) {
				return invalidf("%s: revenue growth must be greater than -100%%", s.Name)
			}
		}
		if s.TerminalMargin.IsNegative() || s.TerminalMargin.GreaterThan(one) {
			return invalidf("%s: terminal_margin must be between 0 and 1", s.Name)
		}
		if !s.ExitMultiple.IsPositive() {
			return invalidf("%s: exit_multiple must be positive", s.Name)
		}
		if !s.DiscountRate.IsPositive() {
			return invalidf("%s: discount_rate must be positive", s.Name)
		}
		if s.Probability.IsNegative() || s.Probability.GreaterThan(one) {
			return invalidf("%s: probability must be between 0 and 1", s.Name)
		}
		sum = sum.Add(s.Probability)
	}
	if sum.Sub(one).Abs().GreaterThan(probabilityTolerance) {
		return invalidf("probabilities sum to %s, must be 1", sum)
	}
	return nil
}

// ScenarioDCF computes the probability-weighted value per share.
//
// Per scenario: revenue is compounded for 5 years with the yearly growth
// rates, terminal earnings = revenue5 x terminal margin, terminal value =
// earnings x exit multiple, present value = terminal value / (1+r)^5, and
// value per share = (present value - net debt) / shares. Interim cash flows
// are not modelled; the exit multiple carries the value of the business.
func ScenarioDCF(in DCFInput) (DCFResult, error) {
	if err := in.Validate(); err != nil {
		return DCFResult{}, err
	}
	res := DCFResult{Scenarios: make([]ScenarioValue, 0, len(in.Scenarios))}
	one := decimal.NewFromInt(1)
	weighted := decimal.Zero
	for _, s := range in.Scenarios {
		rev := in.BaseRevenue
		for _, g := range s.RevenueGrowth {
			rev = rev.Mul(one.Add(g))
		}
		tv := rev.Mul(s.TerminalMargin).Mul(s.ExitMultiple)
		pv := tv.Div(one.Add(s.DiscountRate).Pow(decimal.NewFromInt(DCFYears)))
		vps := pv.Sub(in.NetDebt).Div(in.Shares)
		weighted = weighted.Add(vps.Mul(s.Probability))
		res.Scenarios = append(res.Scenarios, ScenarioValue{
			Name: s.Name, Probability: s.Probability, Revenue5: rev.Round(DCFValuePrecision),
			TerminalValue: tv.Round(DCFValuePrecision), PresentValue: pv.Round(DCFValuePrecision),
			ValuePerShare: vps.Round(DCFValuePrecision),
		})
	}
	res.ValuePerShare = weighted.Round(DCFValuePrecision)
	return res, nil
}
