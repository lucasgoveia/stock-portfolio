package engine

import "testing"

func TestDividendWithholding(t *testing.T) {
	p := DefaultDividendParams()
	for _, c := range []struct {
		name, kind, cur, gross, before, want string
	}{
		{"jcp 17.5%", DividendJCP, "BRL", "1000", "0", "175"},
		{"small dividend exempt", DividendCash, "BRL", "1000", "0", "0"},
		{"exactly 50k exempt", DividendCash, "BRL", "50000", "0", "0"},
		{"over 50k taxes the whole amount", DividendCash, "BRL", "60000", "0", "6000"},
		{"crossing the limit picks up earlier payments", DividendCash, "BRL", "20000", "40000", "6000"},
		{"already over: only this payment", DividendCash, "BRL", "10000", "60000", "1000"},
		{"us 30%", DividendCash, "USD", "12.34", "0", "3.7"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := p.Withholding(c.kind, c.cur, d(c.gross), d(c.before)); !got.Equal(d(c.want)) {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestYieldOnCost(t *testing.T) {
	if got := YieldOnCost(d("450"), d("10000")); !got.Equal(d("0.045")) {
		t.Fatalf("got %s", got)
	}
	if got := YieldOnCost(d("450"), d("0")); !got.IsZero() {
		t.Fatalf("zero cost: %s", got)
	}
}
