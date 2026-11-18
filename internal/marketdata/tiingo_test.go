package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fixture is hand-written from Tiingo's documentation: unverified against
// the live API.
func TestTiingoDailyPrices(t *testing.T) {
	var gotAuth, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "tiingo", "daily_prices_AAPL.json"))
	}))
	defer srv.Close()
	bars, err := NewTiingo(srv.Client(), srv.URL, "KEY").DailyPrices(context.Background(), "AAPL", Date(2020, 8, 28), Date(2020, 11, 6))
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token KEY" || gotPath != "/tiingo/daily/aapl/prices" ||
		!strings.Contains(gotQuery, "startDate=2020-08-28") || !strings.Contains(gotQuery, "endDate=2020-11-06") {
		t.Errorf("request: %s %s %s", gotAuth, gotPath, gotQuery)
	}
	if len(bars) != 3 {
		t.Fatalf("bars %d", len(bars))
	}
	dec(t, bars[0].Close, "499.23")
	dec(t, bars[0].AdjClose, "124.8075")
	dec(t, bars[1].SplitFactor, "4")
	dec(t, bars[2].DivCash, "0.205")
	if !bars[2].Date.Equal(Date(2020, 11, 6)) {
		t.Errorf("date %v", bars[2].Date)
	}
}

func TestTiingoErrors(t *testing.T) {
	if _, err := NewTiingo(nil, "", "").DailyPrices(context.Background(), "AAPL", Date(2020, 1, 1), Date(2020, 1, 2)); err == nil {
		t.Error("expected missing token error")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"quota"}`))
	}))
	defer srv.Close()
	_, err := NewTiingo(srv.Client(), srv.URL, "K").DailyPrices(context.Background(), "AAPL", Date(2020, 1, 1), Date(2020, 1, 2))
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("got %v", err)
	}
}
