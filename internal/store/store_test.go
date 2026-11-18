package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_revisions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	files, err := dir.Files()
	if err != nil {
		t.Fatal(err)
	}
	if n != len(files) || n == 0 {
		t.Fatalf("revisions = %d, files = %d", n, len(files))
	}
	// Re-running is a no-op.
	if err := Migrate(context.Background(), conn); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	_ = conn.Close()
	conn, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var fk int
	if err := conn.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v", fk, err)
	}
	// Spot-check a table from each migration.
	for _, table := range []string{"assets", "alerts", "fair_values", "asset_prices", "job_runs"} {
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Errorf("%s: %v", table, err)
		}
	}
}
