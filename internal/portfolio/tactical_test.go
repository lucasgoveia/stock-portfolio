package portfolio

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func TestTacticalLifecycle(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	itub := e.ids["ITUB4"]

	note, err := e.research.Create(ctx, itub, research.CreateInput{Body: "Rate cuts lift NII next quarter"})
	if err != nil {
		t.Fatal(err)
	}
	in := tactical.Input{NoteID: note.ID, EntryPrice: m("30"), TargetPrice: m("36"), StopPrice: m("27"), ExpiresOn: "2026-12-31"}
	if _, err := e.tactical.Put(ctx, itub, in); !errors.Is(err, tactical.ErrValidation) {
		t.Fatalf("core asset must be refused: %v", err)
	}
	sleeve := assets.SleeveTactical
	if _, err := e.svc.d.Assets.Update(ctx, itub, assets.UpdateInput{Sleeve: &sleeve}); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.StopPrice = m("31")
	if _, err := e.tactical.Put(ctx, itub, bad); !errors.Is(err, tactical.ErrValidation) {
		t.Fatalf("stop above entry: %v", err)
	}
	st, err := e.tactical.Put(ctx, itub, in)
	if err != nil {
		t.Fatal(err)
	}
	// Stop distance 10% -> 1%/10% = 10%, capped at 2%.
	near(t, "size", st.Sizing.Size, "0.02")

	snap, err := e.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var x Position
	for _, h := range snap.Holdings {
		if h.AssetID == itub {
			x = h
		}
	}
	if x.Tactical == nil || len(x.Tactical.Signals) != 0 || x.Target != nil || snap.TacticalOpen != 1 {
		t.Fatalf("tactical view = %+v open %d", x.Tactical, snap.TacticalOpen)
	}
	near(t, "suggested value", x.Tactical.SuggestedValueBRL, "1300") // 2% of R$65k
	// Gate judged against the target price (36), not the fair value (35).
	if x.Gate == nil || !x.Gate.FairValueSet {
		t.Fatalf("gate = %+v", x.Gate)
	}
	near(t, "gate discount", x.Gate.Discount, "0.166667") // (36-30)/36
	near(t, "tactical weight", snap.TacticalWeight, "0.230769")

	// The planner never funds tactical positions.
	pl, err := e.svc.PlanContribution(ctx, d("5000"))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range pl.Orders {
		if o.AssetID == key(itub) {
			t.Fatalf("tactical asset funded: %+v", o)
		}
	}

	// Price reaches the target: the daily evaluation raises tactical_target_hit.
	if _, err := e.conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, '2026-11-19', 'cotahist', '37', 'x')`, itub); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	open, err := e.alerts.List(ctx, alerts.Filter{State: "open", Type: "tactical_target_hit"})
	if err != nil || len(open) != 1 || *open[0].AssetID != itub {
		t.Fatalf("target alert = %+v %v", open, err)
	}

	// Promotion fails the core gate at 37 (fair value 35, tolerance limit 36.75) ...
	if _, err := e.svc.Promote(ctx, itub); !errors.Is(err, ErrValidation) {
		t.Fatalf("promote above fair value: %v", err)
	}
	// ... and a price slightly above the fair value (36 <= 36.75) is tolerated by the gate.
	if _, err := e.conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, '2026-11-20', 'cotahist', '36', 'x')`, itub); err != nil {
		t.Fatal(err)
	}
	// Within tolerance the price check passes, but a broken thesis still blocks.
	if _, err := e.conn.Exec(`UPDATE asset_prices SET close = '36' WHERE asset_id = ? AND ref_date = '2026-11-20'`, itub); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.d.Thesis.SetStatus(ctx, itub, thesis.StatusBroken); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Promote(ctx, itub); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "thesis broken") {
		t.Fatalf("promote with broken thesis: %v", err)
	}
	// A thesis under review does not block.
	if _, err := e.svc.d.Thesis.SetStatus(ctx, itub, thesis.StatusUnderReview); err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.Promote(ctx, itub)
	if err != nil || a.Sleeve != assets.SleeveCore {
		t.Fatalf("promote: %+v %v", a, err)
	}
	if _, err := e.tactical.Get(ctx, itub); !errors.Is(err, tactical.ErrNotFound) {
		t.Fatalf("setup should be gone: %v", err)
	}
	snap, err = e.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range snap.Holdings {
		if h.AssetID == itub && (h.Target == nil || h.Tactical != nil) {
			t.Fatalf("promoted asset should have a core target: %+v", h)
		}
	}
	if _, err := e.svc.Promote(ctx, itub); !errors.Is(err, ErrValidation) {
		t.Fatalf("promote core asset: %v", err)
	}
}

func TestTacticalCountAlert(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sleeve := assets.SleeveTactical
	for i, tk := range []string{"T1", "T2", "T3", "T4"} {
		a, err := e.svc.d.Assets.Create(ctx, assets.CreateInput{Ticker: tk, Market: "B3", Kind: "stock", Sleeve: sleeve})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', '10', 'x')`, a.ID, today); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.d.Trades.Create(ctx, a.ID, tradeIn("10")); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Evaluate(ctx); err != nil {
			t.Fatal(err)
		}
		open, _ := e.alerts.List(ctx, alerts.Filter{State: "open", Type: "tactical_count"})
		if want := i == 3; (len(open) == 1) != want {
			t.Fatalf("after %d tactical positions: count alerts %d", i+1, len(open))
		}
	}
}

func tradeIn(qty string) trades.CreateInput {
	return trades.CreateInput{Date: "2026-10-01", Side: "buy", Quantity: m(qty), Price: m("10")}
}

func TestCatalystNoteProtected(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	id := e.ids["ITUB4"]
	sleeve := assets.SleeveTactical
	if _, err := e.svc.d.Assets.Update(ctx, id, assets.UpdateInput{Sleeve: &sleeve}); err != nil {
		t.Fatal(err)
	}
	note, err := e.research.Create(ctx, id, research.CreateInput{Body: "catalyst"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tactical.Put(ctx, id, tactical.Input{NoteID: note.ID, EntryPrice: m("30"), TargetPrice: m("36"),
		StopPrice: m("27"), ExpiresOn: "2026-12-31"}); err != nil {
		t.Fatal(err)
	}
	if err := e.research.Delete(ctx, note.ID); !errors.Is(err, research.ErrValidation) {
		t.Fatalf("deleting the catalyst note: %v", err)
	}
}
