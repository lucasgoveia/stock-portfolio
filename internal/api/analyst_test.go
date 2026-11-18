package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestAnalystAPIWithoutKey(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	sets := settings.NewService(conn, nil)
	svc := analyst.NewService(analyst.Deps{Conn: conn, Assets: as, Settings: sets})
	sets.UseUsage(svc.MonthSpend)
	h := NewRouter(WithAssets(as), WithAnalyst(svc), WithSettings(sets))
	do(t, h, "POST", "/api/assets", `{"ticker":"AAPL","market":"US","kind":"stock"}`)

	code, body, _ := do(t, h, "POST", "/api/assets/1/analyst/runs", "")
	if code != 409 || !strings.Contains(body["error"].(string), "API key") {
		t.Fatalf("start without key: %d %v", code, body)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/9/analyst/runs", ""); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
	code, u, _ := do(t, h, "GET", "/api/analyst/usage", "")
	if code != 200 || u["can_run"] != false || u["key_configured"] != false || u["spent_usd"] != "0" || u["cap_usd"] != "20" {
		t.Fatalf("usage: %d %v", code, u)
	}
	if code, _, list := do(t, h, "GET", "/api/assets/1/analyst/runs", ""); code != 200 || len(list) != 0 {
		t.Fatalf("list: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "GET", "/api/analyst/runs/5", ""); code != 404 {
		t.Fatalf("missing run: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/analyst/runs/5/approve", `{"market_size":true}`); code != 404 {
		t.Fatalf("approve missing: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/analyst/runs/5/discard", ""); code != 404 {
		t.Fatalf("discard missing: %d", code)
	}
	if code, st, _ := do(t, h, "GET", "/api/settings/status", ""); code != 200 || st["ai_usage_usd"] != "0" {
		t.Fatalf("status: %d %v", code, st)
	}
}
