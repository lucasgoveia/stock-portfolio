package engine

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

// flatInput is dcfInput with every scenario flat (no growth, 10% margin, 10x,
// 10%), so interim cash is easy to compute by hand: revenue 100, earnings 10.
func flatInput(mod func(i int, s *Scenario)) DCFInput {
	in := DCFInput{BaseRevenue: d("100"), Shares: d("10"), NetDebt: d("50"), Scenarios: []Scenario{
		scen(ScenarioBear, "0", "0.1", "10", "0.1", "0.25"),
		scen(ScenarioBase, "0", "0.1", "10", "0.1", "0.5"),
		scen(ScenarioBull, "0", "0.1", "10", "0.1", "0.25"),
	}}
	for i := range in.Scenarios {
		mod(i, &in.Scenarios[i])
	}
	return in
}

func TestScenarioDCFBasisAndInterimCash(t *testing.T) {
	tests := []struct {
		name    string
		mod     func(s *Scenario)
		wantVPS string // base scenario value per share
		wantPV  string // base scenario interim present value
	}{
		// 100 x 0.1 x 10 / 1.1^5 = 62.0921; net debt 50 is subtracted: (62.0921-50)/10.
		{"ebit_ev default subtracts net debt", func(s *Scenario) {}, "1.2092", "0"},
		{"ebit_ev explicit", func(s *Scenario) { s.Basis = BasisEBITEV }, "1.2092", "0"},
		// net_pe: the same terminal value is equity already, 62.0921 / 10.
		{"net_pe does not subtract net debt", func(s *Scenario) { s.Basis = BasisNetPE }, "6.2092", "0"},
		// cash = 100 x 0.1 x 0.66 x 0.5 = 3.3 a year, discounted 1.1^t (annuity 3.790787) = 12.5096.
		{"ebit_ev cash conversion after tax", func(s *Scenario) {
			s.CashConversion, s.TaxRate = d("0.5"), d("0.34")
		}, "2.4602", "12.5096"},
		// net_pe: cash = 100 x 0.1 x 0.5 = 5 a year = 18.9539; (62.0921 + 18.9539) / 10.
		{"net_pe cash conversion ignores tax", func(s *Scenario) {
			s.Basis, s.CashConversion, s.TaxRate = BasisNetPE, d("0.5"), d("0.34")
		}, "8.1046", "18.9539"},
		// margin 6%..10% over the years, all earnings returned: 29.6065 + 62.0921, no net debt.
		{"net_pe margin path from start margin", func(s *Scenario) {
			s.Basis, s.CashConversion, s.StartMargin = BasisNetPE, d("1"), dp("0.05")
		}, "9.1699", "29.6065"},
		// start margin equal to terminal is the same as none.
		{"start margin equal to terminal", func(s *Scenario) { s.StartMargin = dp("0.1") }, "1.2092", "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ScenarioDCF(flatInput(func(_ int, s *Scenario) { tc.mod(s) }))
			if err != nil {
				t.Fatal(err)
			}
			base := res.Scenarios[1]
			eqD(t, "value per share", base.ValuePerShare, d(tc.wantVPS))
			eqD(t, "interim present value", base.InterimPresentValue, d(tc.wantPV))
			eqD(t, "initiatives value", base.InitiativesValuePerShare, d("0"))
			eqD(t, "core value", base.CoreValuePerShare, d(tc.wantVPS))
		})
	}
}

func TestScenarioDCFInitiatives(t *testing.T) {
	// One initiative from year 3 to 50 of revenue in year 5 at a 20% margin,
	// succeeding with 20% / 50% / 100% in bear / base / bull.
	ini := Initiative{
		Name: "Fintech", StartYear: 3, RevenueYear5: d("50"), Margin: d("0.2"),
		SuccessProbability: InitiativeProbabilities{Bear: d("0.2"), Base: d("0.5"), Bull: d("1")},
	}
	tests := []struct {
		name                  string
		cashConversion, tax   string
		wantVPS, wantCore     [3]string // bear, base, bull
		wantInit, wantBaseRev string
	}{
		{
			// Terminal only: 50 x w x 0.2 x 10 / 1.1^5 / 10; core stays 1.2092.
			name: "terminal value only", cashConversion: "0", tax: "0",
			wantVPS:  [3]string{"2.4511", "4.3138", "7.4184"},
			wantCore: [3]string{"1.2092", "1.2092", "1.2092"}, wantInit: "3.1046", wantBaseRev: "25",
		},
		{
			// Interim cash too: revenue ramps 50/3, 100/3, 50 in years 3 to 5.
			name: "with interim cash", cashConversion: "0.5", tax: "0.2",
			wantVPS:  [3]string{"4.0735", "6.0955", "9.4654"},
			wantCore: [3]string{"2.7255", "2.7255", "2.7255"}, wantInit: "3.3699", wantBaseRev: "25",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := flatInput(func(_ int, s *Scenario) { s.CashConversion, s.TaxRate = d(tc.cashConversion), d(tc.tax) })
			in.Initiatives = []Initiative{ini}
			res, err := ScenarioDCF(in)
			if err != nil {
				t.Fatal(err)
			}
			for i, sv := range res.Scenarios {
				eqD(t, sv.Name+" value", sv.ValuePerShare, d(tc.wantVPS[i]))
				eqD(t, sv.Name+" core", sv.CoreValuePerShare, d(tc.wantCore[i]))
				if sum := sv.CoreValuePerShare.Add(sv.InitiativesValuePerShare); sum.Sub(sv.ValuePerShare).Abs().GreaterThan(d("0.0001")) {
					t.Errorf("%s: core + initiatives = %s, value %s", sv.Name, sum, sv.ValuePerShare)
				}
			}
			eqD(t, "base initiatives", res.Scenarios[1].InitiativesValuePerShare, d(tc.wantInit))
			eqD(t, "base weighted initiatives revenue", res.Scenarios[1].InitiativesRevenue5, d(tc.wantBaseRev))
			eqD(t, "core revenue unchanged", res.Scenarios[1].Revenue5, d("100"))
			// The weighted totals are the probability-weighted per-scenario values.
			want := res.Scenarios[0].ValuePerShare.Mul(d("0.25")).Add(res.Scenarios[1].ValuePerShare.Mul(d("0.5"))).Add(res.Scenarios[2].ValuePerShare.Mul(d("0.25")))
			if res.ValuePerShare.Sub(want).Abs().GreaterThan(d("0.0001")) {
				t.Errorf("weighted value %s, want %s", res.ValuePerShare, want)
			}
			if sum := res.CoreValuePerShare.Add(res.InitiativesValuePerShare); sum.Sub(res.ValuePerShare).Abs().GreaterThan(d("0.0001")) {
				t.Errorf("weighted core + initiatives = %s, value %s", sum, res.ValuePerShare)
			}
		})
	}
}

func TestInitiativeRamp(t *testing.T) {
	tests := []struct {
		start int
		want  [5]string
	}{
		{1, [5]string{"10", "20", "30", "40", "50"}},
		{3, [5]string{"0", "0", "50/3", "100/3", "50"}},
		{5, [5]string{"0", "0", "0", "0", "50"}},
	}
	for _, tc := range tests {
		i := Initiative{StartYear: tc.start, RevenueYear5: d("50")}
		for y := 1; y <= 5; y++ {
			var want decimal.Decimal
			switch tc.want[y-1] {
			case "50/3":
				want = d("50").Div(d("3"))
			case "100/3":
				want = d("100").Div(d("3"))
			default:
				want = d(tc.want[y-1])
			}
			nearD(t, "ramp", i.RevenueInYear(y), want)
		}
	}
}

// A version stored before basis, start margin, cash conversion, tax rate and
// initiatives existed must keep its value: read as ebit_ev, no interim cash,
// no initiatives.
func TestScenarioDCFStoredVersionWithoutNewFields(t *testing.T) {
	const stored = `{"base_revenue":"100","net_debt":"50","shares":"10","scenarios":[
	 {"name":"bear","revenue_growth":["0","0","0","0","0"],"terminal_margin":"0.1","exit_multiple":"10","discount_rate":"0.1","probability":"0.25"},
	 {"name":"base","revenue_growth":["0.1","0.1","0.1","0.1","0.1"],"terminal_margin":"0.1","exit_multiple":"10","discount_rate":"0.1","probability":"0.5"},
	 {"name":"bull","revenue_growth":["0.2","0.2","0.2","0.2","0.2"],"terminal_margin":"0.2","exit_multiple":"15","discount_rate":"0.1","probability":"0.25"}]}`
	var in DCFInput
	if err := json.Unmarshal([]byte(stored), &in); err != nil {
		t.Fatal(err)
	}
	res, err := ScenarioDCF(in)
	if err != nil {
		t.Fatal(err)
	}
	// Values of the original formula, (terminal value / 1.1^5 - net debt) / shares.
	for i, want := range []string{"1.2092", "5", "41.3515"} {
		eqD(t, res.Scenarios[i].Name, res.Scenarios[i].ValuePerShare, d(want))
		if res.Scenarios[i].Basis != BasisEBITEV {
			t.Errorf("%s basis %q, want ebit_ev", res.Scenarios[i].Name, res.Scenarios[i].Basis)
		}
		eqD(t, res.Scenarios[i].Name+" interim", res.Scenarios[i].InterimPresentValue, d("0"))
		eqD(t, res.Scenarios[i].Name+" initiatives", res.Scenarios[i].InitiativesValuePerShare, d("0"))
	}
	eqD(t, "weighted", res.ValuePerShare, d("13.1402"))

	// Explicit ebit_ev and zero new fields give the same result.
	for i := range in.Scenarios {
		in.Scenarios[i].Basis = BasisEBITEV
	}
	again, err := ScenarioDCF(in)
	if err != nil {
		t.Fatal(err)
	}
	eqD(t, "explicit ebit_ev", again.ValuePerShare, res.ValuePerShare)
}

func TestScenarioDCFNewFieldValidation(t *testing.T) {
	good := Initiative{Name: "X", StartYear: 1, RevenueYear5: d("10"), Margin: d("0.1"), SuccessProbability: InitiativeProbabilities{Base: d("0.5")}}
	tests := []struct {
		name string
		mod  func(*DCFInput)
	}{
		{"unknown basis", func(in *DCFInput) { in.Scenarios[0].Basis = "fcf" }},
		{"start margin above 1", func(in *DCFInput) { in.Scenarios[0].StartMargin = dp("1.2") }},
		{"negative start margin", func(in *DCFInput) { in.Scenarios[0].StartMargin = dp("-0.1") }},
		{"tax rate 1", func(in *DCFInput) { in.Scenarios[0].TaxRate = d("1") }},
		{"negative tax rate", func(in *DCFInput) { in.Scenarios[0].TaxRate = d("-0.1") }},
		{"cash conversion above 1", func(in *DCFInput) { in.Scenarios[0].CashConversion = d("1.1") }},
		{"negative cash conversion", func(in *DCFInput) { in.Scenarios[0].CashConversion = d("-0.1") }},
		{"initiative without name", func(in *DCFInput) { i := good; i.Name = " "; in.Initiatives = []Initiative{i} }},
		{"initiative start year 0", func(in *DCFInput) { i := good; i.StartYear = 0; in.Initiatives = []Initiative{i} }},
		{"initiative start year 6", func(in *DCFInput) { i := good; i.StartYear = 6; in.Initiatives = []Initiative{i} }},
		{"initiative zero revenue", func(in *DCFInput) { i := good; i.RevenueYear5 = d("0"); in.Initiatives = []Initiative{i} }},
		{"initiative negative margin", func(in *DCFInput) { i := good; i.Margin = d("-0.1"); in.Initiatives = []Initiative{i} }},
		{"initiative margin above 1", func(in *DCFInput) { i := good; i.Margin = d("1.1"); in.Initiatives = []Initiative{i} }},
		{"initiative probability above 1", func(in *DCFInput) {
			i := good
			i.SuccessProbability.Bull = d("1.1")
			in.Initiatives = []Initiative{i}
		}},
		{"initiative probability negative", func(in *DCFInput) {
			i := good
			i.SuccessProbability.Bear = d("-0.1")
			in.Initiatives = []Initiative{i}
		}},
		{"too many initiatives", func(in *DCFInput) {
			for n := 0; n <= MaxInitiatives; n++ {
				in.Initiatives = append(in.Initiatives, good)
			}
		}},
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
	// A valid initiative with probability 0 and an edge tax rate is accepted.
	in := dcfInput()
	in.Initiatives = []Initiative{good}
	in.Scenarios[0].TaxRate, in.Scenarios[0].CashConversion = d("0"), d("1")
	if _, err := ScenarioDCF(in); err != nil {
		t.Fatal(err)
	}
}

func TestReverseDCFWithBasisCashAndInitiatives(t *testing.T) {
	ini := []Initiative{{Name: "X", StartYear: 2, RevenueYear5: d("200"), Margin: d("0.2"), SuccessProbability: InitiativeProbabilities{Base: d("0.5")}}}
	base := ReverseDCFInput{
		BaseRevenue: d("1000"), NetDebt: d("200"), Shares: d("100"), TerminalMargin: d("0.2"), ExitMultiple: d("10"), DiscountRate: d("0.1"),
		Basis: BasisNetPE, StartMargin: dp("0.15"), CashConversion: d("0.5"), Initiatives: ini,
	}
	growth := d("0.08")
	sc := base.scenario()
	for i := range sc.RevenueGrowth {
		sc.RevenueGrowth[i] = growth
	}
	base.Price = calcScenario(base.BaseRevenue, base.NetDebt, base.Shares, sc, ini).vps
	res, err := ReverseDCF(base)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ReverseOK || res.ImpliedGrowth.Sub(growth).Abs().GreaterThan(d("0.00001")) {
		t.Fatalf("got %+v, want growth %s", res, growth)
	}
	// ScenarioDCF with identical scenarios agrees with the solved value.
	full, err := ScenarioDCF(DCFInput{BaseRevenue: base.BaseRevenue, NetDebt: base.NetDebt, Shares: base.Shares, Initiatives: ini,
		Scenarios: []Scenario{withName(sc, ScenarioBear, "0.3"), withName(sc, ScenarioBase, "0.4"), withName(sc, ScenarioBull, "0.3")}})
	if err != nil {
		t.Fatal(err)
	}
	// Only the base scenario carries a success probability here.
	baseVal := full.Scenarios[1].ValuePerShare
	if baseVal.Sub(base.Price).Abs().GreaterThan(d("0.0001")) {
		t.Errorf("ScenarioDCF base %s vs reverse price %s", baseVal, base.Price)
	}

	// net_pe skips the net debt that ebit_ev subtracts.
	ev, pe := base, base
	ev.Basis = BasisEBITEV
	ev.Initiatives, pe.Initiatives = nil, nil
	ev.Price, pe.Price = d("20"), d("20")
	rEV, _ := ReverseDCF(ev)
	rPE, _ := ReverseDCF(pe)
	if !rEV.ImpliedGrowth.GreaterThan(rPE.ImpliedGrowth) {
		t.Errorf("ebit_ev implied growth %s should exceed net_pe %s (net debt must be earned back)", rEV.ImpliedGrowth, rPE.ImpliedGrowth)
	}

	bad := base
	bad.CashConversion = d("2")
	if _, err := ReverseDCF(bad); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}
