package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(t *testing.T, ui fstest.MapFS, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h := NewRouter(ui)
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func testUI() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func TestHealth(t *testing.T) {
	rec := get(t, testUI(), "/api/health")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("body %q", got)
	}
}

func TestSPAFallback(t *testing.T) {
	for _, p := range []string{"/", "/alerts", "/assets/42", "/missing/deep/path"} {
		rec := get(t, testUI(), p)
		if rec.Code != http.StatusOK || rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: status %d body %q", p, rec.Code, rec.Body.String())
		}
	}
}

func TestStaticAsset(t *testing.T) {
	rec := get(t, testUI(), "/assets/app.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestAPINotSwallowed(t *testing.T) {
	for _, p := range []string{"/api/nope", "/api/", "/api"} {
		rec := get(t, testUI(), p)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content-type %q", p, ct)
		}
		if strings.Contains(rec.Body.String(), "<html>") {
			t.Errorf("%s: served index.html", p)
		}
	}
}

func TestUINotBuilt(t *testing.T) {
	rec := get(t, fstest.MapFS{}, "/alerts")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
