package portfolio

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
)

// fractional matches a JSON decimal string with more than 8 fractional digits.
var fractional = regexp.MustCompile(`"-?\d+\.\d{9,}"`)

func TestSnapshotDecimalsAreRounded(t *testing.T) {
	e := setup(t)
	s, err := e.svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if m := fractional.FindAllString(string(b), -1); len(m) > 0 {
		t.Fatalf("decimals with more than 8 fractional digits: %v", m)
	}
	// BRL amounts are centavos; weights keep 8 places.
	if s.TotalValue.Round(2).Cmp(s.TotalValue) != 0 || s.CashWeight.Round(8).Cmp(s.CashWeight) != 0 {
		t.Fatalf("total %s cash weight %s", s.TotalValue, s.CashWeight)
	}
	for _, h := range s.Holdings {
		if h.ValueBRL.Round(2).Cmp(h.ValueBRL) != 0 || h.Weight.Round(8).Cmp(h.Weight) != 0 {
			t.Fatalf("%s value %s weight %s", h.Ticker, h.ValueBRL, h.Weight)
		}
	}
}
