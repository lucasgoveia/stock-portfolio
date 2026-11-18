package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func setup(t *testing.T) (http.Handler, *alerts.Service, *time.Time) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	svc := alerts.New(conn, func() time.Time { return now })
	return NewRouter(WithAlerts(svc)), svc, &now
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

type listResp struct {
	Alerts []alerts.Alert `json:"alerts"`
}

func list(t *testing.T, h http.Handler, query string) []alerts.Alert {
	t.Helper()
	rec := do(h, http.MethodGet, "/api/alerts"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body)
	}
	var r listResp
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r.Alerts
}

func TestAlertsList(t *testing.T) {
	h, svc, _ := setup(t)
	ctx := t.Context()
	one, two := int64(1), int64(2)
	a, _ := svc.Upsert(ctx, "overweight", &one, "", map[string]int{"x": 1})
	_, _ = svc.Upsert(ctx, "overweight", &two, "", nil)
	_, _ = svc.Upsert(ctx, "overvalued", &one, "", nil)
	_, _ = svc.Acknowledge(ctx, a.ID)

	tests := []struct {
		query string
		want  int
		code  int
	}{
		{"", 2, 200},
		{"?state=acknowledged", 1, 200},
		{"?state=all", 3, 200},
		{"?type=overvalued", 1, 200},
		{"?state=all&asset_id=1", 2, 200},
		{"?type=nope", 0, 400},
		{"?state=nope", 0, 400},
		{"?asset_id=abc", 0, 400},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			if tc.code != 200 {
				if rec := do(h, http.MethodGet, "/api/alerts"+tc.query, ""); rec.Code != tc.code {
					t.Fatalf("code %d", rec.Code)
				}
				return
			}
			if got := len(list(t, h, tc.query)); got != tc.want {
				t.Fatalf("len = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAlertsActions(t *testing.T) {
	h, svc, now := setup(t)
	a, _ := svc.Upsert(t.Context(), "overweight", nil, "", nil)
	id := func(suffix string) string { return "/api/alerts/" + itoa(a.ID) + suffix }

	if rec := do(h, http.MethodPost, id("/ack"), ""); rec.Code != 200 {
		t.Fatalf("ack %d %s", rec.Code, rec.Body)
	}
	if got := list(t, h, "?state=acknowledged"); len(got) != 1 {
		t.Fatalf("acked = %d", len(got))
	}

	until := now.Add(24 * time.Hour).Format(time.RFC3339)
	if rec := do(h, http.MethodPost, id("/snooze"), `{"until":"`+until+`"}`); rec.Code != 200 {
		t.Fatalf("snooze %d %s", rec.Code, rec.Body)
	}
	if got := list(t, h, ""); len(got) != 0 {
		t.Fatalf("snoozed alert visible in default list")
	}
	*now = now.Add(25 * time.Hour)
	if got := list(t, h, ""); len(got) != 1 || got[0].State != "open" {
		t.Fatalf("expired snooze should reappear as open: %+v", got)
	}

	if rec := do(h, http.MethodPost, id("/snooze"), `{"until":"2000-01-01T00:00:00Z"}`); rec.Code != 400 {
		t.Fatalf("past snooze %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, id("/snooze"), `{}`); rec.Code != 400 {
		t.Fatalf("empty snooze %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, id("/resolve"), ""); rec.Code != 200 {
		t.Fatalf("resolve %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, id("/ack"), ""); rec.Code != http.StatusConflict {
		t.Fatalf("ack resolved %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/alerts/999/ack", ""); rec.Code != 404 {
		t.Fatalf("missing %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/api/alerts/x/ack", ""); rec.Code != 400 {
		t.Fatalf("bad id %d", rec.Code)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
