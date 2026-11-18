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
	// AnthropicAPIKey enables the AI analyst.
	AnthropicAPIKey string `toml:"anthropic_api_key"`
	// SECUserAgent identifies this app to SEC EDGAR (name and contact email).
	SECUserAgent string `toml:"sec_user_agent"`
	// DailyJobAt is the America/Sao_Paulo wall time (HH:MM) of the daily job.
	DailyJobAt string `toml:"daily_job_at"`
	// DisableScheduler turns the in-process daily scheduler off.
	DisableScheduler bool `toml:"disable_scheduler"`

	envKeys map[string]bool // keys whose value came from the environment
}

// KeyStatus says whether an external key is set and where it came from. It
// never carries the value.
type KeyStatus struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Configured bool   `json:"configured"`
	// Source is "env", "config" or "" when not configured.
	Source string `json:"source"`
}

// KeyStatuses reports the external keys (brapi, Tiingo, Anthropic, SEC).
func (c Config) KeyStatuses() []KeyStatus {
	k := func(name, label, val string) KeyStatus {
		st := KeyStatus{Name: name, Label: label, Configured: val != ""}
		switch {
		case !st.Configured:
		case c.envKeys[name]:
			st.Source = "env"
		default:
			st.Source = "config"
		}
		return st
	}
	return []KeyStatus{
		k("brapi", "brapi token", c.BrapiToken),
		k("tiingo", "Tiingo token", c.TiingoToken),
		k("anthropic", "Anthropic API key", c.AnthropicAPIKey),
		k("sec", "SEC User-Agent", c.SECUserAgent),
	}
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
	cfg.envKeys = map[string]bool{}
	for _, e := range []struct {
		env, name string
		dst       *string
	}{
		{"STOCKPORTFOLIO_BRAPI_TOKEN", "brapi", &cfg.BrapiToken},
		{"STOCKPORTFOLIO_TIINGO_TOKEN", "tiingo", &cfg.TiingoToken},
		{"STOCKPORTFOLIO_ANTHROPIC_API_KEY", "anthropic", &cfg.AnthropicAPIKey},
		{"STOCKPORTFOLIO_SEC_USER_AGENT", "sec", &cfg.SECUserAgent},
	} {
		if v := os.Getenv(e.env); v != "" {
			*e.dst = v
			cfg.envKeys[e.name] = true
		}
	}
	return cfg, nil
}
