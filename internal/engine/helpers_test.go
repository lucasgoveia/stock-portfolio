package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dp(s string) *decimal.Decimal { x := mustDec(s); return &x }

func eqD(t *testing.T, name string, got, want decimal.Decimal) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

// nearD compares within 1e-12.
func nearD(t *testing.T, name string, got, want decimal.Decimal) {
	t.Helper()
	if got.Sub(want).Abs().GreaterThan(d("0.000000000001")) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}
