package api

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

type fakeCVM struct{ err error }

func (f fakeCVM) AnnualFinancials(context.Context, string, []int) (marketdata.FinancialFacts, error) {
	if f.err != nil {
		return marketdata.FinancialFacts{}, f.err
	}
	v := func(s string) money.Decimal { return money.New(decimal.RequireFromString(s)) }
	return marketdata.FinancialFacts{Source: "cvm", Currency: "BRL", Periods: []marketdata.FinancialPeriod{{
		End: marketdata.Date(2025, 12, 31), Type: marketdata.PeriodAnnual, Values: map[string]money.Decimal{
			marketdata.ConceptRevenue: v("1000"), marketdata.ConceptEBIT: v("200"), marketdata.ConceptNetIncome: v("100"),
			marketdata.ConceptEquity: v("800"), marketdata.ConceptCash: v("100"), marketdata.ConceptDebt: v("300"),
			marketdata.ConceptShares: v("100"), marketdata.ConceptDPS: v("1.2"),
		},
	}}}, nil
}

func TestFundamentalsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	svc := fundamentals.NewService(conn, as, fundamentals.Sources{BR: fakeCVM{}}, clock)
	h := NewRouter(WithAssets(as), WithFundamentals(svc))

	do(t, h, "POST", "/api/assets", `{"ticker":"PETR4","market":"B3","kind":"stock"}`)
	code, r, _ := do(t, h, "GET", "/api/assets/1/fundamentals", "")
	if code != 200 || r["status"] != "needs_identifier" || r["identifier_kind"] != "cnpj" {
		t.Fatalf("without CNPJ: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/fundamentals/refresh", ""); code != 400 {
		t.Fatalf("refresh without CNPJ: %d", code)
	}
	if code, _, _ := do(t, h, "PATCH", "/api/assets/1", `{"cnpj":"123"}`); code != 400 {
		t.Fatalf("invalid CNPJ: %d", code)
	}
	code, a, _ := do(t, h, "PATCH", "/api/assets/1", `{"cnpj":"33000167000101"}`)
	if code != 200 || a["cnpj"] != "33.000.167/0001-01" {
		t.Fatalf("set CNPJ: %d %v", code, a)
	}
	if err := db.New(conn).UpsertAssetPrice(context.Background(), db.UpsertAssetPriceParams{
		AssetID: 1, RefDate: "2026-11-17", Source: "cotahist", Close: "20", FetchedAt: "2026-11-17T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	code, r, _ = do(t, h, "POST", "/api/assets/1/fundamentals/refresh", "")
	if code != 200 || r["status"] != "ok" || r["source"] != "cvm" || r["as_of"] != "2025-12-31" {
		t.Fatalf("refresh: %d %v", code, r)
	}
	m, _ := r["metrics"].(map[string]any)
	if m["pe"] != "20" || m["dividend_yield"] != "0.06" || m["ev_ebit"] != "11" {
		t.Fatalf("metrics: %v", m)
	}
	hp, _ := r["helpers"].(map[string]any)
	if bz, _ := hp["bazin"].(map[string]any); bz["value"] != "20" {
		t.Fatalf("bazin: %v", hp["bazin"])
	}
	// Editable inputs.
	code, r, _ = do(t, h, "GET", "/api/assets/1/fundamentals?bazin_yield=0.08&exit_multiple=12", "")
	hp, _ = r["helpers"].(map[string]any)
	if bz, _ := hp["bazin"].(map[string]any); code != 200 || bz["value"] != "15" {
		t.Fatalf("bazin 8%%: %d %v", code, hp["bazin"])
	}
	if rd, _ := hp["reverse_dcf"].(map[string]any); rd["exit_multiple"] != "12" {
		t.Fatalf("reverse dcf: %v", rd)
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/1/fundamentals?bazin_yield=abc", ""); code != 400 {
		t.Fatalf("bad param: %d", code)
	}
	code, r, _ = do(t, h, "GET", "/api/assets/1/fundamentals/fair-value-draft?method=graham", "")
	if code != 200 || r["method"] != "graham" || r["value_per_share"] != "13.4164" || r["currency"] != "BRL" {
		t.Fatalf("draft: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/1/fundamentals/fair-value-draft?method=manual", ""); code != 400 {
		t.Fatalf("draft method: %d", code)
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/9/fundamentals", ""); code != 404 {
		t.Fatalf("unknown asset: %d", code)
	}

	// An upstream failure is a 502 with the cause.
	down := fundamentals.NewService(conn, as, fundamentals.Sources{BR: fakeCVM{err: errors.New("cvm down")}}, clock)
	h2 := NewRouter(WithAssets(as), WithFundamentals(down))
	code, r, _ = do(t, h2, "POST", "/api/assets/1/fundamentals/refresh", "")
	if code != 502 || r["error"] == "" {
		t.Fatalf("upstream failure: %d %v", code, r)
	}
}
