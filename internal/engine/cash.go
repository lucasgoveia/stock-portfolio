package engine

import "github.com/shopspring/decimal"

// CoreWeight is a core holding for the overvalued-share computation.
type CoreWeight struct {
	ID        string
	Weight    decimal.Decimal // actual weight
	Price     decimal.Decimal
	FairValue *decimal.Decimal // nil when missing
}

// Overvalued reports price > fair value x 1.3. Missing fair value is never
// overvalued.
func (p Params) Overvalued(price decimal.Decimal, fv *decimal.Decimal) bool {
	return fv != nil && price.GreaterThan(fv.Mul(p.OvervaluedMult))
}

// CashResult is the dynamic cash target (spec §7).
type CashResult struct {
	OvervaluedShare  decimal.Decimal `json:"overvalued_share"`
	Target           decimal.Decimal `json:"target"`
	MissingFairValue []string        `json:"missing_fair_value"` // holdings to raise "missing_fair_value" for
}

// CashTarget = 4% + 16% x overvalued_share, where overvalued_share is the sum
// of core weights with price > FV x 1.3 over the sum of core weights.
func (p Params) CashTarget(core []CoreWeight) CashResult {
	total, over := zero, zero
	var missing []string
	for _, c := range core {
		total = total.Add(c.Weight)
		if c.FairValue == nil {
			missing = append(missing, c.ID)
			continue
		}
		if p.Overvalued(c.Price, c.FairValue) {
			over = over.Add(c.Weight)
		}
	}
	share := zero
	if total.IsPositive() {
		share = over.Div(total)
	}
	return CashResult{
		OvervaluedShare:  share,
		Target:           p.CashBase.Add(p.CashSpan.Mul(share)),
		MissingFairValue: missing,
	}
}

// CashSignal is the cash band alert state.
type CashSignal int

const (
	CashOK     CashSignal = iota
	CashRaise             // < 4%: raise cash + trim plan
	CashDeploy            // > 20%: deploy cash via planner
)

// CashBand evaluates actual cash weight against the 4-20% band. Always
// CashOK in bootstrap.
func (p Params) CashBand(actual decimal.Decimal, bootstrap bool) CashSignal {
	switch {
	case bootstrap:
		return CashOK
	case actual.LessThan(p.CashBandMin):
		return CashRaise
	case actual.GreaterThan(p.CashBandMax):
		return CashDeploy
	}
	return CashOK
}
