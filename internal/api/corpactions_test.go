package api

import (
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestCorporateActionsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	ts := trades.NewService(conn, as)
	h := NewRouter(WithAssets(as), WithTrades(ts), WithCorporateActions(corpactions.NewService(conn, as, ts, nil, nil)))

	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-01-05","side":"buy","quantity":"100","price":"40"}`)
	code, a, _ := do(t, h, "POST", "/api/assets/1/corporate-actions", `{"kind":"split","ex_date":"2026-04-01","factor":"2"}`)
	if code != 201 || a["status"] != "confirmed" {
		t.Fatalf("record: %d %v", code, a)
	}
	if code, p, _ := do(t, h, "GET", "/api/assets/1/position", ""); code != 200 || p["quantity"] != "200" || p["avg_cost"] != "20" {
		t.Fatalf("position: %d %v", code, p)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/corporate-actions", `{"kind":"bonus","ex_date":"2026-05-01","factor":"1.1"}`); code != 400 {
		t.Fatalf("bonus without cost: %d", code)
	}
	if code, _, list := do(t, h, "GET", "/api/corporate-actions?asset_id=1", ""); code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-06-01","side":"sell","quantity":"150","price":"25"}`)
	if code, _, _ := do(t, h, "DELETE", "/api/corporate-actions/1", ""); code != 409 {
		t.Fatalf("delete needed split: %d", code)
	}
}
