package analyst

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
)

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func arr(items any, desc string) map[string]any {
	return map[string]any{"type": "array", "items": items, "description": desc}
}

func scenarioSchema() map[string]any {
	return obj(map[string]any{
		"revenue_growth":  arr(map[string]any{"type": "number"}, "Exactly 5 yearly revenue growth rates as fractions (0.08 = 8%), year 1 first"),
		"basis":           map[string]any{"type": "string", "enum": []string{engine.BasisEBITEV, engine.BasisNetPE}, "description": "ebit_ev: terminal_margin and start_margin are EBIT margins, exit_multiple is EV/EBIT and net debt is subtracted. net_pe: they are net margins, exit_multiple is P/E and net debt is NOT subtracted. Pick one and keep margin and multiple consistent with it"},
		"start_margin":    num("Margin today (year 0) on the same basis; the margin moves linearly to terminal_margin over the 5 years. Use the latest reported margin from the fundamentals; equal to terminal_margin if flat"),
		"terminal_margin": num("Margin on year-5 revenue on the chosen basis (EBIT margin for ebit_ev, net margin for net_pe), fraction between 0 and 1"),
		"exit_multiple":   num("Multiple applied to year-5 earnings on the chosen basis (EV/EBIT or P/E), positive"),
		"cash_conversion": num("Fraction between 0 and 1 of each year's earnings returned or accumulated for shareholders (dividends, buybacks, FCF); 0 values only the exit"),
		"tax_rate":        num("Tax rate as a fraction in [0, 1), used to turn EBIT into after-tax cash for ebit_ev (about 0.34 for Brazil, 0.21 for the US); ignored for net_pe"),
		"discount_rate":   num("Annual discount rate as a fraction, positive"),
		"probability":     num("Scenario weight as a fraction; the three probabilities must sum to 1"),
		"rationale":       str("Why these assumptions, grounded in the provided documents"),
	}, "basis", "revenue_growth", "start_margin", "terminal_margin", "exit_multiple", "cash_conversion", "tax_rate", "discount_rate", "probability", "rationale")
}

func initiativeSchema() map[string]any {
	prob := obj(map[string]any{
		"bear": num("Probability the initiative succeeds in the bear scenario, fraction in [0, 1]"),
		"base": num("Probability in the base scenario"),
		"bull": num("Probability in the bull scenario"),
	}, "bear", "base", "bull")
	return obj(map[string]any{
		"name":                str("Short name of the new business"),
		"rationale":           str("Why it is credible and why it is not in past revenue"),
		"sources":             arr(map[string]any{"type": "string"}, "URLs of the provided documents that support it"),
		"start_year":          map[string]any{"type": "integer", "description": "First forecast year (1 to 5) in which it earns revenue"},
		"revenue_year5":       num("Revenue in year 5 if it succeeds, in the asset's currency, positive; ramps linearly from start_year"),
		"margin":              num("Earnings margin on the same basis as the scenarios, fraction in [0, 1], constant over its life"),
		"success_probability": prob,
	}, "name", "rationale", "sources", "start_year", "revenue_year5", "margin", "success_probability")
}

// OutputSchema is the JSON schema of an analysis. The in-app analyst forces the
// model's answer into it and the MCP submit tools take the same shape.
func OutputSchema() map[string]any { return outputSchema() }

// outputSchema is the JSON schema the model's answer must follow.
func outputSchema() map[string]any {
	return obj(map[string]any{
		"catalysts": arr(obj(map[string]any{
			"title":         str("Short name of the catalyst"),
			"detail":        str("What could happen and why it matters for the price"),
			"expected_date": str("YYYY-MM-DD when a date is known, otherwise an empty string"),
			"source_urls":   arr(map[string]any{"type": "string"}, "URLs of the provided documents that support it"),
		}, "title", "detail", "expected_date", "source_urls"), "Upcoming catalysts, at most 12"),
		"market_size_notes": str("What the documents say about market size and share; say so if they say little"),
		"scenarios": obj(map[string]any{
			"bear": scenarioSchema(), "base": scenarioSchema(), "bull": scenarioSchema(),
		}, "bear", "base", "bull"),
		"initiatives": arr(initiativeSchema(), "New businesses (new products, segments, R&D, capex programs, market entries) not reflected in historical revenue, found in the documents; empty when there is none. At most 10"),
		"fair_value_range": obj(map[string]any{
			"low":      num("Low fair value per share, positive"),
			"high":     num("High fair value per share, not below low"),
			"currency": map[string]any{"type": "string", "enum": []string{"BRL", "USD"}},
		}, "low", "high", "currency"),
		"thesis_check": arr(obj(map[string]any{
			"condition": str("The break condition, copied from the input"),
			"status":    map[string]any{"type": "string", "enum": []string{CheckHolds, CheckAtRisk, CheckBroken}},
			"evidence":  str("Evidence from the provided documents, or why there is none"),
		}, "condition", "status", "evidence"), "One entry per break condition of the thesis"),
		"sources": arr(obj(map[string]any{
			"title": str("Document title"),
			"url":   str("URL exactly as provided"),
		}, "title", "url"), "Provided documents the answer relied on"),
	}, "catalysts", "market_size_notes", "scenarios", "initiatives", "fair_value_range", "thesis_check", "sources")
}

// ErrInvalidOutput marks model output that failed server-side validation.
var errInvalidOutput = fmt.Errorf("invalid model output")

func invalidf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errInvalidOutput, fmt.Sprintf(format, a...))
}

// parseOutput decodes and validates the model's JSON. Sources and catalyst
// URLs that are not among the provided documents are removed (the model may
// only cite what it was given); the count removed is returned.
func parseOutput(text string, allowed map[string]bool) (Output, int, error) {
	var out Output
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(text)))
	if err := dec.Decode(&out); err != nil {
		return Output{}, 0, invalidf("not valid JSON: %v", err)
	}
	if err := out.validate(); err != nil {
		return Output{}, 0, err
	}
	dropped := out.restrictSources(allowed)
	return out, dropped, nil
}

// validate checks a full answer: scenarios, range and thesis check are required.
func (o *Output) validate() error { return o.check(true) }

// Validate checks an answer submitted over MCP. With partial, scenarios and the
// fair value range may be left out (a candidate may carry only a note or only
// a fair value); whatever is given is validated like a full answer.
func (o *Output) Validate(partial bool) error { return o.check(!partial) }

func (o *Output) hasScenarios() bool {
	return len(o.Scenarios.Bear.RevenueGrowth)+len(o.Scenarios.Base.RevenueGrowth)+len(o.Scenarios.Bull.RevenueGrowth) > 0
}

func (o *Output) hasRange() bool {
	r := o.FairValueRange
	return !r.Low.IsZero() || !r.High.IsZero() || r.Currency != ""
}

func (o *Output) check(full bool) error {
	if len(o.Catalysts) > maxCatalysts {
		o.Catalysts = o.Catalysts[:maxCatalysts]
	}
	for i, c := range o.Catalysts {
		if strings.TrimSpace(c.Title) == "" {
			return invalidf("catalyst %d has no title", i+1)
		}
		if c.ExpectedDate != "" {
			if _, err := time.Parse("2006-01-02", c.ExpectedDate); err != nil {
				o.Catalysts[i].ExpectedDate = ""
			}
		}
	}
	if full || o.hasScenarios() {
		dcf := engine.DCFInput{BaseRevenue: decimal.NewFromInt(1), Shares: decimal.NewFromInt(1)}
		for _, p := range []struct {
			name string
			s    ScenarioDraft
		}{{engine.ScenarioBear, o.Scenarios.Bear}, {engine.ScenarioBase, o.Scenarios.Base}, {engine.ScenarioBull, o.Scenarios.Bull}} {
			if len(p.s.RevenueGrowth) != engine.DCFYears {
				return invalidf("%s needs %d yearly revenue growth rates, got %d", p.name, engine.DCFYears, len(p.s.RevenueGrowth))
			}
			dcf.Scenarios = append(dcf.Scenarios, p.s.toEngine(p.name))
		}
		dcf.Initiatives = o.Initiatives
		if err := dcf.Validate(); err != nil {
			return invalidf("%v", err)
		}
		for i, sc := range dcf.Scenarios {
			if sc.Basis == "" {
				return invalidf("%s must state its basis (ebit_ev or net_pe)", dcf.Scenarios[i].Name)
			}
		}
	} else if len(o.Initiatives) > 0 {
		return invalidf("initiatives need scenarios (they are valued with them)")
	}
	if full || o.hasRange() {
		r := o.FairValueRange
		if !r.Low.IsPositive() || r.High.LessThan(r.Low) {
			return invalidf("fair value range must have 0 < low <= high (got %s to %s)", r.Low, r.High)
		}
		if r.Currency != "BRL" && r.Currency != "USD" {
			return invalidf("fair value range currency must be BRL or USD")
		}
	}
	for i, t := range o.ThesisCheck {
		switch t.Status {
		case CheckHolds, CheckAtRisk, CheckBroken:
		default:
			return invalidf("thesis check %d has status %q", i+1, t.Status)
		}
		if strings.TrimSpace(t.Condition) == "" {
			return invalidf("thesis check %d has no condition", i+1)
		}
	}
	if o.Catalysts == nil {
		o.Catalysts = []Catalyst{}
	}
	if o.ThesisCheck == nil {
		o.ThesisCheck = []ThesisCheck{}
	}
	if o.Sources == nil {
		o.Sources = []Source{}
	}
	return nil
}

func (s ScenarioDraft) toEngine(name string) engine.Scenario {
	sc := engine.Scenario{
		Name: name, TerminalMargin: s.TerminalMargin, ExitMultiple: s.ExitMultiple,
		DiscountRate: s.DiscountRate, Probability: s.Probability,
	}
	sc.Basis, sc.StartMargin, sc.CashConversion, sc.TaxRate = s.Basis, s.StartMargin, s.CashConversion, s.TaxRate
	copy(sc.RevenueGrowth[:], s.RevenueGrowth)
	return sc
}

// DCF builds the scenario DCF input for the given base figures.
func (o Output) DCF(b DCFBase) engine.DCFInput {
	return engine.DCFInput{
		BaseRevenue: b.BaseRevenue, NetDebt: b.NetDebt, Shares: b.Shares,
		Scenarios: []engine.Scenario{
			o.Scenarios.Bear.toEngine(engine.ScenarioBear),
			o.Scenarios.Base.toEngine(engine.ScenarioBase),
			o.Scenarios.Bull.toEngine(engine.ScenarioBull),
		},
		Initiatives: o.Initiatives,
	}
}

// restrictSources keeps only cited URLs that are provided documents and
// returns how many entries were removed.
func (o *Output) restrictSources(allowed map[string]bool) int {
	dropped := 0
	srcs := make([]Source, 0, len(o.Sources))
	for _, s := range o.Sources {
		if allowed[s.URL] {
			srcs = append(srcs, s)
		} else {
			dropped++
		}
	}
	o.Sources = srcs
	for i := range o.Catalysts {
		keep := make([]string, 0, len(o.Catalysts[i].SourceURLs))
		for _, u := range o.Catalysts[i].SourceURLs {
			if allowed[u] {
				keep = append(keep, u)
			} else {
				dropped++
			}
		}
		o.Catalysts[i].SourceURLs = keep
	}
	for i := range o.Initiatives {
		keep := make([]string, 0, len(o.Initiatives[i].Sources))
		for _, u := range o.Initiatives[i].Sources {
			if allowed[u] {
				keep = append(keep, u)
			} else {
				dropped++
			}
		}
		o.Initiatives[i].Sources = keep
	}
	return dropped
}
