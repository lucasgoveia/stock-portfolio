package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("listen_addr = \":9\"\ndb_path = \"a.db\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil || cfg.ListenAddr != ":9" || cfg.DBPath != "a.db" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	t.Setenv("STOCKPORTFOLIO_DB_PATH", "env.db")
	cfg, _ = Load(p)
	if cfg.DBPath != "env.db" {
		t.Fatalf("env override failed: %+v", cfg)
	}
	cfg, err = Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || cfg.ListenAddr != Default().ListenAddr {
		t.Fatalf("missing file: %+v, %v", cfg, err)
	}
}
