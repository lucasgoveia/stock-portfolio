package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestCashAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rs := cash.NewRateStore(conn)
	clock := func() time.Time { return time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC) }
	h := NewRouter(WithCash(cash.NewService(conn, rs, clock)))
	ctx := context.Background()
	_ = rs.UpsertCDI(ctx, "2025-01-03", decimal.RequireFromString("1"))
	_ = rs.UpsertCDI(ctx, "2025-01-06", decimal.RequireFromString("1"))
	_ = rs.UpsertUSDBRL(ctx, "2025-01-03", decimal.RequireFromString("5"))

	if code, i, _ := do(t, h, "POST", "/api/cash/instruments", `{"kind":"cdi","name":"CDB","pct_cdi":"100"}`); code != 201 || i["pct_cdi"] != "100" {
		t.Fatalf("create cdi: %d %v", code, i)
	}
	if code, _, _ := do(t, h, "POST", "/api/cash/instruments", `{"kind":"usd_cash","name":"USD"}`); code != 201 {
		t.Fatalf("create usd: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/cash/instruments", `{"kind":"nope","name":"x"}`); code != 400 {
		t.Fatalf("bad kind: %d", code)
	}
	if code, _, list := do(t, h, "GET", "/api/cash/instruments", ""); code != 200 || len(list) != 2 {
		t.Fatalf("list: %d %v", code, list)
	}
	if code, m, _ := do(t, h, "POST", "/api/cash/instruments/1/movements", `{"date":"2025-01-02","type":"deposit","amount":"1000"}`); code != 201 || m["amount"] != "1000" {
		t.Fatalf("movement: %d %v", code, m)
	}
	do(t, h, "POST", "/api/cash/instruments/2/movements", `{"date":"2025-01-02","type":"deposit","amount":"100"}`)
	if code, _, _ := do(t, h, "POST", "/api/cash/instruments/2/movements", `{"date":"2025-01-03","type":"withdraw","amount":"101"}`); code != 409 {
		t.Fatalf("overdraw: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/cash/instruments/9/movements", `{"date":"2025-01-03","type":"deposit","amount":"1"}`); code != 404 {
		t.Fatalf("missing instrument: %d", code)
	}
	code, s, _ := do(t, h, "GET", "/api/cash/summary", "")
	// 1000 x 1.01 x 1.01 = 1020.1 BRL; USD 100 x 5 = 500.
	if code != 200 || s["brl"] != "1020.1" || s["usd_in_brl"] != "500" || s["total_brl"] != "1520.1" || s["usd_balance"] != "100" {
		t.Fatalf("summary: %d %v", code, s)
	}
	if code, _, _ := do(t, h, "GET", "/api/cash/summary?as_of=bad", ""); code != 400 {
		t.Fatalf("bad as_of: %d", code)
	}
	if code, v, _ := do(t, h, "GET", "/api/cash/instruments/1/value?as_of=2025-01-03", ""); code != 200 || v["balance"] != "1010" {
		t.Fatalf("value: %d %v", code, v)
	}
	if code, _, list := do(t, h, "GET", "/api/cash/instruments/1/movements", ""); code != 200 || len(list) != 1 {
		t.Fatalf("movements: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/cash/movements/1", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/cash/movements/1", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
}
