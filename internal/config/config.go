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
	// BrapiToken is the brapi.dev token (free plan); empty works for test tickers only.
	BrapiToken string `toml:"brapi_token"`
	// TiingoToken enables US prices; the US step is skipped when empty.
	TiingoToken string `toml:"tiingo_token"`
	// DailyJobAt is the America/Sao_Paulo wall time (HH:MM) of the daily job.
	DailyJobAt string `toml:"daily_job_at"`
	// DisableScheduler turns the in-process daily scheduler off.
	DisableScheduler bool `toml:"disable_scheduler"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{ListenAddr: "127.0.0.1:8080", DBPath: "stockportfolio.db", DailyJobAt: "19:00"}
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
	if v := os.Getenv("STOCKPORTFOLIO_BRAPI_TOKEN"); v != "" {
		cfg.BrapiToken = v
	}
	if v := os.Getenv("STOCKPORTFOLIO_TIINGO_TOKEN"); v != "" {
		cfg.TiingoToken = v
	}
	return cfg, nil
}
