package api

import (
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/performance"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestPerformanceAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	ts := trades.NewService(conn, as)
	cs := cash.NewService(conn, cash.NewRateStore(conn), nil)
	h := NewRouter(WithAssets(as), WithTrades(ts), WithPerformance(performance.NewService(conn, as, ts, cs, nil)))

	if code, r, _ := do(t, h, "GET", "/api/performance", ""); code != 200 || r["series"] == nil {
		t.Fatalf("empty report: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "GET", "/api/performance?from=bad", ""); code != 400 {
		t.Fatalf("bad date: %d", code)
	}
	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-11-02","side":"buy","quantity":"10","price":"40"}`)
	code, b, _ := do(t, h, "POST", "/api/performance/backfill", "")
	if code != 200 || b["days"] == float64(0) {
		t.Fatalf("backfill: %d %v", code, b)
	}
	code, r, _ := do(t, h, "GET", "/api/performance?from=2026-11-01", "")
	segs, _ := r["segments"].([]any)
	if code != 200 || len(segs) == 0 {
		t.Fatalf("report: %d %v", code, r)
	}
}
