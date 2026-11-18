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
	// Stock: 1.10 x (2310-1100)/1100 = 1.21 -> +21%, flat on day 4.
	near(t, "WEGE3 twr", seg["asset:1"].TWR, "0.21")
	near(t, "core twr", seg["core"].TWR, "0.21")
	near(t, "br twr", seg["br"].TWR, "0.21")
	// Total: d2 (2100-1000)/1000 = 1.1; d3 (3310-1100)/2100 = 1.052381;
	// d4 (3360)/3310 = 1.015106 (the dividend is return). 1.1x1.052381x1.015106.
	near(t, "total twr", seg["total"].TWR, "0.175106")
	near(t, "total flows after day 1", seg["total"].NetFlows, "2100")
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
