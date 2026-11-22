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

func TestBDRUnderlyingLink(t *testing.T) {
	svc := newSvc(t)
	ctx := context.Background()
	us, err := svc.Create(ctx, CreateInput{Ticker: "MSFT", Market: "US", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	b3, err := svc.Create(ctx, CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	// Create with a link.
	bdr, err := svc.Create(ctx, CreateInput{Ticker: "MSFT34", Market: "B3", Kind: "bdr", UnderlyingAssetID: &us.ID, BDRRatio: "20"})
	if err != nil || !bdr.UnderlyingAssetID.Valid || bdr.UnderlyingAssetID.Int64 != us.ID || bdr.BdrRatio != "20" {
		t.Fatalf("create: %+v %v", bdr, err)
	}
	bad := []CreateInput{
		{Ticker: "X1", Market: "B3", Kind: "stock", UnderlyingAssetID: &us.ID, BDRRatio: "1"}, // not a bdr
		{Ticker: "X2", Market: "B3", Kind: "bdr", UnderlyingAssetID: &b3.ID, BDRRatio: "1"},   // underlying not US
		{Ticker: "X3", Market: "B3", Kind: "bdr", UnderlyingAssetID: &us.ID, BDRRatio: "0"},   // bad ratio
		{Ticker: "X4", Market: "B3", Kind: "bdr", UnderlyingAssetID: &us.ID},                  // ratio missing
		{Ticker: "X5", Market: "B3", Kind: "bdr", BDRRatio: "2"},                              // ratio without underlying
	}
	for _, in := range bad {
		if _, err := svc.Create(ctx, in); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want validation error", in.Ticker, err)
		}
	}
	// Update: change the ratio, then clear the link.
	r := "10"
	u, err := svc.Update(ctx, bdr.ID, UpdateInput{BDRRatio: &r})
	if err != nil || u.BdrRatio != "10" || !u.UnderlyingAssetID.Valid {
		t.Fatalf("update ratio: %+v %v", u, err)
	}
	zero := int64(0)
	u, err = svc.Update(ctx, bdr.ID, UpdateInput{UnderlyingAssetID: &zero})
	if err != nil || u.UnderlyingAssetID.Valid || u.BdrRatio != "" {
		t.Fatalf("clear: %+v %v", u, err)
	}
	if _, err := svc.Update(ctx, b3.ID, UpdateInput{UnderlyingAssetID: &us.ID, BDRRatio: &r}); !errors.Is(err, ErrValidation) {
		t.Errorf("stock link: %v", err)
	}
}
