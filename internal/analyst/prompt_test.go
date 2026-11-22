package analyst

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/capm"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

func md(s string) money.Decimal { return money.New(decimal.RequireFromString(s)) }

func TestRulesAnchorDiscountRateOnCAPM(t *testing.T) {
	for _, m := range []Mode{ModeAPI, ModeMCP} {
		r := Rules(m)
		if strings.Contains(r, "0.12") {
			t.Errorf("mode %d: prompt still anchors discount_rate on 0.12", m)
		}
		if !strings.Contains(r, "CAPM cost of equity") || !strings.Contains(r, "explain any deviation") {
			t.Errorf("mode %d: discount_rate rule missing the CAPM guidance", m)
		}
	}
}

func TestUserPromptCostOfEquity(t *testing.T) {
	in := Inputs{Ticker: "AMD", Market: "US", Currency: "USD"}
	if p := userPrompt(in); !strings.Contains(p, "## Cost of equity (CAPM)\nNot available") {
		t.Errorf("missing-CAPM section:\n%s", p)
	}
	in.CAPM = &capm.CostOfEquity{
		Rate: md("0.1307"), RiskFree: md("0.0412"), RiskFreeDate: "2026-11-17", RawBeta: md("2.18"), AdjBeta: md("1.7906"),
		BetaDate: "2026-11-18", BetaIndex: "SP500", ERP: md("0.05"), MinPremium: md("0.03"),
	}
	p := userPrompt(in)
	for _, want := range []string{"Cost of equity 0.1307 = risk-free 0.0412 (as of 2026-11-17) + adjusted beta 1.7906 x equity risk premium 0.05", "raw beta 2.18"} {
		if !strings.Contains(p, want) {
			t.Errorf("%q missing from:\n%s", want, p)
		}
	}
	in.CAPM.Floored = true
	if p := userPrompt(in); !strings.Contains(p, "below the floor") {
		t.Errorf("floored note missing:\n%s", p)
	}
}
