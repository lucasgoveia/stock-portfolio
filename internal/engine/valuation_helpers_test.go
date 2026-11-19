package engine

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestBazin(t *testing.T) {
	tests := []struct {
		name   string
		dps, y string
		want   string
		wantOK bool
	}{
		{"default yield", "1.20", "0.06", "20", true},
		{"custom yield", "1.20", "0.08", "15", true},
		{"no dividends", "0", "0.06", "0", false},
		{"negative", "-1", "0.06", "0", false},
		{"zero yield", "1", "0", "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Bazin(d(tc.dps), d(tc.y))
			if ok != tc.wantOK || !got.Equal(d(tc.want)) {
				t.Errorf("Bazin = %s, %v; want %s, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestGraham(t *testing.T) {
	tests := []struct {
		name      string
		eps, bvps string
		want      string
		wantOK    bool
	}{
		{"classic", "2", "11.25", "22.5", true}, // sqrt(22.5*2*11.25)=sqrt(506.25)
		{"irrational", "1", "1", "4.7434", true},
		{"negative eps", "-1", "10", "0", false},
		{"zero bvps", "1", "0", "0", false},
		{"negative bvps", "1", "-5", "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Graham(d(tc.eps), d(tc.bvps))
			if ok != tc.wantOK || !got.Equal(d(tc.want)) {
				t.Errorf("Graham = %s, %v; want %s, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestReverseDCFRoundTrip(t *testing.T) {
	base := ReverseDCFInput{
		BaseRevenue: d("1000"), NetDebt: d("200"), Shares: d("100"),
		TerminalMargin: d("0.2"), ExitMultiple: d("10"), DiscountRate: d("0.1"),
	}
	// Price produced by the scenario DCF at 8% growth must be solved back to 8%.
	growth := d("0.08")
	sc := Scenario{TerminalMargin: base.TerminalMargin, ExitMultiple: base.ExitMultiple, DiscountRate: base.DiscountRate}
	for i := range sc.RevenueGrowth {
		sc.RevenueGrowth[i] = growth
	}
	vps := calcScenario(base.BaseRevenue, base.NetDebt, base.Shares, sc, nil).vps
	base.Price = vps
	res, err := ReverseDCF(base)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ReverseOK || res.ImpliedGrowth.Sub(growth).Abs().GreaterThan(d("0.00001")) {
		t.Fatalf("got %+v, want growth %s", res, growth)
	}

	// Consistency with ScenarioDCF: three identical scenarios give the same value.
	full, err := ScenarioDCF(DCFInput{BaseRevenue: base.BaseRevenue, NetDebt: base.NetDebt, Shares: base.Shares, Scenarios: []Scenario{
		withName(sc, ScenarioBear, "0.3"), withName(sc, ScenarioBase, "0.4"), withName(sc, ScenarioBull, "0.3"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if full.ValuePerShare.Sub(vps).Abs().GreaterThan(d("0.0001")) {
		t.Errorf("ScenarioDCF %s vs helper %s", full.ValuePerShare, vps)
	}
}

func withName(s Scenario, name, p string) Scenario {
	s.Name, s.Probability = name, d(p)
	return s
}

func TestReverseDCFRangesAndErrors(t *testing.T) {
	in := ReverseDCFInput{
		Price: d("0.0001"), BaseRevenue: d("1000"), NetDebt: d("0"), Shares: d("100"),
		TerminalMargin: d("0.2"), ExitMultiple: d("10"), DiscountRate: d("0.1"),
	}
	res, err := ReverseDCF(in)
	if err != nil || res.Status != ReverseBelowRange {
		t.Errorf("low price: %+v %v", res, err)
	}
	in.Price = d("1000000")
	res, err = ReverseDCF(in)
	if err != nil || res.Status != ReverseAboveRange {
		t.Errorf("high price: %+v %v", res, err)
	}
	// Negative implied growth is representable.
	in.Price = d("5")
	res, err = ReverseDCF(in)
	if err != nil || res.Status != ReverseOK || !res.ImpliedGrowth.IsNegative() {
		t.Errorf("shrinking: %+v %v", res, err)
	}
	for _, bad := range []func(*ReverseDCFInput){
		func(i *ReverseDCFInput) { i.Price = decimal.Zero },
		func(i *ReverseDCFInput) { i.BaseRevenue = decimal.Zero },
		func(i *ReverseDCFInput) { i.Shares = decimal.Zero },
		func(i *ReverseDCFInput) { i.TerminalMargin = decimal.Zero },
		func(i *ReverseDCFInput) { i.ExitMultiple = decimal.Zero },
		func(i *ReverseDCFInput) { i.DiscountRate = decimal.Zero },
	} {
		x := in
		bad(&x)
		if _, err := ReverseDCF(x); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("want ErrInvalidInput, got %v", err)
		}
	}
}
