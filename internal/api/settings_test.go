package api

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/config"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestSettingsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	keys := []config.KeyStatus{{Name: "brapi", Label: "brapi token", Configured: true, Source: "env"},
		{Name: "tiingo", Label: "Tiingo token"}}
	h := NewRouter(WithSettings(settings.NewService(conn, keys)))

	code, r, _ := do(t, h, "GET", "/api/settings", "")
	planner := r["planner"].(map[string]any)
	if code != 200 || planner["values"].(map[string]any)["min_order_b3"] != "300" || planner["defaults"].(map[string]any)["us_batch_min"] != "2000" {
		t.Fatalf("get: %d %v", code, r)
	}

	body := `{"bootstrap_below":"10000","min_order_b3":"450","us_batch_min":"2000"}`
	if code, r, _ = do(t, h, "PUT", "/api/settings/planner", body); code != 200 || r["min_order_b3"] != "450" {
		t.Fatalf("put: %d %v", code, r)
	}
	_, r, _ = do(t, h, "GET", "/api/settings", "")
	if got := r["planner"].(map[string]any)["values"].(map[string]any)["min_order_b3"]; got != "450" {
		t.Fatalf("not persisted: %v", got)
	}

	code, r, _ = do(t, h, "PUT", "/api/settings/planner", `{"bootstrap_below":"10000","min_order_b3":"-5","us_batch_min":"2000"}`)
	if msg, _ := r["error"].(string); code != 400 || !strings.Contains(msg, "min_order_b3:") {
		t.Fatalf("validation: %d %v", code, r)
	}

	if code, _, _ = do(t, h, "POST", "/api/settings/planner/reset", ""); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	_, r, _ = do(t, h, "GET", "/api/settings", "")
	if got := r["planner"].(map[string]any)["values"].(map[string]any)["min_order_b3"]; got != "300" {
		t.Fatalf("after reset: %v", got)
	}
	if code, _, _ = do(t, h, "POST", "/api/settings/bogus/reset", ""); code != 400 && code != 404 {
		t.Fatalf("unknown group: %d", code)
	}

	code, r, _ = do(t, h, "GET", "/api/settings/status", "")
	ks := r["keys"].([]any)
	if code != 200 || len(ks) != 2 || r["ai_usage_usd"] != "0" || ks[0].(map[string]any)["source"] != "env" {
		t.Fatalf("status: %d %v", code, r)
	}
}
