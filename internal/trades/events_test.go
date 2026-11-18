package trades

import (
	"errors"
	"testing"
)

func TestReplayWithEvents(t *testing.T) {
	buy := tr("buy", "2026-01-10", "100", "30", "0", "1")
	for _, c := range []struct {
		name            string
		trades          []Trade
		events          []Event
		qty, cost, avg  string
		gain            string // realized gain of the last sale, if any
		insufficientErr bool
	}{
		{
			name:   "2-for-1 split halves the average cost",
			trades: []Trade{buy},
			events: []Event{{Date: "2026-03-01", Kind: "split", Factor: d("2")}},
			qty:    "200", cost: "3000", avg: "15",
		},
		{
			name:   "1-for-10 reverse split",
			trades: []Trade{buy},
			events: []Event{{Date: "2026-03-01", Kind: "reverse_split", Factor: d("0.1")}},
			qty:    "10", cost: "3000", avg: "300",
		},
		{
			name:   "10% bonificação at R$5 declared cost",
			trades: []Trade{buy},
			events: []Event{{Date: "2026-03-01", Kind: "bonus", Factor: d("1.1"), CostPerShare: d("5")}},
			// 10 new shares x 5 = 50 added to cost.
			qty: "110", cost: "3050", avg: "27.72727273",
		},
		{
			name:   "event on a trade date applies before that trade",
			trades: []Trade{buy, tr("sell", "2026-03-01", "150", "20", "0", "1")},
			events: []Event{{Date: "2026-03-01", Kind: "split", Factor: d("2")}},
			// 200 held at avg 15; sell 150 at 20 -> gain 750, 50 left at 750 cost.
			qty: "50", cost: "750", avg: "15", gain: "750",
		},
		{
			name:            "selling post-split shares without the event fails",
			trades:          []Trade{buy, tr("sell", "2026-03-01", "150", "20", "0", "1")},
			insufficientErr: true,
		},
		{
			name:   "event before any purchase changes nothing",
			trades: []Trade{buy},
			events: []Event{{Date: "2025-12-01", Kind: "split", Factor: d("2")}},
			qty:    "100", cost: "3000", avg: "30",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := ReplayWithEvents(1, c.trades, c.events)
			if c.insufficientErr {
				if !errors.Is(err, ErrInsufficientQuantity) {
					t.Fatalf("want insufficient quantity, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !p.Quantity.Equal(d(c.qty)) || !p.TotalCost.Equal(d(c.cost)) || !p.AvgCost.Equal(d(c.avg)) {
				t.Fatalf("got qty %s cost %s avg %s", p.Quantity, p.TotalCost, p.AvgCost)
			}
			if c.gain != "" && !p.Sales[len(p.Sales)-1].Gain.Equal(d(c.gain)) {
				t.Fatalf("gain %s", p.Sales[len(p.Sales)-1].Gain)
			}
		})
	}
}
