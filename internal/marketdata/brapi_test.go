package marketdata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func brapiServer(t *testing.T, routes map[string][]byte, status map[string]int) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(context.Background()))
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if s := status[r.URL.Path]; s != 0 {
			w.WriteHeader(s)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestBrapiLatestQuote(t *testing.T) {
	srv, seen := brapiServer(t, map[string][]byte{"/api/v2/stocks/quote": fixture(t, "brapi", "v2_stocks_quote_PETR4.json")}, nil)
	b := NewBrapi(srv.Client(), srv.URL, "tok123")
	q, err := b.LatestQuote(context.Background(), "PETR4")
	if err != nil {
		t.Fatal(err)
	}
	if q.Ticker != "PETR4" || q.Currency != "BRL" {
		t.Errorf("%+v", q)
	}
	dec(t, q.Price, "43.53")
	if !q.Date.Equal(Date(2026, 11, 18)) {
		t.Errorf("date %v", q.Date)
	}
	r := (*seen)[0]
	if r.URL.Query().Get("symbols") != "PETR4" || r.Header.Get("Authorization") != "Bearer tok123" {
		t.Errorf("request: %s %v", r.URL, r.Header)
	}
}

func TestBrapiNoTokenOmitsHeader(t *testing.T) {
	srv, seen := brapiServer(t, map[string][]byte{"/api/v2/stocks/quote": fixture(t, "brapi", "v2_stocks_quote_PETR4.json")}, nil)
	if _, err := NewBrapi(srv.Client(), srv.URL, "").LatestQuote(context.Background(), "PETR4"); err != nil {
		t.Fatal(err)
	}
	if h := (*seen)[0].Header.Get("Authorization"); h != "" {
		t.Errorf("unexpected auth header %q", h)
	}
}

func TestBrapiUnauthorized(t *testing.T) {
	srv, _ := brapiServer(t, map[string][]byte{"/api/v2/stocks/quote": fixture(t, "brapi", "quote_nonsandbox_401.json")},
		map[string]int{"/api/v2/stocks/quote": 401})
	_, err := NewBrapi(srv.Client(), srv.URL, "").LatestQuote(context.Background(), "WEGE3")
	var he *HTTPError
	if err == nil || !errors.As(err, &he) || he.Status != 401 || !strings.Contains(he.Body, "MISSING_TOKEN") {
		t.Fatalf("got %v", err)
	}
}

func TestBrapiCorporateActions(t *testing.T) {
	srv, seen := brapiServer(t, map[string][]byte{"/api/v2/stocks/dividends": fixture(t, "brapi", "v2_stocks_dividends_PETR4.json")}, nil)
	ca, err := NewBrapi(srv.Client(), srv.URL, "t").CorporateActions(context.Background(), "PETR4", Date(2026, 1, 1), Date(2026, 12, 31))
	if err != nil {
		t.Fatal(err)
	}
	q := (*seen)[0].URL.Query()
	if q.Get("startDate") != "2026-01-01" || q.Get("endDate") != "2026-12-31" {
		t.Errorf("query %v", q)
	}
	if len(ca.CashDividends) != 8 {
		t.Fatalf("cash dividends: %d", len(ca.CashDividends))
	}
	jcp := ca.CashDividends[0]
	if jcp.Label != "JCP" || jcp.ISIN != "BRPETRACNPR6" || !jcp.Verified {
		t.Errorf("%+v", jcp)
	}
	dec(t, jcp.Rate, "0.20250435")
	if jcp.ExDate == nil || !jcp.ExDate.Equal(Date(2026, 8, 24)) {
		t.Errorf("exDate %v", jcp.ExDate)
	}
	if !jcp.LastDatePrior.Equal(Date(2026, 8, 21)) {
		t.Errorf("lastDatePrior %v", jcp.LastDatePrior)
	}
	if jcp.PaymentDate == nil || !jcp.PaymentDate.Equal(Date(2026, 12, 21)) {
		t.Errorf("paymentDate %v", jcp.PaymentDate)
	}
	labels := map[string]bool{}
	for _, s := range ca.StockDividends {
		labels[s.Label] = true
		if s.ExDate != nil {
			t.Errorf("exDate should be null in fixture: %+v", s)
		}
		if s.Label == "DESDOBRAMENTO" {
			dec(t, s.Factor, "2")
			if s.CompleteFactor != "2 para 1" {
				t.Errorf("complete factor %q", s.CompleteFactor)
			}
		}
		if s.Label == "GRUPAMENTO" {
			dec(t, s.Factor, "0.01")
		}
	}
	for _, l := range []string{"DESDOBRAMENTO", "GRUPAMENTO", "BONIFICACAO"} {
		if !labels[l] {
			t.Errorf("missing %s in %v", l, labels)
		}
	}
	if len(ca.Subscriptions) != 0 {
		t.Errorf("subscriptions: %v", ca.Subscriptions)
	}
}

func TestBrapiSubscriptionsKeepRaw(t *testing.T) {
	body := []byte(`{"results":[{"requestedSymbol":"X1","symbol":"X1","data":{"cashDividends":[],"stockDividends":[],"subscriptions":[{"label":"SUBSCRICAO","isinCode":"BRX1","exDate":"2026-01-05T03:00:00.000Z","price":10.5}]}}]}`)
	srv, _ := brapiServer(t, map[string][]byte{"/api/v2/stocks/dividends": body}, nil)
	ca, err := NewBrapi(srv.Client(), srv.URL, "t").CorporateActions(context.Background(), "X1", Date(2026, 1, 1).AddDate(-1, 0, 0), Date(2026, 1, 1).AddDate(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(ca.Subscriptions) != 1 || ca.Subscriptions[0].Label != "SUBSCRICAO" || !strings.Contains(string(ca.Subscriptions[0].Raw), "10.5") {
		t.Fatalf("%+v", ca.Subscriptions)
	}
	if ca.Subscriptions[0].ExDate == nil || !ca.Subscriptions[0].ExDate.Equal(Date(2026, 1, 5)) {
		t.Errorf("ex %v", ca.Subscriptions[0].ExDate)
	}
}

func TestBrapiHTMLResponseRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer srv.Close()
	if _, err := NewBrapi(srv.Client(), srv.URL, "").LatestQuote(context.Background(), "PETR4"); err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("got %v", err)
	}
}

func TestBrapiContextCancelled(t *testing.T) {
	srv, _ := brapiServer(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewBrapi(srv.Client(), srv.URL, "").LatestQuote(ctx, "PETR4"); err == nil {
		t.Fatal("expected error")
	}
}
