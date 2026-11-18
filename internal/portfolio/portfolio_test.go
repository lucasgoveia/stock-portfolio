package portfolio

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/tax"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func m(s string) money.Decimal   { return money.New(d(s)) }
func mp(s string) *money.Decimal { v := m(s); return &v }

type env struct {
	conn     *sql.DB
	svc      *Service
	alerts   *alerts.Service
	research *research.Service
	tactical *tactical.Service
	ids      map[string]int64
}

const today = "2026-11-18"

// setup builds a R$65k portfolio:
//   - WEGE3 holding 1000 x 40 (score 5, FV 30: overvalued, far overweight)
//   - ITUB4 holding 500 x 30 (score 3, FV 35)
//   - BBAS3 watchlist at 20 (score 4, FV 30: 33% discount, an opportunity)
//   - R$10k in a CDI instrument.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	rs := cash.NewRateStore(conn)
	e := &env{conn: conn, alerts: al, ids: map[string]int64{}}
	tr := trades.NewService(conn, as)
	rt := ratings.NewService(conn, as, clock)
	th := thesis.NewService(conn, as, al, clock)
	va := valuation.NewService(conn, as, al, clock)
	cs := cash.NewService(conn, rs, clock)

	for _, a := range []struct {
		tk, fv, price string
		f             [5]int
		buy           string
		buyPx         string
	}{
		{"WEGE3", "30", "40", [5]int{5, 5, 5, 5, 5}, "1000", "40"},
		{"ITUB4", "35", "30", [5]int{3, 3, 3, 3, 3}, "500", "30"},
		{"BBAS3", "30", "20", [5]int{4, 4, 4, 4, 4}, "", ""},
	} {
		x, err := as.Create(ctx, assets.CreateInput{Ticker: a.tk, Market: "B3", Kind: "stock"})
		if err != nil {
			t.Fatal(err)
		}
		e.ids[a.tk] = x.ID
		if a.buy != "" {
			if _, err := tr.Create(ctx, x.ID, trades.CreateInput{Date: "2026-10-01", Side: "buy", Quantity: m(a.buy), Price: m(a.buyPx)}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := rt.Add(ctx, x.ID, ratings.AddInput{Volatility: a.f[0], Understanding: a.f[1], Moat: a.f[2],
			Predictability: a.f[3], Horizon: a.f[4], Reason: "seed"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := th.Put(ctx, x.ID, thesis.PutInput{Text: "seed"}); err != nil {
			t.Fatal(err)
		}
		if _, err := th.AddCondition(ctx, x.ID, "breaks if X"); err != nil {
			t.Fatal(err)
		}
		if _, err := va.Create(ctx, x.ID, valuation.CreateInput{Method: "manual", ValuePerShare: mp(a.fv)}); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', ?, 'x')`, x.ID, today, a.price); err != nil {
			t.Fatal(err)
		}
	}
	ins, err := cs.Create(ctx, cash.CreateInput{Kind: "cdi", Name: "CDB"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddMovement(ctx, ins.ID, cash.MovementInput{Date: today, Type: "balance", Amount: m("10000")}); err != nil {
		t.Fatal(err)
	}
	rn := research.NewService(conn, as, clock)
	e.research, e.tactical = rn, tactical.NewService(conn, as, rn)
	e.svc = NewService(Deps{Conn: conn, Assets: as, Trades: tr, Cash: cs, Rates: rs, Ratings: rt, Valuation: va,
		Thesis: th, Research: rn, Alerts: al, Tactical: e.tactical, Tax: tax.NewService(as, tr, clock), Now: clock})
	return e
}

func near(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	if got.Sub(d(want)).Abs().GreaterThan(d("0.0001")) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

func TestSnapshot(t *testing.T) {
	e := setup(t)
	s, err := e.svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	near(t, "total", s.TotalValue, "65000")
	near(t, "cash weight", s.CashWeight, "0.153846")
	// WEGE3 is the only overvalued core holding: 40000/55000 of core weight.
	near(t, "overvalued share", s.OvervaluedShare, "0.727273")
	near(t, "cash target", s.CashTarget, "0.156364")
	if s.Bootstrap || !s.TooFew || len(s.Holdings) != 2 || len(s.Watchlist) != 1 {
		t.Fatalf("snapshot = %+v", s)
	}
	w := s.Holdings[1] // sorted by ticker: ITUB4, WEGE3
	if w.Ticker != "WEGE3" || !w.Overvalued || !w.Band.AboveUpper || !w.Band.AboveCap || w.RampStage != 1 {
		t.Fatalf("WEGE3 = %+v", w)
	}
	near(t, "WEGE3 weight", w.Weight, "0.615385")
	b := s.Watchlist[0]
	if b.Gate == nil || !b.Gate.Passed || !b.Gate.Opportunity || b.Target == nil {
		t.Fatalf("BBAS3 = %+v", b)
	}
	if len(s.Unpriced) != 0 || len(s.Unrated) != 0 {
		t.Fatalf("unpriced %v unrated %v", s.Unpriced, s.Unrated)
	}
}

func TestPlanContribution(t *testing.T) {
	e := setup(t)
	pl, err := e.svc.PlanContribution(context.Background(), d("5000"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.Orders) < 2 {
		t.Fatalf("orders = %+v", pl.Orders)
	}
	// BBAS3 opportunity first: first ramp tranche of a 10% target on R$70k
	// = 2333.33, 116 shares at 20.
	o := pl.Orders[0]
	if pl.Tickers[o.AssetID] != "BBAS3" || o.Step != "opportunity" || !o.Quantity.Equal(d("116")) || o.RampStageAfter != 1 {
		t.Fatalf("first order = %+v", o)
	}
	if last := pl.Orders[len(pl.Orders)-1]; last.Kind != engine.OrderCash {
		t.Fatalf("rest should go to cash: %+v", pl.Orders)
	}
	near(t, "new money", pl.NewMoney, "5000")
	if _, err := e.svc.PlanContribution(context.Background(), d("0")); err == nil {
		t.Fatal("zero amount must fail")
	}
}

func TestEvaluateAlerts(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	open, err := e.alerts.List(ctx, alerts.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range open {
		got = append(got, a.Type)
	}
	sort.Strings(got)
	// ITUB4 (23%) is also above its 12% upper band: every target is 10% (too few stocks).
	want := []string{"better_opportunity", "opportunity", "overvalued", "overweight", "overweight", "undeployed_budget"}
	if len(got) != len(want) {
		t.Fatalf("alerts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("alerts = %v, want %v", got, want)
		}
	}
	// Trim alerts resolve once their condition clears.
	// A price crash to 1 puts WEGE3 at 1000/26000 = 3.8%, inside its band
	// and far below fair value.
	if _, err := e.conn.Exec(`UPDATE asset_prices SET close = '1' WHERE asset_id = ?`, e.ids["WEGE3"]); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	wege := e.ids["WEGE3"]
	for _, typ := range []string{"overweight", "overvalued"} {
		if l, _ := e.alerts.List(ctx, alerts.Filter{Type: typ, AssetID: &wege}); len(l) != 0 {
			t.Errorf("WEGE3 %s should resolve: %d open", typ, len(l))
		}
	}
	itub := e.ids["ITUB4"]
	if l, _ := e.alerts.List(ctx, alerts.Filter{Type: "overweight", AssetID: &itub}); len(l) != 1 {
		t.Errorf("ITUB4 overweight should stay open")
	}
}

func TestTrims(t *testing.T) {
	e := setup(t)
	tr, err := e.svc.Trims(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var wege *engine.TrimSuggestion
	for i := range tr.Suggestions {
		if tr.Tickers[tr.Suggestions[i].AssetID] == "WEGE3" {
			wege = &tr.Suggestions[i]
		}
	}
	if wege == nil || len(wege.Reasons) != 2 {
		t.Fatalf("suggestions = %+v", tr.Suggestions)
	}
	// Overvalued trims from the lower band (61.5% - 7.5%), a larger basis than
	// overweight's (61.5% - 10%), so it is the primary reason.
	if wege.Reason != engine.ReasonOvervalued {
		t.Fatalf("primary = %s", wege.Reason)
	}
}

func TestRampAndLastSell(t *testing.T) {
	ts := []trades.Trade{
		{ID: 1, Date: "2026-01-01", Side: "buy", Quantity: d("10")},
		{ID: 2, Date: "2026-02-01", Side: "sell", Quantity: d("10")},
		{ID: 3, Date: "2026-03-01", Side: "buy", Quantity: d("5")},
		{ID: 4, Date: "2026-04-01", Side: "buy", Quantity: d("5")},
	}
	stage, last := rampAndLastSell(ts, 3)
	if stage != 2 || last == nil || *last != "2026-02-01" {
		t.Fatalf("stage %d last %v", stage, last)
	}
	if stage, _ := rampAndLastSell(append(ts, trades.Trade{ID: 5, Date: "2026-05-01", Side: "buy", Quantity: d("1")},
		trades.Trade{ID: 6, Date: "2026-06-01", Side: "buy", Quantity: d("1")}), 3); stage != 3 {
		t.Fatalf("capped stage = %d", stage)
	}
}

func TestBootstrapExitAlert(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	// First evaluation has no previous value: no alert.
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.alerts.List(ctx, alerts.Filter{Type: "bootstrap_exit"}); len(l) != 0 {
		t.Fatalf("unexpected bootstrap_exit: %+v", l)
	}
	if err := e.svc.setSetting(ctx, SettingLastValue, d("9000")); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if l, _ := e.alerts.List(ctx, alerts.Filter{Type: "bootstrap_exit"}); len(l) != 1 {
		t.Fatalf("bootstrap_exit = %+v", l)
	}
}
