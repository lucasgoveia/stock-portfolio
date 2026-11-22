package portfolio

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// emptyEnv is a portfolio with no trades and no cash. Assets are added with add.
type emptyEnv struct {
	t     *testing.T
	svc   *Service
	as    *assets.Service
	tr    *trades.Service
	rt    *ratings.Service
	va    *valuation.Service
	ids   map[string]int64
	setPx func(id int64, px string)
}

func newEmptyEnv(t *testing.T) *emptyEnv {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	rs := cash.NewRateStore(conn)
	if err := rs.UpsertUSDBRL(context.Background(), today, d("5")); err != nil {
		t.Fatal(err)
	}
	tr := trades.NewService(conn, as)
	rt := ratings.NewService(conn, as, clock)
	th := thesis.NewService(conn, as, al, clock)
	va := valuation.NewService(conn, as, al, clock)
	cs := cash.NewService(conn, rs, clock)
	rn := research.NewService(conn, as, clock)
	e := &emptyEnv{t: t, as: as, tr: tr, rt: rt, va: va, ids: map[string]int64{}}
	e.svc = NewService(Deps{Conn: conn, Assets: as, Trades: tr, Cash: cs, Rates: rs, Ratings: rt, Valuation: va,
		Thesis: th, Research: rn, Alerts: al, Now: clock})
	e.setPx = func(id int64, px string) {
		if _, err := conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', ?, 'x')`, id, today, px); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// add creates a watchlist asset with a rating (score = factor), price and an
// optional fair value in the asset's currency. It has no thesis.
func (e *emptyEnv) add(in assets.CreateInput, factor int, price, fv string) int64 {
	e.t.Helper()
	ctx := context.Background()
	a, err := e.as.Create(ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	e.ids[in.Ticker] = a.ID
	if factor > 0 {
		if _, err := e.rt.Add(ctx, a.ID, ratings.AddInput{Volatility: factor, Understanding: factor, Moat: factor,
			Predictability: factor, Horizon: factor, Reason: "seed"}); err != nil {
			e.t.Fatal(err)
		}
	}
	if fv != "" {
		if _, err := e.va.Create(ctx, a.ID, valuation.CreateInput{Method: "manual", ValuePerShare: mp(fv)}); err != nil {
			e.t.Fatal(err)
		}
	}
	e.setPx(a.ID, price)
	return a.ID
}

func (e *emptyEnv) position(snap Snapshot, ticker string) Position {
	e.t.Helper()
	for _, p := range append(append([]Position(nil), snap.Holdings...), snap.Watchlist...) {
		if p.Ticker == ticker {
			return p
		}
	}
	e.t.Fatalf("%s not in the snapshot", ticker)
	return Position{}
}

func tickerOf(pl ContributionPlan, o engine.Order) string { return pl.Tickers[o.AssetID] }

// Issue #68: an empty portfolio starts positions from watchlist names that pass
// the gate, in at most two orders above the minimum, and says why money is left.
func TestPlanFromEmptyPortfolio(t *testing.T) {
	ctx := context.Background()
	e := newEmptyEnv(t)
	for i, tk := range []string{"AAA3", "BBB3", "CCC3", "DDD3", "EEE3", "FFF3"} {
		e.add(assets.CreateInput{Ticker: tk, Market: "B3", Kind: "stock"}, 3+i%2, "10", "12")
	}
	// A name above its fair value never gets money.
	e.add(assets.CreateInput{Ticker: "RICH3", Market: "B3", Kind: "stock"}, 5, "50", "20")

	pl, err := e.svc.PlanContribution(ctx, d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	var buys []engine.Order
	for _, o := range pl.Orders {
		if o.Kind == engine.OrderBuy {
			buys = append(buys, o)
		}
	}
	if len(buys) != 2 {
		t.Fatalf("want 2 buys, got %+v", pl.Orders)
	}
	for _, o := range buys {
		if o.Amount.LessThan(d("300")) {
			t.Errorf("%s order %s is below the minimum", tickerOf(pl, o), o.Amount)
		}
		if tickerOf(pl, o) == "RICH3" {
			t.Errorf("RICH3 fails the gate")
		}
	}
	if pl.Unallocated.GreaterThan(d("60")) {
		t.Errorf("unallocated = %s; the two orders and the cash target should place nearly everything", pl.Unallocated)
	}
	waiting := 0
	for _, s := range pl.Skipped {
		if s.Reason == engine.ReasonMaxNewPositions {
			waiting++
		}
	}
	if waiting != 4 {
		t.Errorf("skipped = %+v", pl.Skipped)
	}
}

// When nothing passes the gate the plan says so instead of staying silent.
func TestPlanFromEmptyPortfolioNothingEligible(t *testing.T) {
	e := newEmptyEnv(t)
	e.add(assets.CreateInput{Ticker: "RICH3", Market: "B3", Kind: "stock"}, 5, "50", "20")
	pl, err := e.svc.PlanContribution(context.Background(), d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Notes) == 0 || pl.Notes[0].Code != engine.NoteNoEligible {
		t.Fatalf("notes = %+v", pl.Notes)
	}
	near(t, "unallocated", pl.Unallocated, "960") // 4% goes to the cash target
}

// US assets planned directly go to the pending US bucket and the plan says so.
func TestPlanDirectUSMoneyGoesToPendingBucket(t *testing.T) {
	e := newEmptyEnv(t)
	e.svc.d.Params.USVehicle = engine.USVehicleDirect
	e.add(assets.CreateInput{Ticker: "MSFT", Market: "US", Kind: "stock"}, 4, "100", "150")
	pl, err := e.svc.PlanContribution(context.Background(), d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	var hold *engine.Order
	for i := range pl.Orders {
		if pl.Orders[i].Kind == engine.OrderHold {
			hold = &pl.Orders[i]
		}
	}
	if hold == nil || len(hold.Allocations) != 1 || pl.Tickers[hold.Allocations[0].AssetID] != "MSFT" {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	near(t, "pending after", pl.PendingUSAfter, "960")
	found := false
	for _, n := range pl.Notes {
		found = found || n.Code == engine.NotePendingUS
	}
	if !found {
		t.Errorf("notes = %+v", pl.Notes)
	}
}

// ---- #69 BDRs ----

// bdrEnv: MSFT (US, score 4, FV US$500, price US$400) and MSFT34, a BDR worth
// 1/20 of a share (price R$100). USD/BRL is 5, so the BDR's fair value is
// 500 x 5 / 20 = R$125 and R$100 is a 20% discount.
func bdrEnv(t *testing.T) (*emptyEnv, int64, int64) {
	e := newEmptyEnv(t)
	us := e.add(assets.CreateInput{Ticker: "MSFT", Market: "US", Kind: "stock"}, 4, "400", "500")
	bdr := e.add(assets.CreateInput{Ticker: "MSFT34", Market: "B3", Kind: "bdr", UnderlyingAssetID: &us, BDRRatio: "20"}, 0, "100", "")
	return e, us, bdr
}

func TestBDRTakesRatingFairValueAndGateFromUnderlying(t *testing.T) {
	e, us, _ := bdrEnv(t)
	snap, err := e.svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, u := e.position(snap, "MSFT34"), e.position(snap, "MSFT")
	if b.UnderlyingAssetID == nil || *b.UnderlyingAssetID != us || b.GroupID != us || u.GroupID != us {
		t.Fatalf("groups: %+v %+v", b, u)
	}
	if b.Score == nil || u.Score == nil || !b.Score.Equal(*u.Score) {
		t.Fatalf("score: bdr %v underlying %v", b.Score, u.Score)
	}
	if b.FairValue == nil {
		t.Fatal("BDR fair value missing")
	}
	near(t, "BDR fair value", *b.FairValue, "125")
	if b.Gate == nil || !b.Gate.Passed || !b.Gate.Opportunity || b.GateError != "" {
		t.Fatalf("BDR gate = %+v", b.Gate)
	}
	if len(snap.Unrated) != 0 {
		t.Errorf("unrated = %v", snap.Unrated)
	}
	// One position, one target (bootstrap: the whole 96% core budget).
	if b.Target == nil || u.Target == nil || !b.Target.Weight.Equal(u.Target.Weight) {
		t.Fatalf("targets: %+v %+v", b.Target, u.Target)
	}
	near(t, "target", b.Target.Weight, "0.96")
}

func TestPlanBuysTheBDRByDefault(t *testing.T) {
	e, _, _ := bdrEnv(t)
	pl, err := e.svc.PlanContribution(context.Background(), d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	var buy *engine.Order
	for i := range pl.Orders {
		switch pl.Orders[i].Kind {
		case engine.OrderBuy:
			buy = &pl.Orders[i]
		case engine.OrderHold, engine.OrderRemit:
			t.Fatalf("a BDR plan needs no US bucket: %+v", pl.Orders)
		}
	}
	if buy == nil || pl.Tickers[buy.AssetID] != "MSFT34" || buy.Market != engine.MarketB3 || buy.Step != "opportunity" {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	if buy.Amount.LessThan(d("300")) {
		t.Errorf("below the B3 minimum: %s", buy.Amount)
	}
	near(t, "pending", pl.PendingUSAfter, "0")
}

func TestPlanDirectVehicleBuysTheUnderlying(t *testing.T) {
	e, _, _ := bdrEnv(t)
	e.svc.d.Params.USVehicle = engine.USVehicleDirect
	pl, err := e.svc.PlanContribution(context.Background(), d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range pl.Orders {
		if o.Kind == engine.OrderBuy {
			t.Fatalf("direct US buys wait in the bucket: %+v", pl.Orders)
		}
	}
	var hold *engine.Order
	for i := range pl.Orders {
		if pl.Orders[i].Kind == engine.OrderHold {
			hold = &pl.Orders[i]
		}
	}
	if hold == nil || pl.Tickers[hold.Allocations[0].AssetID] != "MSFT" {
		t.Fatalf("orders = %+v", pl.Orders)
	}
}

// The underlying and its BDR are one position: weights, targets and the plan
// count both, and only one vehicle is a buy candidate.
func TestBDRAndUnderlyingCountAsOnePosition(t *testing.T) {
	ctx := context.Background()
	e, us, bdr := bdrEnv(t)
	// R$2,000 in the underlying (1 share at US$400) and R$1,000 in the BDR.
	if _, err := e.tr.Create(ctx, us, trades.CreateInput{Date: "2026-10-01", Side: "buy", Quantity: m("1"), Price: m("400"), FXRate: mp("5")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tr.Create(ctx, bdr, trades.CreateInput{Date: "2026-10-01", Side: "buy", Quantity: m("10"), Price: m("100")}); err != nil {
		t.Fatal(err)
	}
	snap, err := e.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, u := e.position(snap, "MSFT34"), e.position(snap, "MSFT")
	near(t, "total", snap.TotalValue, "3000")
	near(t, "group value", b.GroupValueBRL, "3000")
	near(t, "group weight", u.GroupWeight, "1")
	near(t, "bdr own weight", b.Weight, "0.333333")
	if !b.BuyVehicle || u.BuyVehicle {
		t.Errorf("vehicle: bdr %v underlying %v", b.BuyVehicle, u.BuyVehicle)
	}

	pl, err := e.svc.PlanContribution(ctx, d("1000"))
	if err != nil {
		t.Fatal(err)
	}
	// The position already holds R$3,000 against a 96% target of R$3,840: only
	// R$840 of room, so the buy is capped by the combined holding.
	for _, o := range pl.Orders {
		if o.Kind == engine.OrderBuy && o.Amount.GreaterThan(d("840")) {
			t.Errorf("order ignores the underlying holding: %+v", o)
		}
	}
}
