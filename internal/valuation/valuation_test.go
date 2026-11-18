package valuation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

type env struct {
	svc     *Service
	alerts  *alerts.Service
	now     *time.Time
	held    int64 // holding (BRL)
	watched int64 // watchlist (USD)
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	h, err := as.Create(ctx, assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.Transition(ctx, h.ID, assets.StatusHolding); err != nil {
		t.Fatal(err)
	}
	w, err := as.Create(ctx, assets.CreateInput{Ticker: "MSFT", Market: "US", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	return &env{svc: NewService(conn, as, al, clock), alerts: al, now: &now, held: h.ID, watched: w.ID}
}

func dec(s string) *money.Decimal { v := money.New(decimal.RequireFromString(s)); return &v }

func (e *env) open(t *testing.T, typ string) []alerts.Alert {
	t.Helper()
	l, err := e.alerts.List(context.Background(), alerts.Filter{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func dcf() *engine.DCFInput {
	s := func(name, g, m, x, p string) engine.Scenario {
		gv := decimal.RequireFromString(g)
		return engine.Scenario{Name: name, RevenueGrowth: [engine.DCFYears]decimal.Decimal{gv, gv, gv, gv, gv},
			TerminalMargin: decimal.RequireFromString(m), ExitMultiple: decimal.RequireFromString(x),
			DiscountRate: decimal.RequireFromString("0.1"), Probability: decimal.RequireFromString(p)}
	}
	return &engine.DCFInput{BaseRevenue: decimal.RequireFromString("100"), Shares: decimal.RequireFromString("10"),
		Scenarios: []engine.Scenario{s("bear", "0", "0.1", "10", "0.25"), s("base", "0.1", "0.1", "10", "0.5"), s("bull", "0.2", "0.2", "15", "0.25")}}
}

func TestCreateAndLatest(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if _, err := e.svc.Latest(ctx, e.held); !errors.Is(err, ErrNotFound) {
		t.Fatalf("latest without versions: %v", err)
	}
	m, err := e.svc.Create(ctx, e.held, CreateInput{Method: "manual", ValuePerShare: dec("40"), AsOf: "2026-06-01", Sources: []string{" x ", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Currency != "BRL" || m.Stale || len(m.Sources) != 1 || m.Sources[0] != "x" {
		t.Fatalf("manual = %+v", m)
	}
	d, err := e.svc.Create(ctx, e.held, CreateInput{Method: "scenario_dcf", DCF: dcf()})
	if err != nil {
		t.Fatal(err)
	}
	if d.ValuePerShare.String() != "18.1402" || d.AsOf != "2026-11-18" {
		t.Fatalf("dcf = %+v", d)
	}
	l, err := e.svc.Latest(ctx, e.held)
	if err != nil || l.ID != d.ID {
		t.Fatalf("latest = %+v, %v", l, err)
	}
	list, err := e.svc.List(ctx, e.held)
	if err != nil || len(list) != 2 || list[0].ID != d.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
	usd, err := e.svc.Create(ctx, e.watched, CreateInput{Method: "target_price", ValuePerShare: dec("500")})
	if err != nil || usd.Currency != "USD" {
		t.Fatalf("usd = %+v, %v", usd, err)
	}
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	bad := dcf()
	bad.Scenarios[0].Probability = decimal.RequireFromString("0.3")
	tests := []struct {
		name string
		in   CreateInput
	}{
		{"unknown method", CreateInput{Method: "vibes", ValuePerShare: dec("10")}},
		{"manual without value", CreateInput{Method: "manual"}},
		{"non-positive value", CreateInput{Method: "manual", ValuePerShare: dec("0")}},
		{"dcf without inputs", CreateInput{Method: "scenario_dcf"}},
		{"dcf inputs on manual", CreateInput{Method: "manual", ValuePerShare: dec("10"), DCF: dcf()}},
		{"invalid dcf", CreateInput{Method: "scenario_dcf", DCF: bad}},
		{"bad date", CreateInput{Method: "manual", ValuePerShare: dec("10"), AsOf: "18/11/2026"}},
		{"future date", CreateInput{Method: "manual", ValuePerShare: dec("10"), AsOf: "2026-11-19"}},
		{"bad currency", CreateInput{Method: "manual", ValuePerShare: dec("10"), Currency: "EUR"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e.svc.Create(ctx, e.held, tc.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	if _, err := e.svc.Create(ctx, 999, CreateInput{Method: "manual", ValuePerShare: dec("10")}); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("unknown asset: %v", err)
	}
}

func TestIsStale(t *testing.T) {
	tests := []struct {
		asOf, today string
		want        bool
	}{
		{"2025-11-18", "2026-11-18", false},
		{"2025-11-17", "2026-11-18", true},
		{"2026-11-18", "2026-11-18", false},
	}
	for _, tc := range tests {
		if got := IsStale(tc.asOf, tc.today); got != tc.want {
			t.Errorf("IsStale(%s, %s) = %v", tc.asOf, tc.today, got)
		}
	}
}

func TestAlerts(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if m := e.open(t, AlertMissing); len(m) != 1 || *m[0].AssetID != e.held {
		t.Fatalf("missing = %+v", m)
	}
	// Watchlist assets never raise missing_fair_value, but do go stale.
	if _, err := e.svc.Create(ctx, e.watched, CreateInput{Method: "manual", ValuePerShare: dec("10"), AsOf: "2025-01-01"}); err != nil {
		t.Fatal(err)
	}
	old, err := e.svc.Create(ctx, e.held, CreateInput{Method: "manual", ValuePerShare: dec("10"), AsOf: "2025-11-01"})
	if err != nil {
		t.Fatal(err)
	}
	if m := e.open(t, AlertMissing); len(m) != 0 {
		t.Fatalf("missing should resolve: %+v", m)
	}
	if s := e.open(t, AlertStale); len(s) != 2 {
		t.Fatalf("stale = %+v", s)
	}
	// A fresh version clears the holding's stale alert.
	if _, err := e.svc.Create(ctx, e.held, CreateInput{Method: "manual", ValuePerShare: dec("12")}); err != nil {
		t.Fatal(err)
	}
	if s := e.open(t, AlertStale); len(s) != 1 || *s[0].AssetID != e.watched {
		t.Fatalf("stale after refresh = %+v", s)
	}
	// Deleting versions back to none re-raises missing.
	list, _ := e.svc.List(ctx, e.held)
	for _, fv := range list {
		if err := e.svc.Delete(ctx, fv.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.Delete(ctx, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
	if m := e.open(t, AlertMissing); len(m) != 1 {
		t.Fatalf("missing after delete = %+v", m)
	}
}
