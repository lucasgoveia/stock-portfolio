package dividends

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func m(s string) money.Decimal   { return money.New(decimal.RequireFromString(s)) }
func mp(s string) *money.Decimal { v := m(s); return &v }

type env struct {
	svc            *Service
	cash           *cash.Service
	actions        *corpactions.Service
	b3, us         int64
	cdi, usd, selc int64
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
	cs := cash.NewService(conn, cash.NewRateStore(conn), clock)
	e := env{svc: NewService(conn, as, tr, cs, clock), cash: cs, actions: corpactions.NewService(conn, as, tr, nil, clock)}
	for _, x := range []struct {
		tk, market string
		id         *int64
	}{{"ITSA4", "B3", &e.b3}, {"MSFT", "US", &e.us}} {
		a, err := as.Create(ctx, assets.CreateInput{Ticker: x.tk, Market: x.market, Kind: "stock"})
		if err != nil {
			t.Fatal(err)
		}
		*x.id = a.ID
		in := trades.CreateInput{Date: "2026-01-10", Side: "buy", Quantity: m("1000"), Price: m("10")}
		if x.market == "US" {
			in.Quantity, in.FXRate = m("10"), mp("5")
		}
		if _, err := tr.Create(ctx, a.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []struct {
		kind string
		id   *int64
	}{{"cdi", &e.cdi}, {"usd_cash", &e.usd}, {"tesouro_selic", &e.selc}} {
		i, err := cs.Create(ctx, cash.CreateInput{Kind: x.kind, Name: x.kind})
		if err != nil {
			t.Fatal(err)
		}
		*x.id = i.ID
	}
	return e
}

func TestRecordCreditsCash(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// JCP: 1000 shares x 0.50 = 500 gross, 17.5% withheld = 87.50, net 412.50.
	d, err := e.svc.Record(ctx, e.b3, RecordInput{Kind: "jcp", RecordDate: "2026-03-01", PayDate: "2026-04-15",
		PerShare: m("0.5"), InstrumentID: e.cdi})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Gross.Equal(decimal.RequireFromString("500")) || !d.Withholding.Equal(decimal.RequireFromString("87.5")) ||
		!d.Net.Equal(decimal.RequireFromString("412.5")) || d.Status != StatusConfirmed || d.MovementID == nil {
		t.Fatalf("jcp = %+v", d)
	}
	mv, err := e.cash.Movements(ctx, e.cdi)
	if err != nil || len(mv) != 1 || !mv[0].Amount.Equal(decimal.RequireFromString("412.5")) {
		t.Fatalf("movements = %+v %v", mv, err)
	}
	// Wrong instruments are refused.
	for _, ins := range []int64{e.usd, e.selc, 99} {
		if _, err := e.svc.Record(ctx, e.b3, RecordInput{Kind: "dividend", RecordDate: "2026-03-01", PayDate: "2026-04-15",
			PerShare: m("0.1"), InstrumentID: ins}); !errors.Is(err, ErrValidation) {
			t.Fatalf("instrument %d: %v", ins, err)
		}
	}
	// US dividend: 10 x 0.83 = 8.30 gross, 30% = 2.49, net 5.81 USD, at FX 5.5 = 31.955 BRL.
	u, err := e.svc.Record(ctx, e.us, RecordInput{Kind: "dividend", RecordDate: "2026-05-14", PayDate: "2026-06-11",
		PerShare: m("0.83"), FXRate: mp("5.5"), InstrumentID: e.usd})
	if err != nil {
		t.Fatal(err)
	}
	if !u.Net.Equal(decimal.RequireFromString("5.81")) || !u.NetBRL.Equal(decimal.RequireFromString("31.955")) {
		t.Fatalf("us = %+v", u)
	}
	if _, err := e.svc.Record(ctx, e.us, RecordInput{Kind: "dividend", RecordDate: "2026-05-14", PayDate: "2026-06-11",
		PerShare: m("0.83"), InstrumentID: e.usd}); !errors.Is(err, ErrValidation) {
		t.Fatalf("usd without fx: %v", err)
	}
	// Not held before the purchase.
	if _, err := e.svc.Record(ctx, e.b3, RecordInput{Kind: "dividend", RecordDate: "2025-12-01", PayDate: "2026-01-15",
		PerShare: m("0.1"), InstrumentID: e.cdi}); !errors.Is(err, ErrValidation) {
		t.Fatalf("not held: %v", err)
	}

	// Undeployed money and trailing yield.
	und, err := e.svc.Undeployed(ctx)
	if err != nil || !und.Equal(decimal.RequireFromString("444.455")) {
		t.Fatalf("undeployed = %s %v", und, err)
	}
	ttm, err := e.svc.TrailingNetBRL(ctx, time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC))
	if err != nil || !ttm[e.b3].Equal(decimal.RequireFromString("412.5")) {
		t.Fatalf("ttm = %v %v", ttm, err)
	}

	// Deleting removes the cash credit.
	if err := e.svc.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if mv, _ := e.cash.Movements(ctx, e.cdi); len(mv) != 0 {
		t.Fatalf("movement left behind: %+v", mv)
	}
}

func TestDetectConfirmReject(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-08-20", "2026-09-01", decimal.RequireFromString("0.25"), "brapi")
	if err != nil || !ok {
		t.Fatalf("detect: %v %v", ok, err)
	}
	// The same announcement is proposed once.
	if ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-08-20", "2026-09-01", decimal.RequireFromString("0.25"), "brapi"); err != nil || ok {
		t.Fatalf("duplicate detect: %v %v", ok, err)
	}
	// Nothing held before the first buy: not proposed.
	if ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2025-08-20", "", decimal.RequireFromString("0.25"), "brapi"); err != nil || ok {
		t.Fatalf("detect before holding: %v %v", ok, err)
	}
	queue, err := e.svc.List(ctx, Filter{Status: StatusDetected})
	if err != nil || len(queue) != 1 || !queue[0].Gross.Equal(decimal.RequireFromString("250")) {
		t.Fatalf("queue = %+v %v", queue, err)
	}
	if und, _ := e.svc.Undeployed(ctx); !und.IsZero() {
		t.Fatalf("detected counts as money: %s", und)
	}
	c, err := e.svc.Confirm(ctx, queue[0].ID, ConfirmInput{InstrumentID: e.cdi})
	if err != nil || c.Status != StatusConfirmed || c.MovementID == nil || *c.PayDate != "2026-09-01" {
		t.Fatalf("confirm: %+v %v", c, err)
	}
	if _, err := e.svc.Confirm(ctx, c.ID, ConfirmInput{InstrumentID: e.cdi}); !errors.Is(err, ErrConflict) {
		t.Fatalf("confirm twice: %v", err)
	}
	if _, err := e.svc.Reject(ctx, c.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("reject confirmed: %v", err)
	}
	if _, err := e.svc.Detect(ctx, e.b3, "jcp", "2026-10-01", "", decimal.RequireFromString("0.1"), "brapi"); err != nil {
		t.Fatal(err)
	}
	q2, _ := e.svc.List(ctx, Filter{Status: StatusDetected})
	if r, err := e.svc.Reject(ctx, q2[0].ID); err != nil || r.Status != StatusRejected {
		t.Fatalf("reject: %+v %v", r, err)
	}
	// Deployed once a plan takes it.
	if err := e.svc.MarkDeployed(ctx, 0); err == nil {
		// contribution 0 does not exist: the FK must refuse it
		t.Fatal("mark deployed with a missing plan should fail")
	}
}

func TestDetectSizesAcrossLaterSplit(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	// 1000 shares on the record date; a confirmed 2:1 split follows. The brapi
	// rate (0.25) is on the post-split scale, so gross = 0.25 x 1000 x 2.
	if _, err := e.actions.Record(ctx, e.b3, corpactions.Input{Kind: corpactions.KindSplit, ExDate: "2026-09-01", Factor: m("2")}); err != nil {
		t.Fatal(err)
	}
	ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-08-20", "2026-09-15", decimal.RequireFromString("0.25"), "brapi")
	if err != nil || !ok {
		t.Fatalf("detect: %v %v", ok, err)
	}
	q, _ := e.svc.List(ctx, Filter{Status: StatusDetected})
	if len(q) != 1 || !q[0].Gross.Equal(decimal.RequireFromString("500")) || !q[0].Quantity.Equal(decimal.RequireFromString("2000")) {
		t.Fatalf("queue = %+v", q)
	}
	// A record date on or after the ex-date is already on the new scale.
	if ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-09-01", "", decimal.RequireFromString("0.25"), "brapi"); err != nil || !ok {
		t.Fatalf("detect after split: %v %v", ok, err)
	}
	q, _ = e.svc.List(ctx, Filter{Status: StatusDetected})
	if !q[0].Gross.Equal(decimal.RequireFromString("500")) { // newest record date first: 2000 shares x 0.25
		t.Fatalf("post-split record = %+v", q[0])
	}
}

func TestDetectHeldWhilePendingEvent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if ok, err := e.actions.Detect(ctx, e.b3, corpactions.KindSplit, "2026-09-01", decimal.RequireFromString("2"), "brapi", "DESDOBRAMENTO"); err != nil || !ok {
		t.Fatalf("detect action: %v %v", ok, err)
	}
	ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-08-20", "2026-09-15", decimal.RequireFromString("0.25"), "brapi")
	if err != nil || ok {
		t.Fatalf("pending event should hold the proposal: %v %v", ok, err)
	}
	if q, _ := e.svc.List(ctx, Filter{}); len(q) != 0 {
		t.Fatalf("nothing should be stored: %+v", q)
	}
	acts, _ := e.actions.List(ctx, corpactions.StatusDetected, e.b3)
	if _, err := e.actions.Confirm(ctx, acts[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := e.svc.Detect(ctx, e.b3, "dividend", "2026-08-20", "2026-09-15", decimal.RequireFromString("0.25"), "brapi"); err != nil || !ok {
		t.Fatalf("detect after confirm: %v %v", ok, err)
	}
	q, _ := e.svc.List(ctx, Filter{Status: StatusDetected})
	if len(q) != 1 || !q[0].Gross.Equal(decimal.RequireFromString("500")) {
		t.Fatalf("queue = %+v", q)
	}
}
