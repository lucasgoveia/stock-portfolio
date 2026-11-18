package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEDGARFinancials(t *testing.T) {
	var gotUA, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotPath = r.Header.Get("User-Agent"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "edgar", "companyfacts_AAPL_fundamentals.json"))
	}))
	defer srv.Close()
	f, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "test-agent ops@example.com").Financials(context.Background(), "320193")
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "test-agent ops@example.com" || gotPath != "/api/xbrl/companyfacts/CIK0000320193.json" {
		t.Errorf("request: %q %q", gotUA, gotPath)
	}
	if f.Source != "sec" || f.Currency != "USD" || f.Entity != "Apple Inc." {
		t.Errorf("header: %+v", f)
	}
	var annual int
	var latest FinancialPeriod
	for _, p := range f.Periods {
		if p.Type == PeriodAnnual {
			annual++
			latest = p
		}
	}
	if annual < 5 {
		t.Fatalf("annual periods: %d", annual)
	}
	for _, c := range []string{ConceptRevenue, ConceptEBIT, ConceptNetIncome, ConceptEPSDiluted, ConceptEquity,
		ConceptCash, ConceptDebt, ConceptOCF, ConceptCapex, ConceptDA, ConceptDPS, ConceptSharesDilAvg} {
		if _, ok := latest.Values[c]; !ok {
			t.Errorf("latest annual period %s lacks %s", latest.End.Format("2006-01-02"), c)
		}
	}
	// Sorted ascending, no duplicate (end, type).
	seen := map[string]bool{}
	for i, p := range f.Periods {
		k := p.End.Format("2006-01-02") + p.Type
		if seen[k] || (i > 0 && p.End.Before(f.Periods[i-1].End)) {
			t.Errorf("order/duplicate at %d %s", i, k)
		}
		seen[k] = true
	}
	// The cover share count is the latest dei value.
	var cover bool
	for _, p := range f.Periods {
		if v, ok := p.Values[ConceptSharesCover]; ok && v.IsPositive() {
			cover = true
		}
	}
	if !cover {
		t.Error("missing shares_cover")
	}
}

func TestEDGARConceptFallbackAndRestatement(t *testing.T) {
	doc := `{"entityName":"Acme","facts":{"us-gaap":{
	 "SalesRevenueNet":{"units":{"USD":[
	  {"start":"2019-01-01","end":"2019-12-31","val":100,"form":"10-K","filed":"2020-02-01"},
	  {"start":"2019-01-01","end":"2019-12-31","val":110,"form":"10-K","filed":"2021-02-01"},
	  {"start":"2019-10-01","end":"2019-12-31","val":30,"form":"10-K","filed":"2020-02-01"}]}},
	 "Revenues":{"units":{"USD":[
	  {"start":"2020-01-01","end":"2020-12-31","val":200,"form":"10-K","filed":"2021-02-01"},
	  {"start":"2021-01-01","end":"2021-12-31","val":300,"form":"10-Q","filed":"2021-05-01"}]}},
	 "LongTermDebt":{"units":{"USD":[{"end":"2020-12-31","val":50,"form":"10-K","filed":"2021-02-01"}]}},
	 "CommercialPaper":{"units":{"USD":[{"end":"2020-12-31","val":5,"form":"10-K","filed":"2021-02-01"}]}}
	}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(doc))
	}))
	defer srv.Close()
	got, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "").Financials(context.Background(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Periods) != 2 {
		t.Fatalf("periods: %+v", got.Periods)
	}
	// 2019: SalesRevenueNet fallback, restated value (latest filing), quarter ignored.
	if v := got.Periods[0].Values[ConceptRevenue]; v.String() != "110" {
		t.Errorf("2019 revenue %s", v.String())
	}
	// 2020: Revenues; LongTermDebt plus commercial paper.
	if v := got.Periods[1].Values[ConceptRevenue]; v.String() != "200" {
		t.Errorf("2020 revenue %s", v.String())
	}
	if v := got.Periods[1].Values[ConceptDebt]; v.String() != "55" {
		t.Errorf("2020 debt %s", v.String())
	}
}

func TestEDGARHTMLBlockPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Your Request Originates from an Undeclared Automated Tool</html>"))
	}))
	defer srv.Close()
	_, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "").Financials(context.Background(), "320193")
	if err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("got %v", err)
	}
	if _, err := NewEDGAR(srv.Client(), srv.URL, srv.URL, "").Financials(context.Background(), "abc"); err == nil {
		t.Error("expected invalid CIK error")
	}
}

func TestEDGARCIKForTicker(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "edgar", "company_tickers_head.json"))
	}))
	defer srv.Close()
	e := NewEDGAR(srv.Client(), srv.URL, srv.URL, "")
	cik, err := e.CIKForTicker(context.Background(), "aapl")
	if err != nil || cik != "0000320193" || gotPath != "/files/company_tickers.json" {
		t.Fatalf("got %q %v %s", cik, err, gotPath)
	}
	if _, err := e.CIKForTicker(context.Background(), "NOPE"); err == nil {
		t.Error("expected not found")
	}
}
