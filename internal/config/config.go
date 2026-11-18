// Package config loads application configuration.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// Config holds runtime configuration.
type Config struct {
	ListenAddr string `toml:"listen_addr"`
	DBPath     string `toml:"db_path"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{ListenAddr: "127.0.0.1:8080", DBPath: "stockportfolio.db"}
}

// Load reads path (if it exists) over defaults, then applies env overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // path is operator-provided
		switch {
		case err == nil:
			if err := toml.Unmarshal(b, &cfg); err != nil {
				return cfg, fmt.Errorf("parse %s: %w", path, err)
			}
		case !errors.Is(err, fs.ErrNotExist):
			return cfg, fmt.Errorf("read %s: %w", path, err)
		}
	}
	if v := os.Getenv("STOCKPORTFOLIO_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("STOCKPORTFOLIO_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	return cfg, nil
}
