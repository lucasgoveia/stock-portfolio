package api

import (
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestDividendsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	ts := trades.NewService(conn, as)
	cs := cash.NewService(conn, cash.NewRateStore(conn), nil)
	h := NewRouter(WithAssets(as), WithTrades(ts), WithCash(cs), WithDividends(dividends.NewService(conn, as, ts, cs, nil)))

	do(t, h, "POST", "/api/assets", `{"ticker":"BBSE3","market":"B3","kind":"stock"}`)
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-01-05","side":"buy","quantity":"200","price":"35"}`)
	do(t, h, "POST", "/api/cash/instruments", `{"kind":"cdi","name":"CDB"}`)

	code, dv, _ := do(t, h, "POST", "/api/assets/1/dividends", `{"kind":"dividend","record_date":"2026-08-01","pay_date":"2026-08-20","per_share":"1.5","instrument_id":1}`)
	if code != 201 || dv["gross"] != "300" || dv["net"] != "300" || dv["status"] != "confirmed" {
		t.Fatalf("record: %d %v", code, dv)
	}
	if code, s, _ := do(t, h, "GET", "/api/cash/summary", ""); code != 200 || s["brl"] != "300" {
		t.Fatalf("cash credited: %d %v", code, s)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/dividends", `{"kind":"bonus","record_date":"2026-08-01","pay_date":"2026-08-20","per_share":"1","instrument_id":1}`); code != 400 {
		t.Fatalf("bad kind: %d", code)
	}
	if code, _, list := do(t, h, "GET", "/api/dividends?status=confirmed", ""); code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "POST", "/api/dividends/1/reject", ""); code != 409 {
		t.Fatalf("reject confirmed: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/dividends/1", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "GET", "/api/dividends/1/confirm", ""); code != 404 && code != 405 {
		t.Fatalf("unknown route: %d", code)
	}
}
