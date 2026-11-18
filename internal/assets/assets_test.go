package assets

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func TestCanTransition(t *testing.T) {
	all := []string{StatusWatchlist, StatusHolding, StatusExited}
	allowed := map[[2]string]bool{
		{StatusWatchlist, StatusHolding}: true,
		{StatusHolding, StatusExited}:    true,
		{StatusExited, StatusWatchlist}:  true,
	}
	for _, from := range all {
		for _, to := range all {
			if got, want := CanTransition(from, to), allowed[[2]string{from, to}]; got != want {
				t.Errorf("CanTransition(%s,%s)=%v want %v", from, to, got, want)
			}
		}
	}
}

func newSvc(t *testing.T) *Service {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewService(conn)
}

func TestTransitionLifecycle(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	a, err := svc.Create(ctx, CreateInput{Ticker: " wege3 ", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Ticker != "WEGE3" || a.Status != StatusWatchlist || a.Benchmark != BenchmarkIBOV || a.Currency != CurrencyBRL {
		t.Fatalf("unexpected defaults: %+v", a)
	}
	if _, err := svc.Transition(ctx, a.ID, StatusExited); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("watchlist->exited: %v", err)
	}
	for _, to := range []string{StatusHolding, StatusExited, StatusWatchlist} {
		if _, err := svc.Transition(ctx, a.ID, to); err != nil {
			t.Fatalf("to %s: %v", to, err)
		}
	}
	if _, err := svc.Transition(ctx, 999, StatusHolding); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := svc.Transition(ctx, a.ID, "bogus"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bogus: %v", err)
	}
}
