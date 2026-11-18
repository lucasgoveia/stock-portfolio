package trades

import "testing"

func TestReplayDayTrade(t *testing.T) {
	for _, tc := range []struct {
		name   string
		trades []Trade
		// remaining position
		qty, cost string
		// the last sale: day-trade quantity, day-trade gain, total gain
		dayQty, dayGain, gain string
	}{
		{
			name: "pure day trade",
			trades: []Trade{
				tr("buy", "2025-03-03", "100", "10", "0", "1"),
				tr("sell", "2025-03-03", "100", "12", "0", "1"),
			},
			qty: "0", cost: "0", dayQty: "100", dayGain: "200", gain: "200",
		},
		{
			name: "day trade with fees",
			trades: []Trade{
				tr("buy", "2025-03-03", "10", "10", "2", "1"),
				tr("sell", "2025-03-03", "10", "12", "2", "1"),
			},
			qty: "0", cost: "0", dayQty: "10", dayGain: "16", gain: "16",
		},
		{
			name: "sell more than bought same day: rest is swing at average cost",
			trades: []Trade{
				tr("buy", "2025-03-01", "100", "10", "0", "1"),
				tr("buy", "2025-03-05", "50", "12", "0", "1"),
				tr("sell", "2025-03-05", "80", "14", "0", "1"),
			},
			// day 50 x (14-12) = 100; swing 30 x (14-10) = 120; 70 left at 10.
			qty: "70", cost: "700", dayQty: "50", dayGain: "100", gain: "220",
		},
		{
			name: "buy sell buy same day: leftover enters at the day average",
			trades: []Trade{
				tr("buy", "2025-03-03", "10", "10", "0", "1"),
				tr("sell", "2025-03-03", "10", "12", "0", "1"),
				tr("buy", "2025-03-03", "10", "11", "0", "1"),
			},
			// day buys average 10.50: gain 10 x 1.5 = 15; 10 left costing 105.
			qty: "10", cost: "105", dayQty: "10", dayGain: "15", gain: "15",
		},
		{
			name: "prior position and same-day round trip keep the average",
			trades: []Trade{
				tr("buy", "2025-03-01", "100", "10", "0", "1"),
				tr("buy", "2025-03-05", "50", "12", "0", "1"),
				tr("sell", "2025-03-05", "50", "13", "0", "1"),
			},
			qty: "100", cost: "1000", dayQty: "50", dayGain: "50", gain: "50",
		},
		{
			name: "no same-day buy is plain swing",
			trades: []Trade{
				tr("buy", "2025-03-01", "100", "10", "0", "1"),
				tr("sell", "2025-03-05", "40", "13", "0", "1"),
			},
			qty: "60", cost: "600", dayQty: "0", dayGain: "0", gain: "120",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Replay(1, tc.trades)
			if err != nil {
				t.Fatal(err)
			}
			s := p.Sales[len(p.Sales)-1]
			if !p.Quantity.Equal(d(tc.qty)) || !p.TotalCost.Equal(d(tc.cost)) ||
				!s.DayTradeQuantity.Equal(d(tc.dayQty)) || !s.DayTradeGainBRL.Equal(d(tc.dayGain)) || !s.GainBRL.Equal(d(tc.gain)) {
				t.Fatalf("got qty=%s cost=%s dayQty=%s dayGain=%s gain=%s", p.Quantity, p.TotalCost,
					s.DayTradeQuantity, s.DayTradeGainBRL, s.GainBRL)
			}
		})
	}
}
