package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Hand-computed: start 1000; day 2 grows to 1100 (+10%); day 3 a 500
// contribution arrives and the portfolio ends at 1650, so the market move
// is (1650-500)/1100 = +4.545...%; day 4 a 200 withdrawal, ends 1386:
// (1386+200)/1650 = -3.878...%. TWR = 1.1 x 1.0454545 x 0.9612121 - 1 = 10.54%.
func TestTWRWithFlows(t *testing.T) {
	pts := []FlowPoint{
		{Date: "d1", Value: d("1000"), Flow: d("1000")},
		{Date: "d2", Value: d("1100"), Flow: d("0")},
		{Date: "d3", Value: d("1650"), Flow: d("500")},
		{Date: "d4", Value: d("1386"), Flow: d("-200")},
	}
	idx := TWR(pts)
	if !idx[0].Index.Equal(d("1")) || !idx[1].Index.Equal(d("1.1")) {
		t.Fatalf("early index = %+v", idx)
	}
	// 1.1 x 1150/1100 x 1586/1650 = 1.15 x 0.9612121 = 1.1053939...
	if got := idx[3].Index.Round(8); !got.Equal(d("1.10539394")) {
		t.Fatalf("final index = %s", idx[3].Index)
	}
	// Buying at 10 and closing at 11 on the first day is a +10% day.
	first := TWR([]FlowPoint{{Date: "a", Value: d("1100"), Flow: d("1000")}})
	if !first[0].Index.Equal(d("1.1")) {
		t.Fatalf("first-day move: %s", first[0].Index)
	}
	// A money-weighted view would differ: flows do not move the index.
	flat := TWR([]FlowPoint{{Date: "a", Value: d("100"), Flow: d("100")}, {Date: "b", Value: d("600"), Flow: d("500")}})
	if !flat[1].Index.Equal(d("1")) {
		t.Fatalf("a pure contribution must not change TWR: %s", flat[1].Index)
	}
}

func TestBenchmarkIndexes(t *testing.T) {
	c := CDIIndex([]string{"a", "b", "c"}, []decimal.Decimal{d("0.05"), d("0.05"), d("0.05")})
	if !c[2].Index.Equal(d("1.00100025")) {
		t.Fatalf("cdi = %+v", c)
	}
	p := PriceIndex([]string{"a", "b"}, []decimal.Decimal{d("100000"), d("103500")})
	if !p[1].Index.Equal(d("1.035")) {
		t.Fatalf("price = %+v", p)
	}
}
