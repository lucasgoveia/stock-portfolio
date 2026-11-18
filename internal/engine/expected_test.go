package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

func baseScenario(growth ...string) Scenario {
	s := Scenario{Name: ScenarioBase, TerminalMargin: d("0.10"), ExitMultiple: d("10"), DiscountRate: d("0.1"), Probability: d("1")}
	for i, g := range growth {
		s.RevenueGrowth[i] = d(g)
	}
	return s
}

func TestExpectedForFlatGrowth(t *testing.T) {
	// No growth: every quarter is a quarter of the base revenue (400 / 4).
	// Margin runs 5% -> 10% over 5 years: year 1 is 6%, year 5 is 10%.
	in := ExpectedPathInput{BaseRevenue: d("400"), BaseNetMargin: dp("0.05"), Shares: d("10"), Base: baseScenario("0", "0", "0", "0", "0")}
	tests := []struct {
		n       int
		year    int
		revenue string
		eps     string
	}{
		{1, 1, "100", "0.6"},  // 100 x 6% / 10
		{4, 1, "100", "0.6"},  // 100 x 6% / 10
		{5, 2, "100", "0.7"},  // year 2 margin 7%
		{20, 5, "100", "1"},   // year 5 margin = terminal 10%
		{13, 4, "100", "0.9"}, // year 4 margin 9%
	}
	for _, tt := range tests {
		got, err := ExpectedFor(in, tt.n)
		if err != nil {
			t.Fatalf("quarter %d: %v", tt.n, err)
		}
		if got.Year != tt.year {
			t.Errorf("quarter %d: year %d, want %d", tt.n, got.Year, tt.year)
		}
		eqD(t, "revenue", got.Revenue, d(tt.revenue))
		if got.EPS == nil {
			t.Fatalf("quarter %d: no EPS", tt.n)
		}
		eqD(t, "eps", *got.EPS, d(tt.eps))
	}
}

func TestExpectedForGrowthSumsToAnnualPath(t *testing.T) {
	// 10% then 20% growth: year 1 = 440, year 2 = 528. The four quarters of a
	// year sum to the annual figure (within cent rounding) and grow at the
	// year's quarterly rate (1.1^(1/4), 1.2^(1/4)).
	in := ExpectedPathInput{BaseRevenue: d("400"), Shares: d("10"), Base: baseScenario("0.1", "0.2", "0", "0", "0")}
	for _, c := range []struct {
		year   int
		annual string
		rate   string
	}{{1, "440", "1.1"}, {2, "528", "1.2"}} {
		sum := decimal.Zero
		var q [4]decimal.Decimal
		for i := 0; i < 4; i++ {
			got, err := ExpectedFor(in, (c.year-1)*4+i+1)
			if err != nil {
				t.Fatal(err)
			}
			if got.EPS != nil {
				t.Errorf("EPS without a base margin should be nil, got %s", got.EPS)
			}
			q[i] = got.Revenue
			sum = sum.Add(got.Revenue)
		}
		if sum.Sub(d(c.annual)).Abs().GreaterThan(d("0.02")) {
			t.Errorf("year %d quarters sum to %s, want %s", c.year, sum, c.annual)
		}
		// Quarter 4 over quarter 1 is r^3 = rate^(3/4).
		r4 := sqrt(sqrt(d(c.rate)))
		want := r4.Mul(r4).Mul(r4)
		got := q[3].Div(q[0])
		if got.Sub(want).Abs().GreaterThan(d("0.0001")) {
			t.Errorf("year %d: q4/q1 = %s, want %s", c.year, got, want)
		}
	}
	// Hand computed: r = 1.1^(1/4) = 1.024114, 1+r+r^2+r^3 = 4.146950, so
	// quarter 1 is 440 / 4.14695 = 106.10.
	got, _ := ExpectedFor(in, 1)
	if got.Revenue.Sub(d("106.10")).Abs().GreaterThan(d("0.01")) {
		t.Errorf("quarter 1 revenue = %s, want about 106.10", got.Revenue)
	}
}

func TestExpectedForEPSUndefined(t *testing.T) {
	base := ExpectedPathInput{BaseRevenue: d("400"), BaseNetMargin: dp("0.05"), Shares: d("10"), Base: baseScenario("0", "0", "0", "0", "0")}
	noMargin := base
	noMargin.BaseNetMargin = nil
	noShares := base
	noShares.Shares = decimal.Zero
	for name, in := range map[string]ExpectedPathInput{"no margin": noMargin, "no shares": noShares} {
		got, err := ExpectedFor(in, 3)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.EPS != nil {
			t.Errorf("%s: EPS = %s, want nil", name, got.EPS)
		}
		eqD(t, name+" revenue", got.Revenue, d("100"))
	}
	// A negative base margin works: -10% -> 10% over 5 years, year 1 is -6%.
	neg := base
	neg.BaseNetMargin = dp("-0.10")
	got, err := ExpectedFor(neg, 1)
	if err != nil {
		t.Fatal(err)
	}
	eqD(t, "negative margin eps", *got.EPS, d("-0.6"))
}

func TestExpectedForValidation(t *testing.T) {
	in := ExpectedPathInput{BaseRevenue: d("400"), Shares: d("10"), Base: baseScenario("0", "0", "0", "0", "0")}
	for _, n := range []int{0, -1, 21} {
		if _, err := ExpectedFor(in, n); err == nil {
			t.Errorf("quarter %d accepted", n)
		}
	}
	zero := in
	zero.BaseRevenue = decimal.Zero
	if _, err := ExpectedFor(zero, 1); err == nil {
		t.Error("zero base revenue accepted")
	}
	bad := in
	bad.Base = baseScenario("-1", "0", "0", "0", "0")
	if _, err := ExpectedFor(bad, 1); err == nil {
		t.Error("growth of -100% accepted")
	}
}

func TestQuartersAfter(t *testing.T) {
	for _, tt := range []struct{ days, want int }{
		{90, 1},  // 2025-12-31 to 2026-03-31
		{181, 2}, // to 2026-06-30
		{273, 3},
		{365, 4},
		{91, 1},
		{728, 8}, // 52-week fiscal calendars drift a few days
		{1826, 20},
	} {
		if got := QuartersAfter(tt.days); got != tt.want {
			t.Errorf("QuartersAfter(%d) = %d, want %d", tt.days, got, tt.want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name             string
		actual, expected string
		tol              string
		delta            string
		want             Outcome
	}{
		{"beat", "110", "100", "0.02", "0.1", OutcomeMet},
		{"exact", "100", "100", "0.02", "0", OutcomeMet},
		{"within tolerance", "98", "100", "0.02", "-0.02", OutcomeMet},
		{"just below tolerance", "97.99", "100", "0.02", "-0.0201", OutcomeMiss},
		{"miss, no tolerance", "99.99", "100", "0", "-0.0001", OutcomeMiss},
		{"negative expectation, better loss", "-0.5", "-1", "0.02", "0.5", OutcomeMet},
		{"negative expectation, worse loss", "-1.5", "-1", "0.02", "-0.5", OutcomeMiss},
	}
	for _, tt := range tests {
		delta, got := Compare(d(tt.actual), d(tt.expected), d(tt.tol))
		if got != tt.want {
			t.Errorf("%s: outcome %s, want %s", tt.name, got, tt.want)
		}
		if delta == nil {
			t.Fatalf("%s: nil delta", tt.name)
		}
		eqD(t, tt.name+" delta", *delta, d(tt.delta))
	}
	if delta, _ := Compare(d("1"), decimal.Zero, d("0.02")); delta != nil {
		t.Errorf("delta against a zero expectation = %s, want nil", delta)
	}
}

func TestMissStreak(t *testing.T) {
	m, ok, na := OutcomeMiss, OutcomeMet, OutcomeNA
	tests := []struct {
		name string
		in   []Outcome
		want int
	}{
		{"empty", nil, 0},
		{"one miss", []Outcome{ok, m}, 1},
		{"two misses", []Outcome{ok, m, m}, 2},
		{"beat after misses resets", []Outcome{m, m, ok}, 0},
		{"gap breaks the streak", []Outcome{m, na, m}, 1},
		{"unavailable latest", []Outcome{m, m, na}, 0},
		{"all misses", []Outcome{m, m, m, m}, 4},
	}
	for _, tt := range tests {
		if got := MissStreak(tt.in); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
}
