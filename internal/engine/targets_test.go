package engine

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
)

func holdings(scores ...string) []CoreHolding {
	hs := make([]CoreHolding, len(scores))
	for i, s := range scores {
		hs[i] = CoreHolding{ID: fmt.Sprintf("S%02d", i+1), Score: d(s)}
	}
	return hs
}

func rep(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func sumWeights(r TargetResult) decimal.Decimal {
	s := zero
	for _, t := range r.Targets {
		s = s.Add(t.Weight)
	}
	return s
}

func TestComputeTargets(t *testing.T) {
	p := DefaultParams()
	big := d("100000")
	tests := []struct {
		name        string
		in          TargetInput
		wantBudget  string
		wantTooFew  bool
		wantTooMany bool
		wantUndep   string
		wantW       []string // expected weights by ID order; "" skips
		wantClamped []bool
	}{
		{
			name:       "ten equal stocks, budget 90%: no clamps",
			in:         TargetInput{Holdings: holdings("3", "3", "3", "3", "3", "3", "3", "3", "3", "3"), PortfolioValue: big, CashTarget: d("0.10")},
			wantBudget: "0.9",
			wantW:      []string{"0.09", "0.09", "0.09", "0.09", "0.09", "0.09", "0.09", "0.09", "0.09", "0.09"},
		},
		{
			name: "tactical weight shrinks core budget",
			in:   TargetInput{Holdings: holdings("3", "3", "3", "3", "3", "3", "3", "3", "3", "3"), PortfolioValue: big, CashTarget: d("0.04"), TacticalWeight: d("0.06")},
			// budget 0.90
			wantBudget: "0.9",
			wantW:      []string{"0.09"},
		},
		{
			name:       "clamp at top: one score 5 among low scores, 12 stocks",
			in:         TargetInput{Holdings: holdings("5", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1"), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96",
			// raw: 0.10 + 11*0.02 = 0.32 ; first = 0.10*0.96/0.32 = 0.30 -> clamp 10%
			// remaining 0.86 over 11 -> 0.078181... others not clamped at 2%? 0.86*0.02/0.22=0.0782 ok but > 10? no
			wantW:       []string{"0.1"},
			wantClamped: []bool{true},
		},
		{
			name:       "clamp at bottom: score 1 among score 5 names",
			in:         TargetInput{Holdings: holdings("1", "5", "5", "5", "5", "5", "5", "5", "5", "5", "5", "5"), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96",
			// raw: 0.02 + 11*0.10 = 1.12; first=0.02*.96/1.12=0.01714 <2% -> 2%; rest .94/11
			wantW:       []string{"0.02", "0.0854545454545454"},
			wantClamped: []bool{true, false},
		},
		{
			name:       "both ends clamped in the same pass",
			in:         TargetInput{Holdings: holdings("5", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1", "1"), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96",
			wantW:      []string{"0.1"},
		},
		{
			name: "redistribution converges over several passes",
			// two high scores clamp, which pushes a third over 10% on the next pass
			in:         TargetInput{Holdings: holdings("5", "5", "4.9", "1", "1", "1", "1", "1", "1", "1", "1", "1"), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96",
		},
		{
			name:       "too few stocks: all 10%, remainder undeployed",
			in:         TargetInput{Holdings: holdings("5", "3", "1"), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96", wantTooFew: true, wantUndep: "0.66",
			wantW: []string{"0.1", "0.1", "0.1"}, wantClamped: []bool{true, true, true},
		},
		{
			name:       "too many stocks: all 2%",
			in:         TargetInput{Holdings: holdings(rep(60, "1")...), PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96", wantTooMany: true,
		},
		{
			name:       "exactly n x 10% == budget is feasible (no undeployed)",
			in:         TargetInput{Holdings: holdings("1", "5", "3", "3", "3", "3", "3", "3", "3"), PortfolioValue: big, CashTarget: d("0.1")}, // n*0.1=0.9
			wantBudget: "0.9", wantW: []string{"0.1"},
		},
		{
			name:       "exactly n x 2% == budget is feasible",
			in:         TargetInput{Holdings: holdings(append([]string{"5"}, rep(44, "1")...)...), PortfolioValue: big, CashTarget: d("0.1")}, // 45*0.02=0.9
			wantBudget: "0.9",
		},
		{
			name:       "bootstrap: no clamps, pure proportional",
			in:         TargetInput{Holdings: holdings("5", "1"), PortfolioValue: d("1000"), CashTarget: d("0.04")},
			wantBudget: "0.96",
			// raw 0.10, 0.02 -> 0.8, 0.16
			wantW: []string{"0.8", "0.16"},
		},
		{
			name:       "bootstrap single stock takes whole budget",
			in:         TargetInput{Holdings: holdings("3"), PortfolioValue: d("0"), CashTarget: d("0.04")},
			wantBudget: "0.96", wantW: []string{"0.96"},
		},
		{
			name:       "threshold crossing: exactly 10000 is full rules",
			in:         TargetInput{Holdings: holdings("5", "3"), PortfolioValue: d("10000"), CashTarget: d("0.04")},
			wantBudget: "0.96", wantTooFew: true, wantUndep: "0.76", wantW: []string{"0.1", "0.1"},
		},
		{
			name: "just below threshold is bootstrap",
			in:   TargetInput{Holdings: holdings("5", "3"), PortfolioValue: d("9999.99"), CashTarget: d("0.04")},
			// raw 0.10, 0.06 -> 0.96*0.10/0.16, 0.96*0.06/0.16
			wantBudget: "0.96", wantW: []string{"0.6", "0.36"},
		},
		{
			name:       "no holdings",
			in:         TargetInput{PortfolioValue: big, CashTarget: d("0.04")},
			wantBudget: "0.96",
		},
		{
			name:       "budget cannot go negative",
			in:         TargetInput{Holdings: holdings("3"), PortfolioValue: big, CashTarget: d("0.5"), TacticalWeight: d("0.6")},
			wantBudget: "0", wantTooMany: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := p.ComputeTargets(tc.in)
			nearD(t, "budget", r.CoreBudget, d(tc.wantBudget))
			if r.TooFew != tc.wantTooFew || r.TooMany != tc.wantTooMany {
				t.Errorf("flags tooFew=%v tooMany=%v, want %v %v", r.TooFew, r.TooMany, tc.wantTooFew, tc.wantTooMany)
			}
			if tc.wantUndep != "" {
				nearD(t, "undeployed", r.Undeployed, d(tc.wantUndep))
			}
			for i, w := range tc.wantW {
				if w == "" || i >= len(r.Targets) {
					continue
				}
				nearD(t, "weight["+r.Targets[i].ID+"]", r.Targets[i].Weight, d(w))
			}
			for i, c := range tc.wantClamped {
				if r.Targets[i].Clamped != c {
					t.Errorf("clamped[%d] = %v, want %v", i, r.Targets[i].Clamped, c)
				}
			}
			if len(r.Targets) == 0 {
				return
			}
			// Invariant: targets + undeployed == budget, except too-many.
			if !r.TooMany {
				nearD(t, "sum+undeployed", sumWeights(r).Add(r.Undeployed), r.CoreBudget)
			}
			// Invariant: full rules keep every target in [2%,10%].
			if !r.Bootstrap {
				for _, tg := range r.Targets {
					if tg.Weight.LessThan(p.ClampMin) || tg.Weight.GreaterThan(p.ClampMax) {
						t.Errorf("%s weight %s outside clamp", tg.ID, tg.Weight)
					}
				}
			}
		})
	}
}

func TestClampRedistributionConvergence(t *testing.T) {
	p := DefaultParams()
	// Many top scores clamp at 10%; the leftover must flow to the rest and
	// still sum to the budget with every position in range.
	r := p.ComputeTargets(TargetInput{
		Holdings:       holdings("5", "5", "5", "5", "5", "1", "1", "1", "1", "1", "1", "1", "1", "1"),
		PortfolioValue: d("50000"), CashTarget: d("0.04"),
	})
	nearD(t, "sum", sumWeights(r), r.CoreBudget)
	clamped := 0
	for _, tg := range r.Targets {
		if tg.Clamped {
			clamped++
		}
	}
	if clamped == 0 {
		t.Fatal("expected clamped positions")
	}
	// Unclamped positions keep proportional weights (all raw equal here).
	var first decimal.Decimal
	for i, tg := range r.Targets {
		if tg.Clamped {
			continue
		}
		if i == 0 || first.IsZero() {
			first = tg.Weight
		}
		nearD(t, "equal unclamped", tg.Weight, first)
	}
}

func TestBandsAndStatus(t *testing.T) {
	p := DefaultParams()
	r := p.ComputeTargets(TargetInput{Holdings: holdings("3", "3", "3", "3", "3", "3", "3", "3", "3", "3"), PortfolioValue: d("100000"), CashTarget: d("0.1")})
	tg := r.Targets[0]
	nearD(t, "lower", tg.Lower, d("0.0675"))
	nearD(t, "upper", tg.Upper, d("0.1125"))
	// Upper capped at 12%.
	r2 := p.ComputeTargets(TargetInput{Holdings: holdings("5", "5", "5"), PortfolioValue: d("100000"), CashTarget: d("0.04")})
	eqD(t, "capped upper", r2.Targets[0].Upper, d("0.12"))

	tests := []struct {
		name      string
		actual    string
		bootstrap bool
		want      PositionStatus
	}{
		{"inside", "0.09", false, PositionStatus{}},
		{"above upper", "0.12", false, PositionStatus{AboveUpper: true}},
		{"above hard cap", "0.1201", false, PositionStatus{AboveUpper: true, AboveCap: true}},
		{"below lower", "0.05", false, PositionStatus{BelowLower: true}},
		{"below floor", "0.014", false, PositionStatus{BelowLower: true, BelowFloor: true}},
		{"bootstrap silences everything", "0.014", true, PositionStatus{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.Status(d(tc.actual), tg, tc.bootstrap); got != tc.want {
				t.Errorf("got %+v want %+v", got, tc.want)
			}
		})
	}

	// Bootstrap targets carry no bands.
	rb := p.ComputeTargets(TargetInput{Holdings: holdings("5"), PortfolioValue: d("100"), CashTarget: d("0.04")})
	eqD(t, "bootstrap lower", rb.Targets[0].Lower, zero)
	eqD(t, "bootstrap upper", rb.Targets[0].Upper, rb.Targets[0].Weight)
}

func TestBootstrapThreshold(t *testing.T) {
	p := DefaultParams()
	tests := []struct {
		prev, cur string
		boot      bool
		exit      bool
	}{
		{"9999.99", "9999.99", true, false},
		{"9999.99", "10000", false, true},
		{"10000", "20000", false, false},
		{"10000", "9000", true, false}, // falling back is not an exit
	}
	for _, tc := range tests {
		if got := p.IsBootstrap(d(tc.cur)); got != tc.boot {
			t.Errorf("IsBootstrap(%s)=%v", tc.cur, got)
		}
		if got := p.BootstrapExit(d(tc.prev), d(tc.cur)); got != tc.exit {
			t.Errorf("BootstrapExit(%s,%s)=%v", tc.prev, tc.cur, got)
		}
	}
}

func TestTargetsDeterministicOrder(t *testing.T) {
	p := DefaultParams()
	a := []CoreHolding{{"B", d("4")}, {"A", d("2")}, {"C", d("3")}}
	b := []CoreHolding{{"C", d("3")}, {"B", d("4")}, {"A", d("2")}}
	ra := p.ComputeTargets(TargetInput{Holdings: a, PortfolioValue: d("100"), CashTarget: d("0.04")})
	rb := p.ComputeTargets(TargetInput{Holdings: b, PortfolioValue: d("100"), CashTarget: d("0.04")})
	for i := range ra.Targets {
		if ra.Targets[i].ID != rb.Targets[i].ID || !ra.Targets[i].Weight.Equal(rb.Targets[i].Weight) || ra.Targets[i].ID != []string{"A", "B", "C"}[i] {
			t.Fatalf("order not deterministic: %v vs %v", ra.Targets, rb.Targets)
		}
	}
}
