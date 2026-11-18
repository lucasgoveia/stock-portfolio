package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestResearchAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	clock := func() time.Time { return time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC) }
	h := NewRouter(WithAssets(as), WithResearch(research.NewService(conn, as, clock)))
	do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock"}`)

	code, n, _ := do(t, h, "POST", "/api/assets/1/notes", `{"text":"Capex cycle","sources":["https://x.test"]}`)
	if code != 201 || n["author"] != "user" || n["opportunity_active"] != false {
		t.Fatalf("create: %d %v", code, n)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/notes", `{"text":"x","opportunity":true}`); code != 400 {
		t.Fatalf("opportunity needs expiry: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/9/notes", `{"text":"x"}`); code != 404 {
		t.Fatalf("missing asset: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/notes", `{"text":"x","bogus":1}`); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
	code, n, _ = do(t, h, "POST", "/api/assets/1/notes", `{"text":"Tender offer","opportunity":true,"opportunity_expires_on":"2025-03-20"}`)
	if code != 201 || n["opportunity_active"] != true {
		t.Fatalf("create opp: %d %v", code, n)
	}
	do(t, h, "POST", "/api/assets/1/notes", `{"text":"Old","opportunity":true,"opportunity_expires_on":"2025-03-01"}`)

	if code, _, list := do(t, h, "GET", "/api/assets/1/notes", ""); code != 200 || len(list) != 3 {
		t.Fatalf("list: %d %v", code, list)
	}
	code, _, list := do(t, h, "GET", "/api/opportunities", "")
	if code != 200 || len(list) != 1 {
		t.Fatalf("opportunities: %d %v", code, list)
	}
	if code, _, list := do(t, h, "GET", "/api/opportunities?as_of=2025-02-01&asset_id=1", ""); code != 200 || len(list) != 2 {
		t.Fatalf("opportunities as_of: %d %v", code, list)
	}
	if code, _, _ := do(t, h, "GET", "/api/opportunities?asset_id=x", ""); code != 400 {
		t.Fatalf("bad asset_id: %d", code)
	}
	if code, got, _ := do(t, h, "PATCH", "/api/notes/1", `{"text":"Capex cycle v2","opportunity":false}`); code != 200 || got["text"] != "Capex cycle v2" || got["opportunity"] != false {
		t.Fatalf("patch: %d %v", code, got)
	}
	if code, got, _ := do(t, h, "GET", "/api/notes/1", ""); code != 200 || got["text"] != "Capex cycle v2" {
		t.Fatalf("get: %d %v", code, got)
	}
	if code, _, _ := do(t, h, "PATCH", "/api/notes/1", `{"opportunity":true}`); code != 400 {
		t.Fatalf("patch flag without expiry: %d", code)
	}
	if code, _, _ := do(t, h, "GET", "/api/notes/99", ""); code != 404 {
		t.Fatalf("get missing: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/notes/1", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := do(t, h, "DELETE", "/api/notes/1", ""); code != 404 {
		t.Fatalf("delete again: %d", code)
	}
}
