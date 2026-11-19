package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEDGARFinancialsForeignPrivateIssuer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "edgar", "companyfacts_NU_trimmed.json"))
	}))
	defer srv.Close()
	f, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "test-agent ops@example.com").Financials(context.Background(), "1691493")
	if err != nil {
		t.Fatal(err)
	}
	if f.Currency != "USD" {
		t.Errorf("currency %q", f.Currency)
	}
	var annual []FinancialPeriod
	for _, p := range f.Periods {
		if p.Type == PeriodAnnual {
			annual = append(annual, p)
		}
	}
	if len(annual) < 3 {
		t.Fatalf("annual periods: %d", len(annual))
	}
	for _, p := range annual[len(annual)-3:] {
		for _, c := range []string{ConceptNetIncome, ConceptEquity, ConceptRevenue, ConceptCash, ConceptDebt, ConceptOCF} {
			if _, ok := p.Values[c]; !ok {
				t.Errorf("%s lacks %s", p.End.Format("2006-01-02"), c)
			}
		}
	}
	last := annual[len(annual)-1]
	if last.End.Format("2006-01-02") != "2024-12-31" || last.Values[ConceptNetIncome].String() != "1972112000" {
		t.Errorf("last annual: %s %v", last.End.Format("2006-01-02"), last.Values[ConceptNetIncome])
	}
	var quarterlyNote bool
	for _, n := range f.Notes {
		if strings.Contains(n, "quarterly figures unavailable") {
			quarterlyNote = true
		}
		if strings.Contains(n, "not supported") {
			t.Errorf("stale note %q", n)
		}
	}
	if !quarterlyNote {
		t.Errorf("notes: %v", f.Notes)
	}
}

func TestEDGARFinancialsNonUSDCurrency(t *testing.T) {
	fact := func(start, end, val string) []edgarFact {
		return []edgarFact{{Start: start, End: end, Val: json.Number(val), Form: "20-F", Filed: "2025-03-01"}}
	}
	doc := edgarDoc{EntityName: "Foreign SA", Facts: map[string]map[string]edgarConcept{"ifrs-full": {
		"Revenue":                                {Units: map[string][]edgarFact{"BRL": fact("2024-01-01", "2024-12-31", "100")}},
		"ProfitLossAttributableToOwnersOfParent": {Units: map[string][]edgarFact{"BRL": fact("2024-01-01", "2024-12-31", "10")}},
		"DilutedEarningsLossPerShare":            {Units: map[string][]edgarFact{"BRL/shares": fact("2024-01-01", "2024-12-31", "0.5")}},
		"ShorttermBorrowings":                    {Units: map[string][]edgarFact{"BRL": fact("", "2024-12-31", "7")}},
		"OtherBorrowings":                        {Units: map[string][]edgarFact{"BRL": fact("", "2024-12-31", "3")}},
	}}}
	f := doc.financials()
	if f.Currency != "BRL" {
		t.Fatalf("currency %q", f.Currency)
	}
	if len(f.Periods) != 1 {
		t.Fatalf("periods: %+v", f.Periods)
	}
	v := f.Periods[0].Values
	if v[ConceptRevenue].String() != "100" || v[ConceptEPSDiluted].String() != "0.5" || v[ConceptDebt].String() != "10" {
		t.Errorf("values: %v", v)
	}
	var note bool
	for _, n := range f.Notes {
		if strings.Contains(n, "BRL") {
			note = true
		}
	}
	if !note {
		t.Errorf("no currency note: %v", f.Notes)
	}
}
