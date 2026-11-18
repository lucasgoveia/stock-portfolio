package cash

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Movement types.
const (
	MoveDeposit     = "deposit"
	MoveWithdraw    = "withdraw"
	MoveYieldAdjust = "yield_adjust"
	// MoveBalance is a manual balance update: the balance becomes Amount.
	MoveBalance = "balance"
)

// Movement is one ledger entry of an instrument. Amount is in the
// instrument's currency; Quantity is title units (Tesouro Selic only).
// For yield_adjust, Amount is signed.
type Movement struct {
	ID         int64           `json:"id"`
	Instrument int64           `json:"instrument_id"`
	Date       string          `json:"date"`
	Type       string          `json:"type"`
	Amount     decimal.Decimal `json:"amount"`
	Quantity   decimal.Decimal `json:"quantity"`
}

// CDIRate is the daily CDI rate in percent per day for a reference date.
type CDIRate struct {
	Date string          `json:"date"`
	Pct  decimal.Decimal `json:"rate_pct"`
}

var hundred = decimal.NewFromInt(100)

func (m Movement) applyBalance(bal decimal.Decimal) decimal.Decimal {
	switch m.Type {
	case MoveDeposit, MoveYieldAdjust:
		return bal.Add(m.Amount)
	case MoveWithdraw:
		return bal.Sub(m.Amount)
	case MoveBalance:
		return m.Amount
	}
	return bal
}

// CDIBalance computes the balance of a CDI instrument at asof (YYYY-MM-DD).
//
// moves must be sorted by (date, id) and rates by date. The daily rate dated d
// accrues, at pctCDI percent of the CDI, on the balance held before d: a
// movement dated D starts earning with the rate of the first date after D.
// A "balance" movement replaces the balance (manual update) and accrual
// continues from it. Movements and rates after asof are ignored.
func CDIBalance(moves []Movement, rates []CDIRate, pctCDI decimal.Decimal, asof string) decimal.Decimal {
	bal := decimal.Zero
	i := 0
	for _, r := range rates {
		if r.Date > asof {
			break
		}
		for i < len(moves) && moves[i].Date < r.Date && moves[i].Date <= asof {
			bal = moves[i].applyBalance(bal)
			i++
		}
		factor := decimal.NewFromInt(1).Add(r.Pct.Div(hundred).Mul(pctCDI).Div(hundred))
		bal = bal.Mul(factor)
	}
	for ; i < len(moves) && moves[i].Date <= asof; i++ {
		bal = moves[i].applyBalance(bal)
	}
	return bal
}

// SimpleBalance sums a ledger without accrual (USD cash). It returns an
// error if the balance would go negative.
func SimpleBalance(moves []Movement, asof string) (decimal.Decimal, error) {
	bal := decimal.Zero
	for _, m := range moves {
		if m.Date > asof {
			break
		}
		bal = m.applyBalance(bal)
		if bal.IsNegative() {
			return bal, fmt.Errorf("%w: balance would be %s on %s", ErrInsufficientBalance, bal, m.Date)
		}
	}
	return bal, nil
}

// TesouroQuantity sums title units held at asof. It returns an error if the
// quantity would go negative.
func TesouroQuantity(moves []Movement, asof string) (decimal.Decimal, error) {
	qty := decimal.Zero
	for _, m := range moves {
		if m.Date > asof {
			break
		}
		switch m.Type {
		case MoveDeposit:
			qty = qty.Add(m.Quantity)
		case MoveWithdraw:
			qty = qty.Sub(m.Quantity)
		}
		if qty.IsNegative() {
			return qty, fmt.Errorf("%w: quantity would be %s on %s", ErrInsufficientBalance, qty, m.Date)
		}
	}
	return qty, nil
}
