package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/tax"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestTaxesAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	ts := trades.NewService(conn, as)
	h := NewRouter(WithAssets(as), WithTrades(ts), WithTaxes(tax.NewService(as, ts, nil, nil)))

	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-03-02","side":"buy","quantity":"1000","price":"20"}`)
	// R$25k of sales in March at a R$5k gain: over the exemption, R$750 DARF.
	do(t, h, "POST", "/api/assets/1/trades", `{"date":"2026-03-20","side":"sell","quantity":"1000","price":"25"}`)
	code, r, _ := do(t, h, "GET", "/api/taxes/b3", "")
	months, _ := r["months"].([]any)
	if code != 200 || len(months) != 1 {
		t.Fatalf("taxes: %d %v", code, r)
	}
	m := months[0].(map[string]any)
	code, a, _ := do(t, h, "GET", "/api/taxes/annual/2026", "")
	if code != 200 || a["year"] != float64(2026) {
		t.Fatalf("annual: %d %v", code, a)
	}
	rec := doAlert(h, "GET", "/api/taxes/annual/2026/csv", "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(rec.Body.String(), "section,ticker") || !strings.Contains(rec.Header().Get("Content-Disposition"), "dirpf-2026.csv") {
		t.Fatalf("csv: %d %q %s", rec.Code, rec.Header(), rec.Body)
	}
	if code, _, _ := do(t, h, "GET", "/api/taxes/annual/1999", ""); code != 400 {
		t.Fatalf("bad year: %d", code)
	}
	if m["month"] != "2026-03" || m["exempt"] != false || m["darf"] != "750" || m["darf_code"] != "6015" || m["due_date"] != "2026-04-30" {
		t.Fatalf("march = %v", m)
	}
}
