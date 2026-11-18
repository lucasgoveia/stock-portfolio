package portfolio

import (
	"context"
	"errors"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestContributionLifecycle(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	c, err := e.svc.CreateContribution(ctx, d("5000"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != ContributionPlanned || len(c.Orders) != len(c.Plan.Orders) || len(c.Orders) < 2 {
		t.Fatalf("created = %+v", c)
	}
	for _, o := range c.Orders {
		if o.Status != OrderOpen || o.TradeID != nil {
			t.Fatalf("order not open: %+v", o)
		}
	}
	buy := c.Plan.Orders[0] // BBAS3 opportunity
	cashOrder := c.Plan.Orders[len(c.Plan.Orders)-1]

	// Record the BBAS3 buy and link it: the plan becomes partial.
	bbas := e.ids["BBAS3"]
	trade, err := e.svc.d.Trades.Create(ctx, bbas, trades.CreateInput{Date: today, Side: "buy", Quantity: m("116"), Price: m("20")})
	if err != nil {
		t.Fatal(err)
	}
	c, err = e.svc.SetOrder(ctx, c.ID, int64(buy.Seq), OrderDone, &trade.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != ContributionPartial || c.Orders[0].Status != OrderDone || c.Orders[0].TradeID == nil || *c.Orders[0].TradeID != trade.ID {
		t.Fatalf("after buy = %+v", c)
	}

	// A trade of another asset cannot be linked.
	wege, err := e.svc.d.Trades.Create(ctx, e.ids["WEGE3"], trades.CreateInput{Date: today, Side: "buy", Quantity: m("1"), Price: m("40")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetOrder(ctx, c.ID, int64(buy.Seq), OrderDone, &wege.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong asset trade: %v", err)
	}
	if _, err := e.svc.SetOrder(ctx, c.ID, int64(cashOrder.Seq), OrderDone, &trade.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("trade on cash order: %v", err)
	}
	if _, err := e.svc.SetOrder(ctx, c.ID, 999, OrderDone, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing order: %v", err)
	}
	if _, err := e.svc.SetOrder(ctx, c.ID, int64(buy.Seq), "bogus", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad status: %v", err)
	}

	// Close every other order: done or skipped both count as executed.
	for i, o := range c.Plan.Orders[1:] {
		st := OrderDone
		if i%2 == 1 {
			st = OrderSkipped
		}
		if c, err = e.svc.SetOrder(ctx, c.ID, int64(o.Seq), st, nil); err != nil {
			t.Fatal(err)
		}
	}
	if c.Status != ContributionExecuted {
		t.Fatalf("status = %s", c.Status)
	}
	// Reopening one order makes it partial again.
	if c, err = e.svc.SetOrder(ctx, c.ID, int64(cashOrder.Seq), OrderOpen, nil); err != nil || c.Status != ContributionPartial {
		t.Fatalf("reopen: %s %v", c.Status, err)
	}

	// Deleting the linked trade keeps the order done but drops the link.
	if err := e.svc.d.Trades.Delete(ctx, trade.ID); err != nil {
		t.Fatal(err)
	}
	if c, err = e.svc.GetContribution(ctx, c.ID); err != nil || c.Orders[0].TradeID != nil || c.Orders[0].Status != OrderDone {
		t.Fatalf("after trade delete: %+v %v", c.Orders[0], err)
	}

	// History is newest first; discarding blocks further changes.
	second, err := e.svc.CreateContribution(ctx, d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.ListContributions(ctx, 0)
	if err != nil || len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("list = %v %v", list, err)
	}
	if second, err = e.svc.DiscardContribution(ctx, second.ID); err != nil || second.Status != ContributionDiscarded || second.DiscardedAt == nil {
		t.Fatalf("discard: %+v %v", second, err)
	}
	if _, err := e.svc.SetOrder(ctx, second.ID, 1, OrderDone, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("change discarded: %v", err)
	}
	if _, err := e.svc.GetContribution(ctx, 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}
