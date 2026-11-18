package portfolio

import (
	"context"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/settings"
)

// A changed setting must change the next snapshot and plan.
func TestSettingsChangeSnapshotAndPlan(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	ss := settings.NewService(e.conn, nil)
	e.svc.d.Settings = ss

	snap, err := e.svc.Snapshot(ctx)
	if err != nil || snap.Bootstrap || snap.TacticalMaxOpen != 3 {
		t.Fatalf("defaults: %+v, %v", snap, err)
	}
	hasBBAS := func() bool {
		pl, err := e.svc.PlanContribution(ctx, d("5000"))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range pl.Orders {
			if pl.Tickers[o.AssetID] == "BBAS3" && o.Quantity.IsPositive() {
				return true
			}
		}
		return false
	}
	if !hasBBAS() {
		t.Fatal("default plan should buy BBAS3 (R$2,333 tranche)")
	}

	// Raise the minimum order above the BBAS3 tranche: the order is skipped.
	pl := settings.DefaultPlanner()
	pl.MinOrderB3 = m("3000")
	if _, err := ss.SetPlanner(ctx, pl); err != nil {
		t.Fatal(err)
	}
	if hasBBAS() {
		t.Fatal("BBAS3 order should be skipped under a R$3,000 minimum")
	}

	// Bootstrap threshold and tactical cap flow into the snapshot.
	pl.BootstrapBelow = m("100000")
	if _, err := ss.SetPlanner(ctx, pl); err != nil {
		t.Fatal(err)
	}
	tc := settings.DefaultTactical()
	tc.MaxOpen = 7
	if _, err := ss.SetTactical(ctx, tc); err != nil {
		t.Fatal(err)
	}
	snap, err = e.svc.Snapshot(ctx)
	if err != nil || !snap.Bootstrap || snap.TacticalMaxOpen != 7 {
		t.Fatalf("after change: bootstrap=%v max_open=%d, %v", snap.Bootstrap, snap.TacticalMaxOpen, err)
	}

	// Reset restores the defaults.
	if err := ss.Reset(ctx, settings.GroupPlanner); err != nil {
		t.Fatal(err)
	}
	if !hasBBAS() {
		t.Fatal("reset should restore the BBAS3 order")
	}
}
