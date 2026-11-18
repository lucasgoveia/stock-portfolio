package settings

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/config"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewService(conn, []config.KeyStatus{{Name: "brapi", Label: "brapi token", Configured: true, Source: "env"}})
}

func TestDefaultsMatchEngine(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	p, err := s.Params(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprintf("%+v", p), fmt.Sprintf("%+v", engine.DefaultParams()); got != want {
		t.Fatalf("params = %s, want %s", got, want)
	}
	tp, _ := s.TaxParams(ctx)
	if got, want := fmt.Sprintf("%+v", tp), fmt.Sprintf("%+v", engine.DefaultTaxParams()); got != want {
		t.Fatalf("tax = %s, want %s", got, want)
	}
	dp, _ := s.DividendParams(ctx)
	if got, want := fmt.Sprintf("%+v", dp), fmt.Sprintf("%+v", engine.DefaultDividendParams()); got != want {
		t.Fatalf("div = %s, want %s", got, want)
	}
}

func TestRoundTripAndReset(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	pl := DefaultPlanner()
	pl.MinOrderB3 = d("500")
	if _, err := s.SetPlanner(ctx, pl); err != nil {
		t.Fatal(err)
	}
	got, err := s.Planner(ctx)
	if err != nil || got.MinOrderB3.String() != "500" || got.USBatchMin.String() != "2000" {
		t.Fatalf("planner = %+v, %v", got, err)
	}
	p, _ := s.Params(ctx)
	if p.MinOrderB3.String() != "500" {
		t.Fatalf("params min order = %s", p.MinOrderB3)
	}
	tx := DefaultTax()
	tx.JCPRate, tx.DayTradeDARFCodeConfirmed = d("0.2"), true
	if _, err := s.SetTax(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if dp, _ := s.DividendParams(ctx); dp.JCPRate.String() != "0.2" {
		t.Fatalf("jcp = %s", dp.JCPRate)
	}
	if tp, _ := s.TaxParams(ctx); !tp.DayTradeDARFCodeConfirmed {
		t.Fatal("confirmed flag lost")
	}
	if err := s.Reset(ctx, GroupPlanner); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Planner(ctx); got.MinOrderB3.String() != "300" {
		t.Fatalf("after reset = %s", got.MinOrderB3)
	}
	if err := s.Reset(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestMergeOverDefaults(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	// A stored value with a missing key and an unknown key.
	if err := s.store(ctx, GroupTactical, map[string]any{"max_open": 5, "bogus": 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Tactical(ctx)
	if err != nil || got.MaxOpen != 5 || got.MaxLoss.String() != "0.01" {
		t.Fatalf("tactical = %+v, %v", got, err)
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	a := DefaultAllocation()
	a.ClampMax, a.HardCap = d("1.5"), d("0.05")
	a.CashBandMin = d("0.5")
	_, err := s.SetAllocation(ctx, a)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"clamp_max:", "hard_cap:", "cash_band_min:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing from %v", want, err)
		}
	}
	// Nothing stored after a rejected save.
	if got, _ := s.Allocation(ctx); got.ClampMax.String() != "0.1" {
		t.Fatalf("stored despite error: %s", got.ClampMax)
	}
	pl := DefaultPlanner()
	pl.MinOrderB3 = d("-1")
	if _, err := s.SetPlanner(ctx, pl); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative amount: %v", err)
	}
	tx := DefaultTax()
	tx.DARFCode, tx.SwingRate = "60", d("1.2")
	if _, err := s.SetTax(ctx, tx); !errors.Is(err, ErrValidation) {
		t.Fatalf("tax: %v", err)
	}
	tc := DefaultTactical()
	tc.MinSize, tc.MaxSize = d("0.03"), d("0.02")
	if _, err := s.SetTactical(ctx, tc); !errors.Is(err, ErrValidation) {
		t.Fatalf("tactical: %v", err)
	}
	ai := DefaultAI()
	ai.Model = " "
	if _, err := s.SetAI(ctx, ai); !errors.Is(err, ErrValidation) {
		t.Fatalf("ai: %v", err)
	}
}

func TestStatusHasNoValues(t *testing.T) {
	s := newSvc(t)
	st, err := s.Status(context.Background())
	if err != nil || len(st.Keys) != 1 || st.Keys[0].Source != "env" || st.AIUsageUSD.String() != "0" {
		t.Fatalf("status = %+v, %v", st, err)
	}
}
