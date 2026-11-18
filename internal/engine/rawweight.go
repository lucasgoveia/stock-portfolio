package engine

import "github.com/shopspring/decimal"

// Score is the weighted score with the configured factor weights (spec §5.1).
func (p Params) Score(f Factors) (decimal.Decimal, error) {
	return WeightedScore(f, p.Weights)
}

// TacticalScore is the score without the horizon factor (spec §10.1). The
// horizon rating is ignored and may be unset.
func (p Params) TacticalScore(f Factors) (decimal.Decimal, error) {
	f.Horizon = 1
	w := p.Weights
	w.Horizon = decimal.Zero
	return WeightedScore(f, w)
}

// RawWeight is raw = 2% + (S-1) x 2% (score 1 -> 2%, score 5 -> 10%).
func (p Params) RawWeight(score decimal.Decimal) decimal.Decimal {
	return p.RawBase.Add(score.Sub(decimal.NewFromInt(1)).Mul(p.RawSlope))
}
