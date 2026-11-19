package engine

import (
	"fmt"
	"strings"

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

// Scenario bases (spec 12.1). The basis fixes what the margin and the exit
// multiple mean, and whether net debt is subtracted.
const (
	// BasisEBITEV: EBIT margin x EV/EBIT exit multiple gives an enterprise
	// value; net debt is subtracted to reach equity.
	BasisEBITEV = "ebit_ev"
	// BasisNetPE: net margin x P/E exit multiple gives equity value directly;
	// net debt is not subtracted (it is already in the net income).
	BasisNetPE = "net_pe"
)

// MaxInitiatives bounds the new-business list of one version.
const MaxInitiatives = 10

// Scenario is one DCF scenario. Rates are fractions (0.10 = 10%).
type Scenario struct {
	Name string `json:"name"`
	// RevenueGrowth holds the revenue growth of each of the 5 forecast years.
	RevenueGrowth [DCFYears]decimal.Decimal `json:"revenue_growth"`
	// TerminalMargin is the earnings margin on year-5 revenue: EBIT margin for
	// ebit_ev, net margin for net_pe.
	TerminalMargin decimal.Decimal `json:"terminal_margin"`
	// ExitMultiple is applied to year-5 earnings to get the terminal value:
	// EV/EBIT for ebit_ev, P/E for net_pe.
	ExitMultiple decimal.Decimal `json:"exit_multiple"`
	// DiscountRate is the annual rate used to discount every cash flow.
	DiscountRate decimal.Decimal `json:"discount_rate"`
	// Probability is the scenario weight; the three must sum to 1.
	Probability decimal.Decimal `json:"probability"`
	// Basis is ebit_ev or net_pe. Empty means ebit_ev, which is how versions
	// stored before the field existed are read.
	Basis string `json:"basis,omitempty" enum:"ebit_ev,net_pe" doc:"Empty means ebit_ev"`
	// StartMargin is the margin at year 0; the margin path is linear from it to
	// TerminalMargin over the 5 years. Nil means the terminal margin throughout.
	StartMargin *decimal.Decimal `json:"start_margin,omitempty"`
	// TaxRate converts EBIT to after-tax earnings for the interim cash (ebit_ev
	// only) and the expected EPS. Fraction in [0, 1).
	TaxRate decimal.Decimal `json:"tax_rate" required:"false"`
	// CashConversion is the fraction of each year's earnings returned or
	// accumulated for shareholders. Fraction in [0, 1]; 0 models no interim cash.
	CashConversion decimal.Decimal `json:"cash_conversion" required:"false"`
}

// EffectiveBasis returns the basis, reading an empty one as ebit_ev.
func (s Scenario) EffectiveBasis() string {
	if s.Basis == "" {
		return BasisEBITEV
	}
	return s.Basis
}

// InitiativeProbabilities is the success probability of an initiative in each
// scenario. Fractions in [0, 1].
type InitiativeProbabilities struct {
	Bear decimal.Decimal `json:"bear"`
	Base decimal.Decimal `json:"base"`
	Bull decimal.Decimal `json:"bull"`
}

// For returns the probability for a scenario name; unknown names read as base.
func (p InitiativeProbabilities) For(scenario string) decimal.Decimal {
	switch scenario {
	case ScenarioBear:
		return p.Bear
	case ScenarioBull:
		return p.Bull
	default:
		return p.Base
	}
}

// Initiative is a discrete new business (a product, segment, capex program or
// market entry) that history does not show. It is added to every scenario,
// weighted by that scenario's success probability.
type Initiative struct {
	Name      string   `json:"name"`
	Rationale string   `json:"rationale" required:"false"`
	Sources   []string `json:"sources" required:"false"`
	// StartYear is the first year (1 to 5) in which the initiative earns revenue.
	StartYear int `json:"start_year"`
	// RevenueYear5 is the initiative's revenue in year 5 if it succeeds.
	RevenueYear5 decimal.Decimal `json:"revenue_year5"`
	// Margin is the initiative's earnings margin on the scenario's basis (EBIT
	// margin for ebit_ev, net margin for net_pe), constant over its life.
	Margin             decimal.Decimal         `json:"margin"`
	SuccessProbability InitiativeProbabilities `json:"success_probability"`
}

// RevenueInYear is the initiative's revenue in year t (1 to 5) if it succeeds.
// It ramps linearly from the start year to year 5: zero before StartYear, then
// RevenueYear5 x (t - StartYear + 1) / (5 - StartYear + 1), so it reaches
// RevenueYear5 in year 5 and earns RevenueYear5 / (6 - StartYear) in its first
// year.
func (i Initiative) RevenueInYear(t int) decimal.Decimal {
	if t < i.StartYear || t > DCFYears {
		return decimal.Zero
	}
	return i.RevenueYear5.Mul(decimal.NewFromInt(int64(t - i.StartYear + 1))).
		Div(decimal.NewFromInt(int64(DCFYears - i.StartYear + 1)))
}

// DCFInput is a scenario DCF.
type DCFInput struct {
	// BaseRevenue is the latest annual revenue (year 0).
	BaseRevenue decimal.Decimal `json:"base_revenue"`
	// NetDebt is subtracted from the discounted value of ebit_ev scenarios
	// (default 0). net_pe scenarios do not subtract it.
	NetDebt decimal.Decimal `json:"net_debt" required:"false"`
	// Shares outstanding.
	Shares    decimal.Decimal `json:"shares"`
	Scenarios []Scenario      `json:"scenarios"`
	// Initiatives are new businesses outside the historical revenue.
	Initiatives []Initiative `json:"initiatives,omitempty"`
}

// ScenarioValue is the result for one scenario. Amounts are totals (not per
// share) except the *_per_share fields.
type ScenarioValue struct {
	Name        string          `json:"name"`
	Probability decimal.Decimal `json:"probability"`
	// Revenue5 is the core year-5 revenue (without initiatives); TerminalValue is
	// the undiscounted exit value of core and initiatives together.
	Revenue5      decimal.Decimal `json:"revenue_year5"`
	TerminalValue decimal.Decimal `json:"terminal_value"`
	// PresentValue is the discounted terminal value plus the discounted interim
	// cash, core and initiatives, before net debt.
	PresentValue  decimal.Decimal `json:"present_value"`
	ValuePerShare decimal.Decimal `json:"value_per_share"`
	// Basis is the basis the scenario was computed with (ebit_ev or net_pe).
	Basis string `json:"basis" enum:"ebit_ev,net_pe"`
	// InterimPresentValue is the discounted interim cash, core and initiatives.
	InterimPresentValue decimal.Decimal `json:"interim_present_value"`
	// InitiativesRevenue5 is the probability-weighted year-5 revenue of the
	// initiatives.
	InitiativesRevenue5 decimal.Decimal `json:"initiatives_revenue_year5"`
	// CoreValuePerShare and InitiativesValuePerShare split ValuePerShare: the
	// initiatives' probability-weighted value and everything else (net debt
	// included). They sum to ValuePerShare up to rounding.
	CoreValuePerShare        decimal.Decimal `json:"core_value_per_share"`
	InitiativesValuePerShare decimal.Decimal `json:"initiatives_value_per_share"`
}

// DCFResult is the probability-weighted outcome.
type DCFResult struct {
	Scenarios []ScenarioValue `json:"scenarios"`
	// ValuePerShare is sum(probability * scenario value per share).
	ValuePerShare decimal.Decimal `json:"value_per_share"`
	// CoreValuePerShare and InitiativesValuePerShare are the probability-weighted
	// split of ValuePerShare.
	CoreValuePerShare        decimal.Decimal `json:"core_value_per_share"`
	InitiativesValuePerShare decimal.Decimal `json:"initiatives_value_per_share"`
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
		if err := s.validateBasisFields(); err != nil {
			return err
		}
		sum = sum.Add(s.Probability)
	}
	if sum.Sub(one).Abs().GreaterThan(probabilityTolerance) {
		return invalidf("probabilities sum to %s, must be 1", sum)
	}
	return validateInitiatives(in.Initiatives)
}

// validateBasisFields checks the basis, start margin, tax rate and cash
// conversion of a scenario.
func (s Scenario) validateBasisFields() error {
	one := decimal.NewFromInt(1)
	if s.Basis != "" && s.Basis != BasisEBITEV && s.Basis != BasisNetPE {
		return invalidf("%s: basis must be ebit_ev or net_pe", s.Name)
	}
	if s.StartMargin != nil && (s.StartMargin.IsNegative() || s.StartMargin.GreaterThan(one)) {
		return invalidf("%s: start_margin must be between 0 and 1", s.Name)
	}
	if s.TaxRate.IsNegative() || s.TaxRate.GreaterThanOrEqual(one) {
		return invalidf("%s: tax_rate must be at least 0 and below 1", s.Name)
	}
	if s.CashConversion.IsNegative() || s.CashConversion.GreaterThan(one) {
		return invalidf("%s: cash_conversion must be between 0 and 1", s.Name)
	}
	return nil
}

// validateInitiatives checks an initiative list: at most MaxInitiatives, each
// with a name, a start year in 1..5, positive year-5 revenue, a margin in
// [0, 1] (negative margins are not modelled: a loss-making line would need a
// year-by-year margin path, which this single constant margin cannot express)
// and success probabilities in [0, 1].
func validateInitiatives(list []Initiative) error {
	if len(list) > MaxInitiatives {
		return invalidf("at most %d initiatives", MaxInitiatives)
	}
	one := decimal.NewFromInt(1)
	for n, i := range list {
		label := fmt.Sprintf("initiative %d", n+1)
		if strings.TrimSpace(i.Name) == "" {
			return invalidf("%s needs a name", label)
		}
		if i.StartYear < 1 || i.StartYear > DCFYears {
			return invalidf("%s: start_year must be between 1 and %d", label, DCFYears)
		}
		if !i.RevenueYear5.IsPositive() {
			return invalidf("%s: revenue_year5 must be positive", label)
		}
		if i.Margin.IsNegative() || i.Margin.GreaterThan(one) {
			return invalidf("%s: margin must be between 0 and 1", label)
		}
		for _, p := range []decimal.Decimal{i.SuccessProbability.Bear, i.SuccessProbability.Base, i.SuccessProbability.Bull} {
			if p.IsNegative() || p.GreaterThan(one) {
				return invalidf("%s: success probabilities must be between 0 and 1", label)
			}
		}
	}
	return nil
}

// ScenarioDCF computes the probability-weighted value per share.
//
// Per scenario, with r the discount rate and t = 1..5 the forecast year:
//
//	revenue_t        = revenue_(t-1) x (1 + g_t), revenue_0 = base revenue
//	margin_t         = start + (terminal - start) x t / 5   (start defaults to terminal)
//	cash_t           = revenue_t x margin_t x k x cash_conversion, k = 1 - tax_rate
//	                   for ebit_ev and 1 for net_pe (net income is already after tax)
//	terminal value   = revenue_5 x terminal margin x exit multiple
//	value            = terminal value / (1+r)^5 + sum_t cash_t / (1+r)^t
//
// Every initiative i adds, weighted by its success probability w for this
// scenario: revenue_i,t (the linear ramp of Initiative.RevenueInYear) to the
// interim cash with its own constant margin, and revenue_i,5 x margin_i x exit
// multiple to the terminal value, discounted like the core. ebit_ev subtracts
// net debt from the core; net_pe does not. Value per share is that over shares.
// With cash_conversion 0, no start margin and no initiatives it is the original
// terminal-value-only formula.
func ScenarioDCF(in DCFInput) (DCFResult, error) {
	if err := in.Validate(); err != nil {
		return DCFResult{}, err
	}
	res := DCFResult{Scenarios: make([]ScenarioValue, 0, len(in.Scenarios))}
	weighted, weightedCore, weightedInit := decimal.Zero, decimal.Zero, decimal.Zero
	for _, s := range in.Scenarios {
		c := calcScenario(in.BaseRevenue, in.NetDebt, in.Shares, s, in.Initiatives)
		weighted = weighted.Add(c.vps.Mul(s.Probability))
		weightedCore = weightedCore.Add(c.coreVPS.Mul(s.Probability))
		weightedInit = weightedInit.Add(c.initVPS.Mul(s.Probability))
		res.Scenarios = append(res.Scenarios, ScenarioValue{
			Name: s.Name, Probability: s.Probability, Revenue5: c.rev5.Round(DCFValuePrecision),
			TerminalValue: c.terminalValue.Round(DCFValuePrecision), PresentValue: c.presentValue.Round(DCFValuePrecision),
			ValuePerShare: c.vps.Round(DCFValuePrecision), Basis: s.EffectiveBasis(),
			InterimPresentValue: c.interimPV.Round(DCFValuePrecision), InitiativesRevenue5: c.initRev5.Round(DCFValuePrecision),
			CoreValuePerShare: c.coreVPS.Round(DCFValuePrecision), InitiativesValuePerShare: c.initVPS.Round(DCFValuePrecision),
		})
	}
	res.ValuePerShare = weighted.Round(DCFValuePrecision)
	res.CoreValuePerShare = weightedCore.Round(DCFValuePrecision)
	res.InitiativesValuePerShare = weightedInit.Round(DCFValuePrecision)
	return res, nil
}

// scenarioCalc is the unrounded result of one scenario.
type scenarioCalc struct {
	rev5          decimal.Decimal // core year-5 revenue
	initRev5      decimal.Decimal // probability-weighted initiatives year-5 revenue
	terminalValue decimal.Decimal // undiscounted, core + initiatives
	interimPV     decimal.Decimal // discounted interim cash, core + initiatives
	presentValue  decimal.Decimal // discounted terminal + interim, before net debt
	vps           decimal.Decimal
	coreVPS       decimal.Decimal
	initVPS       decimal.Decimal
}

// calcScenario is the single-scenario formula shared by ScenarioDCF and
// ReverseDCF (see ScenarioDCF for the formulas). The scenario name selects the
// initiatives' success probability.
func calcScenario(baseRevenue, netDebt, shares decimal.Decimal, s Scenario, initiatives []Initiative) scenarioCalc {
	one := decimal.NewFromInt(1)
	years := decimal.NewFromInt(DCFYears)
	basis := s.EffectiveBasis()
	// cashFactor is the share of a year's earnings that becomes interim cash.
	cashFactor := s.CashConversion
	if basis == BasisEBITEV {
		cashFactor = cashFactor.Mul(one.Sub(s.TaxRate))
	}
	start := s.TerminalMargin
	if s.StartMargin != nil {
		start = *s.StartMargin
	}
	rate := one.Add(s.DiscountRate)

	var c scenarioCalc
	coreInterim, initInterim := decimal.Zero, decimal.Zero
	rev := baseRevenue
	for t := 1; t <= DCFYears; t++ {
		rev = rev.Mul(one.Add(s.RevenueGrowth[t-1]))
		if cashFactor.IsZero() {
			continue
		}
		df := rate.Pow(decimal.NewFromInt(int64(t)))
		margin := start.Add(s.TerminalMargin.Sub(start).Mul(decimal.NewFromInt(int64(t))).Div(years))
		coreInterim = coreInterim.Add(rev.Mul(margin).Mul(cashFactor).Div(df))
		for _, i := range initiatives {
			w := i.SuccessProbability.For(s.Name)
			initInterim = initInterim.Add(i.RevenueInYear(t).Mul(w).Mul(i.Margin).Mul(cashFactor).Div(df))
		}
	}
	c.rev5 = rev
	initEarnings5 := decimal.Zero // probability-weighted year-5 initiative earnings
	for _, i := range initiatives {
		w := i.SuccessProbability.For(s.Name)
		c.initRev5 = c.initRev5.Add(i.RevenueYear5.Mul(w))
		initEarnings5 = initEarnings5.Add(i.RevenueYear5.Mul(w).Mul(i.Margin))
	}
	coreTV := rev.Mul(s.TerminalMargin).Mul(s.ExitMultiple)
	initTV := initEarnings5.Mul(s.ExitMultiple)
	df5 := rate.Pow(decimal.NewFromInt(DCFYears))
	corePV := coreTV.Div(df5).Add(coreInterim)
	initPV := initTV.Div(df5).Add(initInterim)

	c.terminalValue = coreTV.Add(initTV)
	c.interimPV = coreInterim.Add(initInterim)
	c.presentValue = corePV.Add(initPV)
	coreEquity := corePV
	if basis == BasisEBITEV {
		coreEquity = corePV.Sub(netDebt)
	}
	c.coreVPS = coreEquity.Div(shares)
	c.initVPS = initPV.Div(shares)
	c.vps = coreEquity.Add(initPV).Div(shares)
	return c
}
