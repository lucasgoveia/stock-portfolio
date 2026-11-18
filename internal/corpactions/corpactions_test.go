package corpactions

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func m(s string) money.Decimal   { return money.New(d(s)) }
func mp(s string) *money.Decimal { v := m(s); return &v }

// MGLU3 raw COTAHIST closes around 2026-10-26: a 2.39x drop with no event
// announced by brapi (fetched from B3 on 2026-11-18).
var mglu3 = [][2]string{
	{"2026-10-20", "13.58"}, {"2026-10-21", "13.04"}, {"2026-10-22", "12.78"}, {"2026-10-23", "12.69"},
	{"2026-10-26", "5.30"}, {"2026-10-27", "5.75"}, {"2026-10-28", "5.22"}, {"2026-10-29", "5.13"}, {"2026-10-30", "5.35"},
}

func TestFindJumpsMGLU3(t *testing.T) {
	var dates []string
	var closes []decimal.Decimal
	for _, p := range mglu3 {
		dates, closes = append(dates, p[0]), append(closes, d(p[1]))
	}
	j := FindJumps(dates, closes)
	if len(j) != 1 || j[0].Date != "2026-10-26" || !j[0].Ratio.Equal(d("2.3943")) {
		t.Fatalf("jumps = %+v", j)
	}
	// Ordinary daily moves are not jumps; a rise of 1.8x is.
	if j := FindJumps([]string{"a", "b", "c"}, []decimal.Decimal{d("10"), d("11"), d("19.8")}); len(j) != 1 || j[0].Date != "c" {
		t.Fatalf("rise = %+v", j)
	}
}

type env struct {
	conn   *sql.DB
	svc    *Service
	trades *trades.Service
	alerts *alerts.Service
	id     int64
}

func setup(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	tr := trades.NewService(conn, as)
	al := alerts.New(conn, clock)
	a, err := as.Create(ctx, assets.CreateInput{Ticker: "MGLU3", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create(ctx, a.ID, trades.CreateInput{Date: "2026-10-01", Side: "buy", Quantity: m("300"), Price: m("12")}); err != nil {
		t.Fatal(err)
	}
	for _, p := range mglu3 {
		if _, err := conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', ?, 'x')`, a.ID, p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	return env{conn: conn, svc: NewService(conn, as, tr, al, clock), trades: tr, alerts: al, id: a.ID}
}

func (e env) open(t *testing.T, typ string) int {
	t.Helper()
	l, err := e.alerts.List(context.Background(), alerts.Filter{State: "open", Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return len(l)
}

func TestUnexplainedJumpThenManualEvent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if e.open(t, AlertPriceJump) != 1 {
		t.Fatal("the MGLU3 drop should raise price_jump_unexplained")
	}
	// The user enters the event by hand: shares x2.4, same total cost.
	a, err := e.svc.Record(ctx, e.id, Input{Kind: KindSplit, ExDate: "2026-10-26", Factor: m("2.4")})
	if err != nil {
		t.Fatal(err)
	}
	if a.PriceRatio == nil || *a.PriceRatio != "2.3943" || a.RatioMatches == nil || !*a.RatioMatches {
		t.Fatalf("price check = %+v", a)
	}
	p, err := e.trades.Position(ctx, e.id)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Quantity.Equal(d("720")) || !p.TotalCost.Equal(d("3600")) || !p.AvgCost.Equal(d("5")) {
		t.Fatalf("position = %+v", p)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if e.open(t, AlertPriceJump) != 0 {
		t.Fatal("the event explains the jump: alert should resolve")
	}

	// Sales now rely on the split shares, so deleting the event is refused.
	if _, err := e.trades.Create(ctx, e.id, trades.CreateInput{Date: "2026-11-03", Side: "sell", Quantity: m("500"), Price: m("5.5")}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, a.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete needed event: %v", err)
	}
	if got, _ := e.svc.Get(ctx, a.ID); got.Status != StatusConfirmed {
		t.Fatalf("event should stay confirmed: %+v", got)
	}
	// A reverse split before the sale cannot be recorded either.
	if _, err := e.svc.Record(ctx, e.id, Input{Kind: KindReverseSplit, ExDate: "2026-11-01", Factor: m("0.1")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("reverse split breaking the history: %v", err)
	}
}

func TestDetectConfirmBonus(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	ok, err := e.svc.Detect(ctx, e.id, KindBonus, "2026-11-05", d("1.1"), "brapi", "BONIFICACAO 10%")
	if err != nil || !ok {
		t.Fatalf("detect: %v %v", ok, err)
	}
	if ok, _ := e.svc.Detect(ctx, e.id, KindBonus, "2026-11-05", d("1.1"), "brapi", ""); ok {
		t.Fatal("same announcement queued twice")
	}
	if _, err := e.svc.Detect(ctx, e.id, KindSplit, "2026-11-05", d("0.5"), "brapi", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("split below 1: %v", err)
	}
	list, _ := e.svc.List(ctx, StatusDetected, 0)
	if len(list) != 1 || list[0].PriceRatio != nil {
		t.Fatalf("queue = %+v", list)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if e.open(t, AlertPending) != 1 {
		t.Fatal("pending alert expected")
	}
	if _, err := e.svc.Confirm(ctx, list[0].ID, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("bonus without cost: %v", err)
	}
	a, err := e.svc.Confirm(ctx, list[0].ID, mp("4"))
	if err != nil || a.Status != StatusConfirmed {
		t.Fatalf("confirm: %+v %v", a, err)
	}
	p, _ := e.trades.Position(ctx, e.id)
	// 30 new shares at R$4 add 120 to the R$3,600 cost.
	if !p.Quantity.Equal(d("330")) || !p.TotalCost.Equal(d("3720")) {
		t.Fatalf("position = %+v", p)
	}
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if e.open(t, AlertPending) != 0 {
		t.Fatal("pending alert should resolve once nothing waits")
	}
	if _, err := e.svc.Reject(ctx, a.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("reject confirmed: %v", err)
	}
	if err := e.svc.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.trades.Position(ctx, e.id); !p.Quantity.Equal(d("300")) {
		t.Fatalf("after delete: %+v", p)
	}
}

func TestKindFromLabelAndExDate(t *testing.T) {
	for label, want := range map[string]string{"DESDOBRAMENTO": KindSplit, "grupamento": KindReverseSplit, "BONIFICACAO": KindBonus} {
		if got, ok := KindFromLabel(label); !ok || got != want {
			t.Errorf("%s: %s %v", label, got, ok)
		}
	}
	if _, ok := KindFromLabel("SUBSCRICAO"); ok {
		t.Error("subscription is not a share event")
	}
	// Friday's last-day-with-right gives Monday's ex-date.
	if got := NextBusinessDay(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)).Format(dateLayout); got != "2026-10-26" {
		t.Errorf("next business day = %s", got)
	}
}
