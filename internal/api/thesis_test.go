package api

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
)

// alertList fetches /api/alerts (a {"alerts": [...]} envelope) and returns the array.
func alertList(t *testing.T, h http.Handler, query string) []any {
	t.Helper()
	code, obj, _ := do(t, h, "GET", "/api/alerts"+query, "")
	if code != 200 {
		t.Fatalf("alerts %s: %d", query, code)
	}
	l, _ := obj["alerts"].([]any)
	return l
}

func TestThesisAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	h := NewRouter(WithAssets(as), WithAlerts(al), WithThesis(thesis.NewService(conn, as, al, clock)))
	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)

	if code, _, _ := do(t, h, "GET", "/api/assets/1/thesis", ""); code != 404 {
		t.Fatalf("no thesis: %d", code)
	}
	code, th, _ := do(t, h, "PUT", "/api/assets/1/thesis", `{"text":"Compounder","next_review_on":"2025-03-01"}`)
	if code != 201 || th["status"] != "active" {
		t.Fatalf("create: %d %v", code, th)
	}
	// Review date already passed: thesis_review_due raised on write.
	if list := alertList(t, h, "?type=thesis_review_due"); len(list) != 1 {
		t.Fatalf("review due alert: %v", list)
	}
	if code, _, _ := do(t, h, "PUT", "/api/assets/1/thesis", `{"text":""}`); code != 400 {
		t.Fatalf("empty text: %d", code)
	}
	if code, _, _ := do(t, h, "PUT", "/api/assets/9/thesis", `{"text":"x"}`); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}

	code, c, _ := do(t, h, "POST", "/api/assets/1/thesis/conditions", `{"text":"Margin < 12%"}`)
	if code != 201 || c["text"] != "Margin < 12%" {
		t.Fatalf("add condition: %d %v", code, c)
	}
	do(t, h, "POST", "/api/assets/1/thesis/conditions", `{"text":"CEO leaves"}`)
	if code, c, _ := do(t, h, "PATCH", "/api/break-conditions/1", `{"text":"Margin < 10%"}`); code != 200 || c["text"] != "Margin < 10%" {
		t.Fatalf("patch condition: %d %v", code, c)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/break-conditions/2", ""); code != 204 {
		t.Fatalf("delete condition: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/break-conditions/2", ""); code != 404 {
		t.Fatalf("delete condition again: %d", code)
	}
	if code, g, _ := do(t, h, "GET", "/api/assets/1/thesis", ""); code != 200 || len(g["break_conditions"].([]any)) != 1 {
		t.Fatalf("get: %d %v", code, g)
	}

	if code, _, _ := do(t, h, "POST", "/api/assets/1/thesis/status", `{"status":"meh"}`); code != 400 {
		t.Fatalf("bad status: %d", code)
	}
	if code, g, _ := do(t, h, "POST", "/api/assets/1/thesis/status", `{"status":"broken"}`); code != 200 || g["status"] != "broken" {
		t.Fatalf("break: %d %v", code, g)
	}
	list := alertList(t, h, "?type=thesis_broken")
	if len(list) != 1 || list[0].(map[string]any)["asset_id"] != float64(1) {
		t.Fatalf("broken alert: %v", list)
	}
	// Broken suppresses (and resolves) the review-due alert.
	if list := alertList(t, h, "?type=thesis_review_due"); len(list) != 0 {
		t.Fatalf("review due should be resolved: %v", list)
	}

	code, rv, _ := do(t, h, "POST", "/api/assets/1/thesis/review", `{"note":"Margins recovered","status":"active","next_review_on":"2025-09-01"}`)
	if code != 201 || rv["status_after"] != "active" {
		t.Fatalf("review: %d %v", code, rv)
	}
	if list := alertList(t, h, "?type=thesis_broken"); len(list) != 0 {
		t.Fatalf("broken alert should resolve: %v", list)
	}
	if code, _, list := do(t, h, "GET", "/api/assets/1/thesis/reviews", ""); code != 200 || len(list) != 1 {
		t.Fatalf("reviews: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/thesis/review", `{"note":""}`); code != 400 {
		t.Fatalf("review without note: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/assets/1/thesis", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/assets/1/thesis", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
}
