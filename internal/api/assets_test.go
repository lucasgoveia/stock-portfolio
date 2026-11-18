package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func newServer(t *testing.T) http.Handler {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewRouter(assets.NewService(conn))
}

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any, []map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var obj map[string]any
	var arr []map[string]any
	if strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "[") {
		_ = json.Unmarshal(rec.Body.Bytes(), &arr)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &obj)
	}
	return rec.Code, obj, arr
}

func TestAssetsAPI(t *testing.T) {
	h := newServer(t)

	code, a, _ := do(t, h, "POST", "/api/assets", `{"ticker":"googl","market":"US","kind":"stock","sleeve":"core"}`)
	if code != 201 || a["ticker"] != "GOOGL" || a["status"] != "watchlist" || a["benchmark"] != "SP500" || a["currency"] != "USD" {
		t.Fatalf("create: %d %v", code, a)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets", `{"ticker":"GOOGL","market":"US","kind":"etf"}`); code != 409 {
		t.Fatalf("duplicate: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets", `{"ticker":"X","market":"MARS","kind":"stock"}`); code != 400 {
		t.Fatalf("bad market: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets", `{not json`); code != 400 {
		t.Fatalf("bad json: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets", `{"ticker":"WEGE3","market":"B3","kind":"stock","sleeve":"tactical"}`); code != 201 {
		t.Fatalf("create b3: %d", code)
	}

	for q, want := range map[string]int{"": 2, "?market=B3": 1, "?sleeve=core": 1, "?status=holding": 0} {
		code, _, list := do(t, h, "GET", "/api/assets"+q, "")
		if code != 200 || len(list) != want {
			t.Fatalf("list %q: %d len=%d want %d", q, code, len(list), want)
		}
	}
	if code, _, _ := do(t, h, "GET", "/api/assets?status=nope", ""); code != 400 {
		t.Fatalf("bad filter: %d", code)
	}

	if code, got, _ := do(t, h, "GET", "/api/assets/1", ""); code != 200 || got["ticker"] != "GOOGL" {
		t.Fatalf("get: %d %v", code, got)
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/99", ""); code != 404 {
		t.Fatalf("get missing: %d", code)
	}
	if code, _, _ := do(t, h, "GET", "/api/assets/abc", ""); code != 400 {
		t.Fatalf("get bad id: %d", code)
	}

	code, got, _ := do(t, h, "PATCH", "/api/assets/1", `{"name":"Alphabet","sleeve":"tactical","benchmark":"IBOV"}`)
	if code != 200 || got["name"] != "Alphabet" || got["sleeve"] != "tactical" || got["benchmark"] != "IBOV" {
		t.Fatalf("patch: %d %v", code, got)
	}
	if code, _, _ := do(t, h, "PATCH", "/api/assets/1", `{"sleeve":"x"}`); code != 400 {
		t.Fatalf("patch invalid: %d", code)
	}
	if code, _, _ := do(t, h, "PATCH", "/api/assets/1", `{"status":"holding"}`); code != 400 {
		t.Fatalf("patch status must be rejected: %d", code)
	}
	if code, _, _ := do(t, h, "PATCH", "/api/assets/99", `{"name":"x"}`); code != 404 {
		t.Fatalf("patch missing: %d", code)
	}

	if code, _, _ := do(t, h, "POST", "/api/assets/1/status", `{"status":"exited"}`); code != 409 {
		t.Fatalf("invalid transition: %d", code)
	}
	if code, got, _ := do(t, h, "POST", "/api/assets/1/status", `{"status":"holding"}`); code != 200 || got["status"] != "holding" {
		t.Fatalf("transition: %d %v", code, got)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/1/status", `{"status":"bogus"}`); code != 400 {
		t.Fatalf("bogus status: %d", code)
	}
	if code, _, _ := do(t, h, "POST", "/api/assets/99/status", `{"status":"holding"}`); code != 404 {
		t.Fatalf("status missing: %d", code)
	}
}
