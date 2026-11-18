package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPTAXUSDBRL(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.RawQuery
		if !strings.Contains(r.URL.Path, "CotacaoDolarPeriodo") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json;odata.metadata=minimal")
		_, _ = w.Write(fixture(t, "bcb", "olinda_ptax_usd_period.json"))
	}))
	defer srv.Close()
	rates, err := NewPTAX(srv.Client(), srv.URL).USDBRL(context.Background(), Date(2026, 11, 9), Date(2026, 11, 17))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotRaw, "@dataInicial='11-09-2026'") || !strings.Contains(gotRaw, "@dataFinalCotacao='11-17-2026'") {
		t.Errorf("dates must be MM-DD-YYYY: %s", gotRaw)
	}
	if len(rates) != 7 {
		t.Fatalf("got %d", len(rates))
	}
	last := rates[6]
	if !last.Date.Equal(Date(2026, 11, 17)) {
		t.Errorf("date %v", last.Date)
	}
	dec(t, last.Buy, "5.7845")
	dec(t, last.Sell, "5.7851")
	if last.At.Hour() != 13 || last.At.Minute() != 7 {
		t.Errorf("at %v", last.At)
	}
}

func sgsServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "sgs.12"):
			_, _ = w.Write(fixture(t, "bcb", "sgs12_cdi_daily.json"))
		case strings.Contains(r.URL.Path, "sgs.1/"):
			_, _ = w.Write(fixture(t, "bcb", "sgs1_usd_venda.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), queries...) }
}

func TestSGSCDI(t *testing.T) {
	srv, queries := sgsServer(t)
	pts, err := NewSGS(srv.Client(), srv.URL).CDI(context.Background(), Date(2026, 11, 3), Date(2026, 11, 18))
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 11 {
		t.Fatalf("got %d", len(pts))
	}
	if !pts[0].Date.Equal(Date(2026, 11, 3)) || !pts[10].Date.Equal(Date(2026, 11, 17)) {
		t.Errorf("dates %v %v", pts[0].Date, pts[10].Date)
	}
	dec(t, pts[10].Value, "0.050788")
	q := queries()
	if len(q) != 1 || !strings.Contains(q[0], "dataInicial=03/11/2026") || !strings.Contains(q[0], "dataFinal=18/11/2026") {
		t.Errorf("queries %v", q)
	}
}

func TestSGSChunksLongRanges(t *testing.T) {
	srv, queries := sgsServer(t)
	_, err := NewSGS(srv.Client(), srv.URL).Series(context.Background(), SGSSeriesCDI, Date(2000, 1, 1), Date(2026, 11, 18))
	if err != nil {
		t.Fatal(err)
	}
	q := queries()
	want := []string{
		"dataInicial=01/01/2000&dataFinal=31/12/2009",
		"dataInicial=01/01/2010&dataFinal=31/12/2019",
		"dataInicial=01/01/2020&dataFinal=18/11/2026",
	}
	if len(q) != len(want) {
		t.Fatalf("queries %v", q)
	}
	for i, w := range want {
		if !strings.Contains(q[i], w) {
			t.Errorf("chunk %d: %s does not contain %s", i, q[i], w)
		}
	}
}

func TestSGSRejectsInvertedRange(t *testing.T) {
	if _, err := NewSGS(nil, "http://127.0.0.1:0").Series(context.Background(), 12, Date(2026, 2, 1), Date(2026, 1, 1)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSGSLatestAndUSDFallback(t *testing.T) {
	srv, queries := sgsServer(t)
	c := NewSGS(srv.Client(), srv.URL)
	if _, err := c.Latest(context.Background(), 12, 5); err != nil {
		t.Fatal(err)
	}
	if q := queries(); !strings.Contains(q[0], "/dados/ultimos/5") {
		t.Errorf("%v", q)
	}
	fx, err := c.USDBRL(context.Background(), Date(2026, 11, 9), Date(2026, 11, 17))
	if err != nil {
		t.Fatal(err)
	}
	dec(t, fx[6].Sell, "5.7851")
}

func TestSGS406(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = w.Write([]byte(`{"error":"max 10 years"}`))
	}))
	defer srv.Close()
	_, err := NewSGS(srv.Client(), srv.URL).Latest(context.Background(), 12, 1)
	if err == nil || !strings.Contains(err.Error(), "HTTP 406") {
		t.Fatalf("got %v", err)
	}
}
