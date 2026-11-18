package engine

import "testing"

func TestForeignTax(t *testing.T) {
	r := d("0.15")
	// Gains 10k + dividends 2k = 12k, 1k loss carried -> 11k x 15% = 1650.
	y := ForeignTax(2026, r, d("10000"), d("2000"), d("600"), d("1000"))
	if !y.Base.Equal(d("12000")) || !y.Taxable.Equal(d("11000")) || !y.Tax.Equal(d("1650")) || !y.LossOut.IsZero() ||
		!y.NetOfWithholding.Equal(d("1050")) {
		t.Fatalf("got %+v", y)
	}
	// A net loss year carries forward and pays nothing.
	y = ForeignTax(2027, r, d("-5000"), d("1000"), d("300"), d("0"))
	if !y.Tax.IsZero() || !y.LossOut.Equal(d("4000")) || !y.NetOfWithholding.IsZero() {
		t.Fatalf("loss year %+v", y)
	}
}
