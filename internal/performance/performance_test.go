package performance

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func m(s string) money.Decimal { return money.New(decimal.RequireFromString(s)) }

func near(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	if got.Sub(decimal.RequireFromString(want)).Abs().GreaterThan(decimal.RequireFromString("0.000001")) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

func TestSnapshotsAndTWR(t *testing.T) {
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 1, 9, 12, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	tr := trades.NewService(conn, as)
	cs := cash.NewService(conn, cash.NewRateStore(conn), clock)
	dv := dividends.NewService(conn, as, tr, cs, clock)
	svc := NewService(conn, as, tr, cs, clock)

	a, _ := as.Create(ctx, assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	ins, _ := cs.Create(ctx, cash.CreateInput{Kind: "cdi", Name: "CDB"})
	for _, p := range [][2]string{{"2026-01-05", "10"}, {"2026-01-06", "11"}, {"2026-01-07", "11.55"}, {"2026-01-08", "11.55"}} {
		if _, err := conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', ?, 'x')`, a.ID, p[0], p[1]); err != nil {
			t.Fatal(err)
		}
	}
	// Day 1: buy 100 x 10 (R$1,000 in). Day 2: price +10%, R$1,000 deposited in cash.
	// Day 3: buy 100 x 11 (R$1,100 in), price +5%. Day 4: a R$50 dividend
	// lands in cash: a return, not a flow.
	if _, err := tr.Create(ctx, a.ID, trades.CreateInput{Date: "2026-01-05", Side: "buy", Quantity: m("100"), Price: m("10")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddMovement(ctx, ins.ID, cash.MovementInput{Date: "2026-01-06", Type: "deposit", Amount: m("1000")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create(ctx, a.ID, trades.CreateInput{Date: "2026-01-07", Side: "buy", Quantity: m("100"), Price: m("11")}); err != nil {
		t.Fatal(err)
	}
	if _, err := dv.Record(ctx, a.ID, dividends.RecordInput{Kind: "dividend", RecordDate: "2026-01-07", PayDate: "2026-01-08",
		PerShare: m("0.25"), InstrumentID: ins.ID}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.Backfill(ctx, time.Time{}, time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC))
	if err != nil || n != 5 {
		t.Fatalf("backfill: %d %v", n, err)
	}
	r, err := svc.Report(ctx, "2026-01-01", "2026-01-08")
	if err != nil {
		t.Fatal(err)
	}
	seg := map[string]Segment{}
	for _, s := range r.Segments {
		seg[s.Segment] = s
	}
	// Stock: a buy is a flow valued at the close (200 x 11.55 - 100 x 11 is
	// the old holding's move): 1.10 x (2310-1155)/1100 = 1.155 -> +15.5%.
	near(t, "WEGE3 twr", seg["asset:1"].TWR, "0.155")
	near(t, "core twr", seg["core"].TWR, "0.155")
	near(t, "br twr", seg["br"].TWR, "0.155")
	// Every buy is a flow of its own position, first one included.
	near(t, "WEGE3 flows", seg["asset:1"].NetFlows, "2155")
	// Total: d2 (2100-1000)/1000 = 1.1; d3 (3310-1155)/2100 = 1.02619;
	// d4 (3360)/3310 = 1.015106 (the dividend is return). 1.1x1.02619x1.015106.
	near(t, "total twr", seg["total"].TWR, "0.145861")
	near(t, "total flows", seg["total"].NetFlows, "3155")
	if seg["total"].Label != "Total" || seg["asset:1"].Label != "WEGE3" || r.Segments[0].Segment != "total" {
		t.Fatalf("segments = %+v", r.Segments)
	}
	if len(r.Series) != 4 || r.Series[0].Date != "2026-01-05" {
		t.Fatalf("series = %+v", r.Series)
	}
	// Re-running a day replaces it.
	if err := svc.Snapshot(ctx, "2026-01-08"); err != nil {
		t.Fatal(err)
	}
	if r2, _ := svc.Report(ctx, "2026-01-01", "2026-01-08"); len(r2.Series) != 4 {
		t.Fatalf("idempotent: %d", len(r2.Series))
	}
}

// Issue #50: trades recorded without cash movements (capital that was never
// in the portfolio) are external flows, and the gap between the execution
// price and the close must not become performance.
func TestTradeWithoutCashIsNotAReturn(t *testing.T) {
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return time.Date(2026, 1, 12, 12, 0, 0, 0, time.UTC) }
	as := assets.NewService(conn)
	tr := trades.NewService(conn, as)
	cs := cash.NewService(conn, cash.NewRateStore(conn), clock)
	svc := NewService(conn, as, tr, cs, clock)

	a, _ := as.Create(ctx, assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	b, _ := as.Create(ctx, assets.CreateInput{Ticker: "ITUB4", Market: "B3", Kind: "stock"})
	ins, _ := cs.Create(ctx, cash.CreateInput{Kind: "cdi", Name: "CDB"})
	prices := [][3]any{
		{a.ID, "2026-01-05", "10"}, {a.ID, "2026-01-06", "10"}, {a.ID, "2026-01-07", "10"}, {a.ID, "2026-01-08", "10"},
		{b.ID, "2026-01-07", "30"}, {b.ID, "2026-01-08", "30"}, {b.ID, "2026-01-09", "33"},
	}
	for _, p := range prices {
		if _, err := conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (?, ?, 'cotahist', ?, 'x')`, p[0], p[1], p[2]); err != nil {
			t.Fatal(err)
		}
	}
	// A R$1,000 deposit, then a WEGE3 buy at 8 (closes 10) with no cash
	// movement, then an ITUB4 buy at 20 (closes 30) funded by a matching
	// R$2,000 cash withdrawal on the same day.
	if _, err := cs.AddMovement(ctx, ins.ID, cash.MovementInput{Date: "2026-01-05", Type: "deposit", Amount: m("1000")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create(ctx, a.ID, trades.CreateInput{Date: "2026-01-06", Side: "buy", Quantity: m("100"), Price: m("8")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddMovement(ctx, ins.ID, cash.MovementInput{Date: "2026-01-06", Type: "deposit", Amount: m("2000")}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create(ctx, b.ID, trades.CreateInput{Date: "2026-01-07", Side: "buy", Quantity: m("100"), Price: m("20")}); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.AddMovement(ctx, ins.ID, cash.MovementInput{Date: "2026-01-07", Type: "withdraw", Amount: m("2000")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Backfill(ctx, time.Time{}, time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Report(ctx, "2026-01-01", "2026-01-09")
	if err != nil {
		t.Fatal(err)
	}
	seg := map[string]Segment{}
	for _, s := range r.Segments {
		seg[s.Segment] = s
	}
	// WEGE3 sits at 10 all week: no return, the buy is its flow.
	near(t, "WEGE3 twr", seg["asset:1"].TWR, "0")
	near(t, "WEGE3 flows", seg["asset:1"].NetFlows, "1000")
	// ITUB4 closes 30 then 33: only the last day is return (+10%).
	near(t, "ITUB4 twr", seg["asset:2"].TWR, "0.1")
	near(t, "ITUB4 flows", seg["asset:2"].NetFlows, "3000")
	// Total never moves on the trade days: only the day-9 ITUB4 gain (300 on
	// 5000 held) is return.
	for _, p := range r.Series {
		if p.Date < "2026-01-09" {
			near(t, "total index "+p.Date, p.Total, "1")
		}
	}
	near(t, "total twr", seg["total"].TWR, "0.06")
}
