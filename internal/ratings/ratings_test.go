package ratings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func m(s string) money.Decimal   { return money.New(d(s)) }

func setup(t *testing.T) (*Service, int64) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	a, err := as.Create(context.Background(), assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC) }
	return NewService(conn, as, clock), a.ID
}

func in(on string, f [5]int, reason string) AddInput {
	return AddInput{RatedOn: on, Volatility: f[0], Understanding: f[1], Moat: f[2], Predictability: f[3], Horizon: f[4], Reason: reason}
}

func TestAddHistoryLatestAndHook(t *testing.T) {
	ctx := context.Background()
	svc, id := setup(t)
	var hooked []int64
	svc.OnChange(func(_ context.Context, assetID int64) error { hooked = append(hooked, assetID); return nil })
	svc.OnChange(func(context.Context, int64) error { return errors.New("boom") }) // must not fail Add

	if _, err := svc.Latest(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no rating yet: %v", err)
	}
	r1, err := svc.Add(ctx, id, in("2025-01-10", [5]int{3, 4, 5, 2, 3}, "initial"))
	if err != nil || !r1.Score.Equal(d("3.4167")) {
		t.Fatalf("r1: %+v %v", r1, err)
	}
	if _, err := svc.Add(ctx, id, in("2025-02-10", [5]int{5, 5, 5, 5, 5}, "override")); err != nil {
		t.Fatal(err)
	}
	// Back-dated entry appended later still sorts by date.
	if _, err := svc.Add(ctx, id, in("2025-01-20", [5]int{1, 1, 1, 1, 1}, "backfill")); err != nil {
		t.Fatal(err)
	}
	if len(hooked) != 3 || hooked[0] != id {
		t.Fatalf("hook calls: %v", hooked)
	}
	h, err := svc.History(ctx, id)
	if err != nil || len(h) != 3 {
		t.Fatalf("history: %v %v", h, err)
	}
	want := []string{"2025-02-10", "2025-01-20", "2025-01-10"}
	for i, r := range h {
		if r.RatedOn != want[i] {
			t.Fatalf("history order: %v", h)
		}
	}
	latest, _ := svc.Latest(ctx, id)
	if latest.Reason != "override" || !latest.Score.Equal(d("5")) {
		t.Fatalf("latest: %+v", latest)
	}
	// Default date comes from the clock.
	r, _ := svc.Add(ctx, id, in("", [5]int{2, 2, 2, 2, 2}, "today"))
	if r.RatedOn != "2025-03-01" {
		t.Fatalf("default date: %s", r.RatedOn)
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	svc, id := setup(t)
	for name, c := range map[string]AddInput{
		"zero factor": in("2025-01-01", [5]int{0, 1, 1, 1, 1}, "x"),
		"six factor":  in("2025-01-01", [5]int{1, 1, 1, 1, 6}, "x"),
		"no reason":   in("2025-01-01", [5]int{1, 1, 1, 1, 1}, "  "),
		"bad date":    in("01/01/2025", [5]int{1, 1, 1, 1, 1}, "x"),
	} {
		if _, err := svc.Add(ctx, id, c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := svc.Add(ctx, 99, in("2025-01-01", [5]int{1, 1, 1, 1, 1}, "x")); !errors.Is(err, assets.ErrNotFound) {
		t.Errorf("missing asset: %v", err)
	}
}

func TestCustomWeights(t *testing.T) {
	ctx := context.Background()
	svc, id := setup(t)
	w, err := svc.Weights(ctx)
	if err != nil || !w.Moat.Equal(d("1.5")) || !w.Volatility.Equal(d("1")) {
		t.Fatalf("defaults: %+v %v", w, err)
	}
	r, _ := svc.Add(ctx, id, in("2025-01-01", [5]int{5, 1, 3, 4, 2}, "x"))
	if !r.Score.Equal(d("3.0833")) { // (5+1+4.5+6+2)/6 = 18.5/6
		t.Fatalf("default weights score %s", r.Score)
	}
	if _, err := svc.SetWeights(ctx, WeightsInput{Volatility: m("2"), Understanding: m("0"), Moat: m("1"), Predictability: m("1"), Horizon: m("1")}); err != nil {
		t.Fatal(err)
	}
	latest, _ := svc.Latest(ctx, id)
	if !latest.Score.Equal(d("3.8")) { // (10+0+3+4+2)/5
		t.Fatalf("custom weights score %s", latest.Score)
	}
	if _, err := svc.SetWeights(ctx, WeightsInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero weights: %v", err)
	}
	if _, err := svc.SetWeights(ctx, WeightsInput{Volatility: m("-1"), Understanding: m("1"), Moat: m("1"), Predictability: m("1"), Horizon: m("1")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative weight: %v", err)
	}
}
