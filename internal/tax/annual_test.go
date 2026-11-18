package tax

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

func m(s string) money.Decimal   { return money.New(decimal.RequireFromString(s)) }
func mp(s string) *money.Decimal { v := m(s); return &v }
func eq(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}

func TestAnnualReport(t *testing.T) {
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	tr := trades.NewService(conn, as)
	cs := cash.NewService(conn, cash.NewRateStore(conn), nil)
	dv := dividends.NewService(conn, as, tr, cs, nil)
	svc := NewService(as, tr, dv, nil)

	msft, _ := as.Create(ctx, assets.CreateInput{Ticker: "MSFT", Market: "US", Kind: "stock"})
	wege, _ := as.Create(ctx, assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	usd, _ := cs.Create(ctx, cash.CreateInput{Kind: "usd_cash", Name: "Avenue"})
	cdi, _ := cs.Create(ctx, cash.CreateInput{Kind: "cdi", Name: "CDB"})
	for _, x := range []struct {
		id                    int64
		date, side, q, px, fx string
	}{
		{msft.ID, "2025-03-01", "buy", "10", "400", "5"},   // cost R$20,000
		{msft.ID, "2025-09-01", "sell", "5", "300", "5.2"}, // 7,800 - 10,000 = -2,200 BRL
		{msft.ID, "2026-04-01", "sell", "3", "500", "5.5"}, // 8,250 - 6,000 = +2,250 BRL
		{wege.ID, "2026-02-10", "buy", "100", "40", "1"},
	} {
		in := trades.CreateInput{Date: x.date, Side: x.side, Quantity: m(x.q), Price: m(x.px)}
		if x.fx != "1" {
			in.FXRate = mp(x.fx)
		}
		if _, err := tr.Create(ctx, x.id, in); err != nil {
			t.Fatal(err)
		}
	}
	// US dividend: 2 x 1.00 = 2 USD gross at 5.5 = 11 BRL, 30% withheld = 3.30 BRL.
	if _, err := dv.Record(ctx, msft.ID, dividends.RecordInput{Kind: "dividend", RecordDate: "2026-05-14", PayDate: "2026-06-11",
		PerShare: m("1"), FXRate: mp("5.5"), InstrumentID: usd.ID}); err != nil {
		t.Fatal(err)
	}
	// B3 JCP: 100 x 0.40 = 40 gross, 7 withheld.
	if _, err := dv.Record(ctx, wege.ID, dividends.RecordInput{Kind: "jcp", RecordDate: "2026-08-01", PayDate: "2026-08-20",
		PerShare: m("0.4"), InstrumentID: cdi.ID}); err != nil {
		t.Fatal(err)
	}

	r25, err := svc.Annual(ctx, 2025)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "2025 gains", r25.Foreign.Gains, "-2200")
	eq(t, "2025 loss out", r25.Foreign.LossOut, "2200")
	eq(t, "2025 tax", r25.Foreign.Tax, "0")
	if len(r25.Positions) != 1 || r25.Positions[0].Ticker != "MSFT" {
		t.Fatalf("2025 positions = %+v", r25.Positions)
	}
	eq(t, "2025 MSFT qty", r25.Positions[0].Quantity, "5")
	eq(t, "2025 MSFT cost BRL", r25.Positions[0].CostBRL, "10000")

	r26, err := svc.Annual(ctx, 2026)
	if err != nil {
		t.Fatal(err)
	}
	// 2,250 gains + 11 dividends - 2,200 carried = 61 x 15% = 9.15.
	eq(t, "2026 gains", r26.Foreign.Gains, "2250")
	eq(t, "2026 dividends", r26.Foreign.Dividends, "11")
	eq(t, "2026 withheld", r26.Foreign.Withheld, "3.3")
	eq(t, "2026 loss in", r26.Foreign.LossIn, "2200")
	eq(t, "2026 taxable", r26.Foreign.Taxable, "61")
	eq(t, "2026 tax", r26.Foreign.Tax, "9.15")
	if len(r26.Sales) != 1 || len(r26.Income) != 2 || len(r26.Positions) != 2 {
		t.Fatalf("2026 report = %+v", r26)
	}
	eq(t, "jcp gross", r26.B3JCPGross, "40")
	eq(t, "jcp withheld", r26.B3JCPWithheld, "7")
	eq(t, "MSFT cost at year end", r26.Positions[0].CostBRL, "4000")
}
