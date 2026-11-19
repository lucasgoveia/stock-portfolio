package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

const dcfJSON = `{"base_revenue":"100","shares":"10","scenarios":[
 {"name":"bear","revenue_growth":["0","0","0","0","0"],"terminal_margin":"0.1","exit_multiple":"10","discount_rate":"0.1","probability":"0.25"},
 {"name":"base","revenue_growth":["0.1","0.1","0.1","0.1","0.1"],"terminal_margin":"0.1","exit_multiple":"10","discount_rate":"0.1","probability":"0.5"},
 {"name":"bull","revenue_growth":["0.2","0.2","0.2","0.2","0.2"],"terminal_margin":"0.2","exit_multiple":"15","discount_rate":"0.1","probability":"0.25"}]}`

func TestValuationAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	svc := valuation.NewService(conn, as, alerts.New(conn, clock), clock)
	h := NewRouter(WithAssets(as), WithValuation(svc))
	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)

	code, r, _ := do(t, h, "POST", "/api/valuation/dcf", dcfJSON)
	if code != 200 || r["value_per_share"] != "18.1402" {
		t.Fatalf("preview: %d %v", code, r)
	}
	// New fields: net_pe basis (no net debt), cash conversion and an initiative.
	scen := func(name, p string) string {
		return `{"name":"` + name + `","basis":"net_pe","start_margin":"0.1","tax_rate":"0.34","cash_conversion":"0.5","revenue_growth":["0","0","0","0","0"],"terminal_margin":"0.1","exit_multiple":"10","discount_rate":"0.1","probability":"` + p + `"}`
	}
	newJSON := `{"base_revenue":"100","net_debt":"50","shares":"10","scenarios":[` + scen("bear", "0.25") + `,` + scen("base", "0.5") + `,` + scen("bull", "0.25") + `],
	 "initiatives":[{"name":"Fintech","start_year":3,"revenue_year5":"50","margin":"0.2","success_probability":{"bear":"0.2","base":"0.5","bull":"1"}}]}`
	code, r, _ = do(t, h, "POST", "/api/valuation/dcf", newJSON)
	if code != 200 || r["initiatives_value_per_share"] == nil {
		t.Fatalf("preview with new fields: %d %v", code, r)
	}
	if sc, _ := r["scenarios"].([]any); len(sc) != 3 || sc[1].(map[string]any)["basis"] != "net_pe" {
		t.Fatalf("scenarios = %v", r["scenarios"])
	}
	if code, _, _ := do(t, h, "POST", "/api/valuation/dcf", `{"base_revenue":"100","shares":"10","scenarios":[]}`); code != 400 {
		t.Fatalf("invalid preview: %d", code)
	}
	code, r, _ = do(t, h, "POST", "/api/assets/1/fair-values", `{"method":"scenario_dcf","dcf":`+dcfJSON+`,"sources":["model.xlsx"]}`)
	if code != 201 || r["value_per_share"] != "18.1402" || r["currency"] != "BRL" {
		t.Fatalf("create dcf: %d %v", code, r)
	}
	code, r, _ = do(t, h, "POST", "/api/assets/1/fair-values", `{"method":"manual","value_per_share":"20","as_of":"2026-01-02"}`)
	if code != 201 || r["value_per_share"] != "20" {
		t.Fatalf("create manual: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/fair-values", `{"method":"manual"}`); code != 400 {
		t.Fatalf("manual without value: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/9/fair-values", `{"method":"manual","value_per_share":"1"}`); code != 404 {
		t.Fatalf("unknown asset: %d", code)
	}
	code, r, _ = do(t, h, "GET", "/api/assets/1/fair-values/latest", "")
	if code != 200 || r["method"] != "scenario_dcf" {
		t.Fatalf("latest: %d %v", code, r)
	}
	code, _, list := do(t, h, "GET", "/api/assets/1/fair-values", "")
	if code != 200 || len(list) != 2 {
		t.Fatalf("list: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/fair-values/1", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/fair-values/1", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
}
