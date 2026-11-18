package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/tracking"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func TestExpectedVsActualAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	st := settings.NewService(conn, nil)
	svc := tracking.NewService(conn, as, valuation.NewService(conn, as, al, clock), al, nil, st)
	h := NewRouter(WithAssets(as), WithExpectedVsActual(svc), WithSettings(st))

	do(t, h, "POST", "/api/assets", `{"ticker":"PETR4","market":"B3","kind":"stock"}`)
	code, r, _ := do(t, h, "GET", "/api/assets/1/expected-vs-actual", "")
	if code != 200 || r["status"] != "no_quarters" || r["consecutive_quarters"] != float64(2) || r["miss_tolerance"] != "0.02" {
		t.Fatalf("empty report: %d %v", code, r)
	}
	if qs, ok := r["quarters"].([]any); !ok || len(qs) != 0 {
		t.Errorf("quarters should be an empty array: %v", r["quarters"])
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/99/expected-vs-actual", ""); code != 404 {
		t.Errorf("unknown asset: %d", code)
	}

	// The rules are editable settings.
	code, r, _ = do(t, h, "PUT", "/api/settings/expectations", `{"miss_tolerance":"0.05","consecutive_quarters":3}`)
	if code != 200 || r["miss_tolerance"] != "0.05" {
		t.Fatalf("set expectations: %d %v", code, r)
	}
	code, r, _ = do(t, h, "GET", "/api/assets/1/expected-vs-actual", "")
	if code != 200 || r["consecutive_quarters"] != float64(3) || r["miss_tolerance"] != "0.05" {
		t.Fatalf("report after settings change: %d %v", code, r)
	}
	if code, _, _ := do(t, h, "PUT", "/api/settings/expectations", `{"miss_tolerance":"1.5","consecutive_quarters":0}`); code != 400 && code != 422 {
		t.Errorf("invalid expectations accepted: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/settings/expectations/reset", ""); code != 204 {
		t.Errorf("reset: %d", code)
	}
}
