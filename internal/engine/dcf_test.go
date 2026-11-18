package engine

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func scen(name, growth, margin, multiple, rate, prob string) Scenario {
	g := d(growth)
	return Scenario{Name: name, RevenueGrowth: [DCFYears]decimal.Decimal{g, g, g, g, g}, TerminalMargin: d(margin),
		ExitMultiple: d(multiple), DiscountRate: d(rate), Probability: d(prob)}
}

func dcfInput() DCFInput {
	return DCFInput{BaseRevenue: d("100"), Shares: d("10"), Scenarios: []Scenario{
		scen(ScenarioBear, "0", "0.1", "10", "0.1", "0.25"),
		scen(ScenarioBase, "0.1", "0.1", "10", "0.1", "0.5"),
		scen(ScenarioBull, "0.2", "0.2", "15", "0.1", "0.25"),
	}}
}

func TestScenarioDCFHandComputed(t *testing.T) {
	// bear: 100*0.1*10 / 1.1^5 / 10 = 6.2092
	// base: 100*1.1^5*0.1*10 / 1.1^5 / 10 = 10
	// bull: 100*1.2^5*0.2*15 / 1.1^5 / 10 = 46.3515
	// weighted: 0.25*6.2092.. + 0.5*10 + 0.25*46.3515.. = 18.1402
	res, err := ScenarioDCF(dcfInput())
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"6.2092", "10", "46.3515"} {
		eqD(t, res.Scenarios[i].Name, res.Scenarios[i].ValuePerShare, d(want))
	}
	eqD(t, "weighted", res.ValuePerShare, d("18.1402"))

	in := dcfInput()
	in.NetDebt = d("50")
	res, err = ScenarioDCF(in)
	if err != nil {
		t.Fatal(err)
	}
	eqD(t, "bear with net debt", res.Scenarios[0].ValuePerShare, d("1.2092"))
}

func TestScenarioDCFValidation(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*DCFInput)
	}{
		{"probabilities must sum to 1", func(in *DCFInput) { in.Scenarios[0].Probability = d("0.3") }},
		{"missing scenario", func(in *DCFInput) { in.Scenarios = in.Scenarios[:2] }},
		{"duplicate scenario", func(in *DCFInput) { in.Scenarios[1].Name = ScenarioBear }},
		{"unknown scenario", func(in *DCFInput) { in.Scenarios[1].Name = "moon" }},
		{"zero shares", func(in *DCFInput) { in.Shares = d("0") }},
		{"zero revenue", func(in *DCFInput) { in.BaseRevenue = d("0") }},
		{"growth at -100%", func(in *DCFInput) { in.Scenarios[0].RevenueGrowth[2] = d("-1") }},
		{"margin above 1", func(in *DCFInput) { in.Scenarios[0].TerminalMargin = d("1.1") }},
		{"zero multiple", func(in *DCFInput) { in.Scenarios[0].ExitMultiple = d("0") }},
		{"zero discount rate", func(in *DCFInput) { in.Scenarios[0].DiscountRate = d("0") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := dcfInput()
			tc.mod(&in)
			if _, err := ScenarioDCF(in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}
