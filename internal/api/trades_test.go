package api

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestTradesAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	h := NewRouter(WithAssets(as), WithTrades(trades.NewService(conn, as)))

	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)
	do(t, h, "POST", "/api/assets", `{"ticker":"GOOGL","market":"US","kind":"stock"}`)

	code, tr, _ := do(t, h, "POST", "/api/assets/1/trades", `{"date":"2025-01-02","side":"buy","quantity":"100","price":"10","fees":"5"}`)
	if code != 201 || tr["quantity"] != "100" {
		t.Fatalf("create trade: %d %v", code, tr)
	}
	if code, a, _ := do(t, h, "GET", "/api/assets/1", ""); code != 200 || a["status"] != "holding" {
		t.Fatalf("status after buy: %v", a)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/trades", `{"date":"2025-01-03","side":"sell","quantity":"101","price":"10"}`); code != http.StatusConflict {
		t.Fatalf("oversell: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/trades", `{"date":"x","side":"buy","quantity":"1","price":"1"}`); code != 400 {
		t.Fatalf("bad date: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/trades", `{"date":"2025-01-03","side":"buy","quantity":1,"price":"1"}`); code != 400 {
		t.Fatalf("numeric decimal must be rejected: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/99/trades", `{"date":"2025-01-03","side":"buy","quantity":"1","price":"1"}`); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
	if code, _, list := do(t, h, "GET", "/api/assets/1/trades", ""); code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, list)
	}

	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2025-02-02","side":"sell","quantity":"40","price":"12.5","fees":"5"}`)
	code, pos, _ := do(t, h, "GET", "/api/assets/1/position", "")
	// cost 1005, sell 40 -> costOut 402, proceeds 495, gain 93, remaining 60 @ 10.05.
	if code != 200 || pos["quantity"] != "60" || pos["avg_cost"] != "10.05" || pos["realized_gain"] != "93" {
		t.Fatalf("position: %d %v", code, pos)
	}

	// US trade keeps USD and BRL.
	if code, _, _ := do(t, h, "POST", "/api/assets/2/trades", `{"date":"2025-01-02","side":"buy","quantity":"2","price":"100"}`); code != 400 {
		t.Fatalf("usd needs fx: %d", code)
	}
	do(t, h, "POST", "/api/assets/2/trades", `{"date":"2025-01-02","side":"buy","quantity":"2","price":"100","fx_rate":"5"}`)
	code, pos, _ = do(t, h, "GET", "/api/assets/2/position", "")
	if code != 200 || pos["total_cost"] != "200" || pos["total_cost_brl"] != "1000" {
		t.Fatalf("us position: %v", pos)
	}
	if code, _, list := do(t, h, "GET", "/api/positions", ""); code != 200 || len(list) != 2 {
		t.Fatalf("positions: %d %v", code, list)
	}

	// Delete: removing the buy would make the sell invalid.
	if code, _, _ := do(t, h, "DELETE", "/api/trades/1", ""); code != http.StatusConflict {
		t.Fatalf("delete buy under sell: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/trades/2", ""); code != 204 {
		t.Fatalf("delete sell: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/trades/2", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/trades/x", ""); code != 400 {
		t.Fatalf("delete bad id: %d", code)
	}
}
