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
		"terminal_margin": num("Earnings (or FCF) margin on year-5 revenue, fraction between 0 and 1"),
		"exit_multiple":   num("Multiple applied to year-5 earnings, positive"),
		"discount_rate":   num("Annual discount rate as a fraction, positive"),
		"probability":     num("Scenario weight as a fraction; the three probabilities must sum to 1"),
		"rationale":       str("Why these assumptions, grounded in the provided documents"),
	}, "revenue_growth", "terminal_margin", "exit_multiple", "discount_rate", "probability", "rationale")
}

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
	}, "catalysts", "market_size_notes", "scenarios", "fair_value_range", "thesis_check", "sources")
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

func (o *Output) validate() error {
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
	if err := dcf.Validate(); err != nil {
		return invalidf("%v", err)
	}
	r := o.FairValueRange
	if !r.Low.IsPositive() || r.High.LessThan(r.Low) {
		return invalidf("fair value range must have 0 < low <= high (got %s to %s)", r.Low, r.High)
	}
	if r.Currency != "BRL" && r.Currency != "USD" {
		return invalidf("fair value range currency must be BRL or USD")
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
	return nil
}

func (s ScenarioDraft) toEngine(name string) engine.Scenario {
	sc := engine.Scenario{
		Name: name, TerminalMargin: s.TerminalMargin, ExitMultiple: s.ExitMultiple,
		DiscountRate: s.DiscountRate, Probability: s.Probability,
	}
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
	return dropped
}
