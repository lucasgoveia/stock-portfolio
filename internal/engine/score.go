// Package engine holds pure, deterministic rule math (no I/O). See
// docs/spec.md section 3.
package engine

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// ErrInvalidInput is returned for out-of-range factors or weights.
var ErrInvalidInput = errors.New("engine: invalid input")

// Factors are the five conviction factors, each 1-5 (spec 5.1).
type Factors struct {
	Volatility     int
	Understanding  int
	Moat           int
	Predictability int
	Horizon        int
}

// Weights are the factor weights.
type Weights struct {
	Volatility     decimal.Decimal
	Understanding  decimal.Decimal
	Moat           decimal.Decimal
	Predictability decimal.Decimal
	Horizon        decimal.Decimal
}

// DefaultWeights returns 1 / 1 / 1.5 / 1.5 / 1.
func DefaultWeights() Weights {
	return Weights{
		Volatility:     decimal.NewFromInt(1),
		Understanding:  decimal.NewFromInt(1),
		Moat:           decimal.RequireFromString("1.5"),
		Predictability: decimal.RequireFromString("1.5"),
		Horizon:        decimal.NewFromInt(1),
	}
}

// ScorePrecision is the number of decimals the weighted score is rounded to.
const ScorePrecision = 4

// WeightedScore returns S = sum(w_i * r_i) / sum(w_i), in [1, 5], rounded to
// ScorePrecision decimals.
func WeightedScore(f Factors, w Weights) (decimal.Decimal, error) {
	rs := []int{f.Volatility, f.Understanding, f.Moat, f.Predictability, f.Horizon}
	ws := []decimal.Decimal{w.Volatility, w.Understanding, w.Moat, w.Predictability, w.Horizon}
	num, den := decimal.Zero, decimal.Zero
	for i, r := range rs {
		if r < 1 || r > 5 {
			return decimal.Zero, fmt.Errorf("%w: factor rating %d out of range 1-5", ErrInvalidInput, r)
		}
		if ws[i].IsNegative() {
			return decimal.Zero, fmt.Errorf("%w: negative weight", ErrInvalidInput)
		}
		num = num.Add(ws[i].Mul(decimal.NewFromInt(int64(r))))
		den = den.Add(ws[i])
	}
	if !den.IsPositive() {
		return decimal.Zero, fmt.Errorf("%w: weights sum to zero", ErrInvalidInput)
	}
	return num.DivRound(den, ScorePrecision), nil
}
