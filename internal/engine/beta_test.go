package engine

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// series builds weekly Friday closes from log returns.
func series(start time.Time, rets []float64, base float64) []PricePoint {
	out := []PricePoint{{Date: start, Close: decimal.NewFromFloat(base)}}
	v := base
	for i, r := range rets {
		v *= math.Exp(r)
		out = append(out, PricePoint{Date: start.AddDate(0, 0, 7*(i+1)), Close: decimal.NewFromFloat(v)})
	}
	return out
}

func TestWeeklyBeta(t *testing.T) {
	start := time.Date(2024, 11, 15, 0, 0, 0, 0, time.UTC) // a Friday
	idx := make([]float64, 104)
	for i := range idx {
		idx[i] = 0.02 * math.Sin(float64(i)*1.3)
	}
	scaled := func(k float64) []float64 {
		out := make([]float64, len(idx))
		for i := range idx {
			out[i] = k * idx[i]
		}
		return out
	}
	tests := []struct {
		name  string
		asset []float64
		want  string
		ok    bool
	}{
		{"beta 1.5", scaled(1.5), "1.5", true},
		{"beta 0.5", scaled(0.5), "0.5", true},
		{"too short", scaled(1)[:40], "0", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := WeeklyBeta(series(start, tc.asset, 30), series(start, idx[:len(tc.asset)], 120000))
			if r.OK != tc.ok {
				t.Fatalf("ok = %v (weeks %d)", r.OK, r.Weeks)
			}
			if tc.ok && r.Beta.Sub(d(tc.want)).Abs().GreaterThan(d("0.0001")) {
				t.Fatalf("beta = %s, want %s", r.Beta, tc.want)
			}
		})
	}

	// A 1:3 split shows as a -ln(3) weekly move and is ignored.
	rets := scaled(1)
	rets[50] = -math.Log(3)
	r := WeeklyBeta(series(start, rets, 30), series(start, idx, 120000))
	if !r.OK || r.Weeks != 103 {
		t.Fatalf("split week not dropped: %+v", r)
	}

	// Daily points collapse to the last close of each ISO week.
	var daily []PricePoint
	for i, p := range series(start, scaled(1), 30) {
		daily = append(daily, PricePoint{Date: p.Date.AddDate(0, 0, -2), Close: d("1")}, p)
		_ = i
	}
	if r := WeeklyBeta(daily, series(start, idx, 120000)); !r.OK || r.Beta.Sub(d("1")).Abs().GreaterThan(d("0.0001")) {
		t.Fatalf("daily collapse: %+v", r)
	}
}

func TestVolatilityScore(t *testing.T) {
	for in, want := range map[string]int{"0.2": 5, "0.69": 5, "0.7": 4, "0.89": 4, "0.9": 3, "1.09": 3, "1.1": 2, "1.4": 2, "1.41": 1, "-0.3": 5} {
		if got := VolatilityScore(d(in)); got != want {
			t.Errorf("VolatilityScore(%s) = %d, want %d", in, got, want)
		}
	}
}
