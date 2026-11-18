package trades

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func tr(side, date, qty, price, fees, fx string) Trade {
	cur := "BRL"
	if fx != "1" {
		cur = "USD"
	}
	return Trade{Side: side, Date: date, Quantity: d(qty), Price: d(price), Fees: d(fees), Currency: cur, FXRate: d(fx)}
}

func TestReplay(t *testing.T) {
	tests := []struct {
		name                          string
		trades                        []Trade
		qty, avg, cost, gain, gainBRL string
		avgBRL                        string
		sales                         int
		wantErr                       error
	}{
		{
			name:   "single buy includes fees",
			trades: []Trade{tr("buy", "2025-01-02", "100", "10", "5", "1")},
			qty:    "100", avg: "10.05", cost: "1005", gain: "0", gainBRL: "0", avgBRL: "10.05",
		},
		{
			name: "two buys weighted average",
			trades: []Trade{
				tr("buy", "2025-01-02", "100", "10", "5", "1"),
				tr("buy", "2025-02-02", "100", "12", "5", "1"),
			},
			qty: "200", avg: "11.05", cost: "2210", gain: "0", gainBRL: "0", avgBRL: "11.05",
		},
		{
			name: "partial sell keeps average cost, realizes gain net of fees",
			trades: []Trade{
				tr("buy", "2025-01-02", "100", "10", "5", "1"),
				tr("buy", "2025-02-02", "100", "12", "5", "1"),
				tr("sell", "2025-03-02", "50", "15", "5", "1"),
			},
			qty: "150", avg: "11.05", cost: "1657.5", gain: "192.5", gainBRL: "192.5", avgBRL: "11.05", sales: 1,
		},
		{
			name: "sell to zero realizes loss and resets",
			trades: []Trade{
				tr("buy", "2025-01-02", "100", "10", "5", "1"),
				tr("buy", "2025-02-02", "100", "12", "5", "1"),
				tr("sell", "2025-03-02", "50", "15", "5", "1"),
				tr("sell", "2025-04-02", "150", "10", "0", "1"),
			},
			qty: "0", avg: "0", cost: "0", gain: "35", gainBRL: "35", avgBRL: "0", sales: 2,
		},
		{
			name: "fractional quantity",
			trades: []Trade{
				tr("buy", "2025-01-02", "0.5", "200", "1", "1"),
				tr("sell", "2025-01-03", "0.25", "220", "0", "1"),
			},
			qty: "0.25", avg: "202", cost: "50.5", gain: "4.5", gainBRL: "4.5", avgBRL: "202", sales: 1,
		},
		{
			name: "us trades keep USD and BRL cost",
			trades: []Trade{
				tr("buy", "2025-01-02", "10", "100", "1", "5"),
				tr("sell", "2025-02-02", "5", "120", "1", "6"),
			},
			// USD: cost 1001, sell 5 -> costOut 500.5, proceeds 599, gain 98.5
			// BRL: cost 5005, costOut 2502.5, proceeds 3594, gain 1091.5
			qty: "5", avg: "100.1", cost: "500.5", gain: "98.5", gainBRL: "1091.5", avgBRL: "500.5", sales: 1,
		},
		{
			name: "us buys at different FX average the BRL cost",
			trades: []Trade{
				tr("buy", "2025-01-02", "10", "100", "0", "5"),
				tr("buy", "2025-02-02", "10", "100", "0", "6"),
			},
			qty: "20", avg: "100", cost: "2000", gain: "0", gainBRL: "0", avgBRL: "550",
		},
		{
			name: "oversell rejected",
			trades: []Trade{
				tr("buy", "2025-01-02", "10", "10", "0", "1"),
				tr("sell", "2025-01-03", "10.5", "10", "0", "1"),
			},
			wantErr: ErrInsufficientQuantity,
		},
		{
			name:    "sell before any buy rejected",
			trades:  []Trade{tr("sell", "2025-01-03", "1", "10", "0", "1")},
			wantErr: ErrInsufficientQuantity,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Replay(1, tc.trades)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			eq := func(label string, got decimal.Decimal, want string) {
				t.Helper()
				if !got.Equal(d(want)) {
					t.Errorf("%s = %s, want %s", label, got, want)
				}
			}
			eq("qty", p.Quantity, tc.qty)
			eq("avg", p.AvgCost, tc.avg)
			eq("cost", p.TotalCost, tc.cost)
			eq("gain", p.RealizedGain, tc.gain)
			eq("gainBRL", p.RealizedGainBRL, tc.gainBRL)
			eq("avgBRL", p.AvgCostBRL, tc.avgBRL)
			if len(p.Sales) != tc.sales {
				t.Errorf("sales = %d, want %d", len(p.Sales), tc.sales)
			}
		})
	}
}

func newSvc(t *testing.T) (*Service, *assets.Service) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	a := assets.NewService(conn)
	return NewService(conn, a), a
}

func dec(s string) money.Decimal { return money.New(d(s)) }

func TestServiceStatusTransitions(t *testing.T) {
	ctx := context.Background()
	svc, as := newSvc(t)
	a, err := as.Create(ctx, assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	if err != nil {
		t.Fatal(err)
	}
	status := func() string {
		got, _ := as.Get(ctx, a.ID)
		return got.Status
	}
	buy := func(date, q string) CreateInput {
		return CreateInput{Date: date, Side: "buy", Quantity: dec(q), Price: dec("10")}
	}
	if _, err := svc.Create(ctx, a.ID, buy("2025-01-02", "10")); err != nil {
		t.Fatal(err)
	}
	if status() != assets.StatusHolding {
		t.Fatalf("after first buy: %s", status())
	}
	if _, err := svc.Create(ctx, a.ID, CreateInput{Date: "2025-01-03", Side: "sell", Quantity: dec("11"), Price: dec("10")}); !errors.Is(err, ErrInsufficientQuantity) {
		t.Fatalf("oversell err = %v", err)
	}
	if ts, _ := svc.List(ctx, a.ID); len(ts) != 1 {
		t.Fatalf("oversell must roll back, have %d trades", len(ts))
	}
	// Back-dated sell that would leave a later sell invalid is rejected.
	if _, err := svc.Create(ctx, a.ID, CreateInput{Date: "2025-01-05", Side: "sell", Quantity: dec("10"), Price: dec("11")}); err != nil {
		t.Fatal(err)
	}
	if status() != assets.StatusExited {
		t.Fatalf("after sell to zero: %s", status())
	}
	// Buying again brings an exited asset back to holding.
	if _, err := svc.Create(ctx, a.ID, buy("2025-02-01", "5")); err != nil {
		t.Fatal(err)
	}
	if status() != assets.StatusHolding {
		t.Fatalf("after rebuy: %s", status())
	}
	p, err := svc.Position(ctx, a.ID)
	if err != nil || !p.Quantity.Equal(d("5")) || !p.RealizedGain.Equal(d("10")) {
		t.Fatalf("position: %+v %v", p, err)
	}
	all, err := svc.Positions(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("positions: %v %v", all, err)
	}
}

func TestServiceValidation(t *testing.T) {
	ctx := context.Background()
	svc, as := newSvc(t)
	br, _ := as.Create(ctx, assets.CreateInput{Ticker: "ITUB4", Market: "B3", Kind: "stock"})
	us, _ := as.Create(ctx, assets.CreateInput{Ticker: "GOOGL", Market: "US", Kind: "stock"})
	fx := dec("5.5")
	neg := dec("-1")
	tests := []struct {
		name  string
		asset int64
		in    CreateInput
	}{
		{"bad date", br.ID, CreateInput{Date: "02/01/2025", Side: "buy", Quantity: dec("1"), Price: dec("1")}},
		{"bad side", br.ID, CreateInput{Date: "2025-01-02", Side: "hold", Quantity: dec("1"), Price: dec("1")}},
		{"zero qty", br.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("0"), Price: dec("1")}},
		{"zero price", br.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("0")}},
		{"negative fees", br.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("1"), Fees: &neg}},
		{"wrong currency", br.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("1"), Currency: "USD"}},
		{"usd without fx", us.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("1")}},
		{"brl with fx", br.ID, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("1"), FXRate: &fx}},
	}
	for _, tc := range tests {
		if _, err := svc.Create(ctx, tc.asset, tc.in); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	if _, err := svc.Create(ctx, 99, CreateInput{Date: "2025-01-02", Side: "buy", Quantity: dec("1"), Price: dec("1")}); !errors.Is(err, assets.ErrNotFound) {
		t.Errorf("missing asset: %v", err)
	}
}
