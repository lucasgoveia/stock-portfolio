package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestJobsAPI(t *testing.T) {
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	steps := []jobs.Step{{Name: "noop", Run: func(context.Context, time.Time) (string, error) { return "done", nil }}}
	h := NewRouter(WithJobs(jobs.NewRunner(conn, nil, steps, nil)))
	if code, _, _ := do(t, h, "GET", "/api/jobs/daily/last", ""); code != 404 {
		t.Fatalf("last before run: %d", code)
	}
	code, r, _ := do(t, h, "POST", "/api/jobs/daily/run", "")
	if code != 200 || r["status"] != "ok" {
		t.Fatalf("run: %d %v", code, r)
	}
	code, r, _ = do(t, h, "GET", "/api/jobs/daily/last", "")
	if code != 200 || r["status"] != "ok" {
		t.Fatalf("last: %d %v", code, r)
	}
}
