package portfolio

import (
	"context"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestTrimTaxAndPoolConflict(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	wege := key(e.ids["WEGE3"])

	r, err := e.svc.Trims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, x := range r.Tax {
		if x.AssetID == wege {
			found = true
			if x.ExceedsExemption || !x.MonthSales.IsZero() {
				t.Fatalf("empty month should fit: %+v", x)
			}
		}
	}
	if !found || r.PoolConflict {
		t.Fatalf("trims tax = %+v conflict %v", r.Tax, r.PoolConflict)
	}

	// A tactical exit of R$15k this month uses most of the R$20k exemption.
	itub := e.ids["ITUB4"]
	tac := assets.SleeveTactical
	if _, err := e.svc.d.Assets.Update(ctx, itub, assets.UpdateInput{Sleeve: &tac}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.d.Trades.Create(ctx, itub, trades.CreateInput{Date: "2026-11-05", Side: "sell", Quantity: m("500"), Price: m("30")}); err != nil {
		t.Fatal(err)
	}
	r, err = e.svc.Trims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Tax {
		if x.AssetID == wege {
			// Room left 5000 at R$40 -> 125 shares fit this month.
			if !x.ExceedsExemption || !x.MonthSales.Equal(d("15000")) || !x.FitQuantity.Equal(d("125")) {
				t.Fatalf("after tactical exit: %+v", x)
			}
		}
	}
	if !r.PoolConflict {
		t.Fatal("tactical exit should conflict with the core trim")
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	open, err := e.alerts.List(ctx, alerts.Filter{State: "open", Type: "tax_pool_conflict"})
	if err != nil || len(open) != 1 {
		t.Fatalf("tax_pool_conflict alerts = %v %v", open, err)
	}
	over, err := e.alerts.List(ctx, alerts.Filter{State: "open", Type: "overweight"})
	if err != nil || len(over) == 0 || !containsTax(string(over[0].Payload)) {
		t.Fatalf("overweight alert should carry tax: %v %v", over, err)
	}
}

func containsTax(payload string) bool {
	return len(payload) > 0 && strings.Contains(payload, `"tax":{`)
}
