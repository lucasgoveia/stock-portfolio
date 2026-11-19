package engine

import "github.com/shopspring/decimal"

// ShareEvent is a confirmed share-count corporate action: Factor multiplies
// the shares held before ExDate (2 for a 2-for-1 split, 0.1 for a 1-for-10
// reverse split, 1.1 for a 10% bonus).
type ShareEvent struct {
	ExDate string // YYYY-MM-DD
	Factor decimal.Decimal
}

// ShareFactorAfter is the cumulative factor that converts a holding counted on
// recordDate into the equivalent holding on today's scale: the product of the
// factors of events whose ex-date is after recordDate and not after asof.
//
// The boundary matches the trades replay: an event applies before the trades of
// its ex-date, so a holding "at the end of recordDate" already includes any
// event whose ex-date is on or before recordDate. Such events are ignored here
// (strictly after). Events with a non-positive factor are skipped. brapi's
// dividend rate is on the current scale, so gross = rate x shares_on_record x
// ShareFactorAfter.
func ShareFactorAfter(recordDate, asof string, events []ShareEvent) decimal.Decimal {
	f := decimal.NewFromInt(1)
	for _, e := range events {
		if e.ExDate > recordDate && e.ExDate <= asof && e.Factor.IsPositive() {
			f = f.Mul(e.Factor)
		}
	}
	return f
}
