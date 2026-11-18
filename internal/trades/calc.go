package trades

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Sale is the realized result of one sell trade.
type Sale struct {
	TradeID int64  `json:"trade_id"`
	Date    string `json:"date"`
	// Quantity sold.
	Quantity decimal.Decimal `json:"quantity"`
	// Proceeds are net of fees, in trade currency.
	Proceeds decimal.Decimal `json:"proceeds"`
	// CostBasis is the average cost of the shares sold, in trade currency.
	CostBasis decimal.Decimal `json:"cost_basis"`
	// Gain is Proceeds - CostBasis in trade currency.
	Gain decimal.Decimal `json:"gain"`
	// GainBRL is realized gain in BRL: proceeds at the sale-date FX minus the
	// BRL cost basis (cost at the FX of each purchase).
	GainBRL decimal.Decimal `json:"gain_brl"`
	// DayTradeQuantity is the part of Quantity matched against shares bought
	// the same day (costed at those buys, not the average cost). The totals
	// above cover both parts; the day-trade share of them is below.
	DayTradeQuantity decimal.Decimal `json:"day_trade_quantity"`
	DayTradeProceeds decimal.Decimal `json:"day_trade_proceeds"`
	DayTradeGainBRL  decimal.Decimal `json:"day_trade_gain_brl"`
}

// Position is the state derived from replaying an asset's trades.
type Position struct {
	AssetID  int64           `json:"asset_id"`
	Currency string          `json:"currency"`
	Quantity decimal.Decimal `json:"quantity"`
	// TotalCost is the cost of the remaining shares, fees included, in trade currency.
	TotalCost decimal.Decimal `json:"total_cost"`
	// AvgCost is TotalCost / Quantity (zero when flat).
	AvgCost      decimal.Decimal `json:"avg_cost"`
	TotalCostBRL decimal.Decimal `json:"total_cost_brl"`
	AvgCostBRL   decimal.Decimal `json:"avg_cost_brl"`
	// RealizedGain is the sum of sale gains in trade currency; BRL variant below.
	RealizedGain    decimal.Decimal `json:"realized_gain"`
	RealizedGainBRL decimal.Decimal `json:"realized_gain_brl"`
	Sales           []Sale          `json:"sales"`
}

const avgScale = 8

// Replay derives a position from trades already sorted by (date, id).
// Average cost follows Receita rules: purchase fees are part of cost, sales
// do not change the average cost. Selling more than held returns
// ErrInsufficientQuantity.
func Replay(assetID int64, ts []Trade) (Position, error) {
	return ReplayWithEvents(assetID, ts, nil)
}

// Event is a confirmed corporate action (spec §11.1). Factor multiplies the
// shares held before Date; total cost is unchanged except for bonus shares,
// which enter at CostPerShare (asset currency, BRL via FXRate).
type Event struct {
	Date         string
	Kind         string // split, reverse_split, bonus
	Factor       decimal.Decimal
	CostPerShare decimal.Decimal
	FXRate       decimal.Decimal // BRL per asset currency unit; zero means 1
}

// ReplayWithEvents is Replay with corporate actions, sorted by date. An event
// applies before the trades of its date (Date is the ex-date).
func ReplayWithEvents(assetID int64, ts []Trade, events []Event) (Position, error) {
	p := Position{AssetID: assetID, Sales: []Sale{}}
	zero := decimal.Zero
	qty, cost, costBRL := zero, zero, zero
	ei := 0
	apply := func(before string) {
		for ; ei < len(events) && (before == "" || events[ei].Date <= before); ei++ {
			e := events[ei]
			if !e.Factor.IsPositive() {
				continue
			}
			newQty := qty.Mul(e.Factor)
			if e.Kind == "bonus" {
				added := newQty.Sub(qty)
				fx := e.FXRate
				if fx.IsZero() {
					fx = decimal.NewFromInt(1)
				}
				cost = cost.Add(added.Mul(e.CostPerShare))
				costBRL = costBRL.Add(added.Mul(e.CostPerShare).Mul(fx))
			}
			qty = newQty
		}
	}
	for i := 0; i < len(ts); {
		j := i
		for j < len(ts) && ts[j].Date == ts[i].Date {
			j++
		}
		day := ts[i:j]
		i = j
		apply(day[0].Date)
		p.Currency = day[len(day)-1].Currency

		// Same-day matching: shares bought and sold the same day are a day
		// trade, costed at the day's weighted buy price (fees included). Only
		// the unmatched part of the day's buys enters the position.
		buyQty, buyCost, buyCostBRL, sellQty := zero, zero, zero, zero
		for _, t := range day {
			switch t.Side {
			case SideBuy:
				c := t.Quantity.Mul(t.Price).Add(t.Fees)
				buyQty = buyQty.Add(t.Quantity)
				buyCost = buyCost.Add(c)
				buyCostBRL = buyCostBRL.Add(c.Mul(t.FXRate))
			case SideSell:
				sellQty = sellQty.Add(t.Quantity)
			default:
				return Position{}, validationf("unknown side %q", t.Side)
			}
		}
		dayLeft := decimal.Min(buyQty, sellQty)
		if buyQty.IsPositive() {
			keep := buyQty.Sub(dayLeft)
			qty = qty.Add(keep)
			cost = cost.Add(buyCost.Mul(keep).Div(buyQty))
			costBRL = costBRL.Add(buyCostBRL.Mul(keep).Div(buyQty))
		}
		for _, t := range day {
			if t.Side != SideSell {
				continue
			}
			dq := decimal.Min(t.Quantity, dayLeft)
			dayLeft = dayLeft.Sub(dq)
			swingQty := t.Quantity.Sub(dq)
			if swingQty.GreaterThan(qty) {
				return Position{}, fmt.Errorf("%w: selling %s on %s with %s held",
					ErrInsufficientQuantity, swingQty, t.Date, qty)
			}
			proceeds := t.Quantity.Mul(t.Price).Sub(t.Fees)
			dayProceeds, dayCost, dayCostBRL := zero, zero, zero
			if dq.IsPositive() {
				dayProceeds = proceeds.Mul(dq).Div(t.Quantity)
				dayCost = buyCost.Mul(dq).Div(buyQty)
				dayCostBRL = buyCostBRL.Mul(dq).Div(buyQty)
			}
			costOut, costOutBRL := zero, zero
			if swingQty.IsPositive() {
				costOut, costOutBRL = cost, costBRL
				if !swingQty.Equal(qty) {
					costOut = cost.Mul(swingQty).Div(qty)
					costOutBRL = costBRL.Mul(swingQty).Div(qty)
				}
			}
			s := Sale{
				TradeID: t.ID, Date: t.Date, Quantity: t.Quantity, Proceeds: proceeds,
				CostBasis: costOut.Add(dayCost).Round(avgScale),
				Gain:      proceeds.Sub(costOut).Sub(dayCost).Round(avgScale),
				GainBRL:   proceeds.Mul(t.FXRate).Sub(costOutBRL).Sub(dayCostBRL).Round(avgScale),
			}
			if dq.IsPositive() {
				s.DayTradeQuantity = dq
				s.DayTradeProceeds = dayProceeds
				s.DayTradeGainBRL = dayProceeds.Mul(t.FXRate).Sub(dayCostBRL).Round(avgScale)
			}
			p.Sales = append(p.Sales, s)
			p.RealizedGain = p.RealizedGain.Add(s.Gain)
			p.RealizedGainBRL = p.RealizedGainBRL.Add(s.GainBRL)
			qty = qty.Sub(swingQty)
			cost = cost.Sub(costOut)
			costBRL = costBRL.Sub(costOutBRL)
		}
	}
	apply("")
	p.Quantity = qty
	p.TotalCost = cost.Round(avgScale)
	p.TotalCostBRL = costBRL.Round(avgScale)
	if qty.IsPositive() {
		p.AvgCost = cost.DivRound(qty, avgScale)
		p.AvgCostBRL = costBRL.DivRound(qty, avgScale)
	}
	return p, nil
}
