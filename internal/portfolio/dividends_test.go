package portfolio

import (
	"context"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
)

func TestPlannerIncludesUndeployedDividends(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// ITUB4: 500 shares x R$2 JCP = 1000 gross, 175 withheld, 825 net into the CDB.
	if _, err := e.dividends.Record(ctx, e.ids["ITUB4"], dividends.RecordInput{Kind: "jcp", RecordDate: "2026-10-10",
		PayDate: "2026-11-01", PerShare: m("2"), InstrumentID: e.cdiID}); err != nil {
		t.Fatal(err)
	}
	snap, err := e.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "undeployed", snap.UndeployedDividends, "825")
	for _, h := range snap.Holdings {
		if h.Ticker == "ITUB4" {
			near(t, "dividends ttm", h.DividendsTTM, "825")
			near(t, "yield on cost", h.YieldOnCost, "0.055") // 825 / 15000
		}
	}

	pl, err := e.svc.PlanContribution(ctx, d("5000"))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "plan dividends", pl.Dividends, "825")
	near(t, "new money", pl.NewMoney, "5825")

	c, err := e.svc.CreateContribution(ctx, d("5000"))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "stored plan dividends", c.Plan.Dividends, "825")
	if u, _ := e.dividends.Undeployed(ctx); !u.IsZero() {
		t.Fatalf("dividends still undeployed: %s", u)
	}
	pl, err = e.svc.PlanContribution(ctx, d("5000"))
	if err != nil || !pl.Dividends.IsZero() {
		t.Fatalf("next plan dividends = %s %v", pl.Dividends, err)
	}
}
