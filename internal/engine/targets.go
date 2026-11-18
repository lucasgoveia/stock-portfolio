package engine

import (
	"sort"

	"github.com/shopspring/decimal"
)

var (
	one  = decimal.NewFromInt(1)
	zero = decimal.Zero
)

// IsBootstrap reports whether the portfolio is below the bootstrap threshold
// (spec §6.4): clamps, bands, floor/cap and cash band alerts are off.
func (p Params) IsBootstrap(totalValue decimal.Decimal) bool {
	return totalValue.LessThan(p.BootstrapBelow)
}

// BootstrapExit is true exactly when the value crosses the threshold upwards.
func (p Params) BootstrapExit(prev, cur decimal.Decimal) bool {
	return p.IsBootstrap(prev) && !p.IsBootstrap(cur)
}

// CoreHolding is a core stock that gets a target.
type CoreHolding struct {
	ID    string
	Score decimal.Decimal
}

// TargetInput feeds ComputeTargets.
type TargetInput struct {
	Holdings       []CoreHolding
	PortfolioValue decimal.Decimal // BRL, decides bootstrap
	CashTarget     decimal.Decimal // fraction, from CashTarget()
	TacticalWeight decimal.Decimal // sum of ACTUAL tactical weights
}

// Target is one core position's target and bands (fractions of portfolio).
// In bootstrap Lower is 0 and Upper equals Weight (no bands; the planner then
// funds up to the target).
type Target struct {
	ID      string
	Score   decimal.Decimal
	Raw     decimal.Decimal
	Weight  decimal.Decimal
	Lower   decimal.Decimal
	Upper   decimal.Decimal
	Clamped bool // frozen at 2% or 10%
}

// TargetResult is the output of ComputeTargets. Targets are sorted by ID.
type TargetResult struct {
	Bootstrap  bool
	CoreBudget decimal.Decimal
	// Undeployed is budget not placeable because every stock is at 10%
	// (too few stocks); the planner routes it to cash.
	Undeployed decimal.Decimal
	TooFew     bool // n x 10% < budget
	TooMany    bool // n x 2% > budget; all at 2%
	Targets    []Target
}

// ComputeTargets implements spec §6.1-§6.4.
func (p Params) ComputeTargets(in TargetInput) TargetResult {
	budget := one.Sub(in.CashTarget).Sub(in.TacticalWeight)
	if budget.IsNegative() {
		budget = zero
	}
	res := TargetResult{Bootstrap: p.IsBootstrap(in.PortfolioValue), CoreBudget: budget, Undeployed: zero}

	hs := append([]CoreHolding(nil), in.Holdings...)
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].ID < hs[j].ID })
	n := len(hs)
	if n == 0 {
		return res
	}
	ts := make([]Target, n)
	sumRaw := zero
	for i, h := range hs {
		raw := p.RawWeight(h.Score)
		ts[i] = Target{ID: h.ID, Score: h.Score, Raw: raw}
		sumRaw = sumRaw.Add(raw)
	}

	switch {
	case res.Bootstrap:
		for i := range ts {
			ts[i].Weight = ts[i].Raw.Mul(budget).Div(sumRaw)
		}
	default:
		nd := decimal.NewFromInt(int64(n))
		switch {
		case nd.Mul(p.ClampMax).LessThan(budget):
			res.TooFew = true
			res.Undeployed = budget.Sub(nd.Mul(p.ClampMax))
			for i := range ts {
				ts[i].Weight, ts[i].Clamped = p.ClampMax, true
			}
		case nd.Mul(p.ClampMin).GreaterThan(budget):
			res.TooMany = true
			for i := range ts {
				ts[i].Weight, ts[i].Clamped = p.ClampMin, true
			}
		default:
			p.clampRedistribute(ts, budget)
		}
	}

	for i := range ts {
		if res.Bootstrap {
			ts[i].Lower, ts[i].Upper = zero, ts[i].Weight
			continue
		}
		ts[i].Lower = ts[i].Weight.Mul(p.BandLowerMult)
		ts[i].Upper = decimal.Min(ts[i].Weight.Mul(p.BandUpperMult), p.BandUpperCap)
	}
	res.Targets = ts
	return res
}

// clampRedistribute scales raw weights to the budget, clamps to [min,max],
// freezes clamped positions and redistributes the remainder proportionally to
// raw until stable. When both bounds are violated in one pass, the side that
// would add weight (shortfall > excess) is frozen first so the budget stays
// feasible; this converges in at most n+1 passes.
func (p Params) clampRedistribute(ts []Target, budget decimal.Decimal) {
	free := make([]int, len(ts))
	for i := range ts {
		free[i] = i
	}
	remaining := budget
	for len(free) > 0 {
		sumRaw := zero
		for _, i := range free {
			sumRaw = sumRaw.Add(ts[i].Raw)
		}
		var lows, highs []int
		shortfall, excess := zero, zero
		for _, i := range free {
			t := ts[i].Raw.Mul(remaining).Div(sumRaw)
			ts[i].Weight = t
			switch {
			case t.LessThan(p.ClampMin):
				lows = append(lows, i)
				shortfall = shortfall.Add(p.ClampMin.Sub(t))
			case t.GreaterThan(p.ClampMax):
				highs = append(highs, i)
				excess = excess.Add(t.Sub(p.ClampMax))
			}
		}
		if len(lows) == 0 && len(highs) == 0 {
			return
		}
		freeze := map[int]decimal.Decimal{}
		switch c := shortfall.Cmp(excess); {
		case c > 0:
			for _, i := range lows {
				freeze[i] = p.ClampMin
			}
		case c < 0:
			for _, i := range highs {
				freeze[i] = p.ClampMax
			}
		default:
			for _, i := range lows {
				freeze[i] = p.ClampMin
			}
			for _, i := range highs {
				freeze[i] = p.ClampMax
			}
		}
		next := free[:0:0]
		for _, i := range free {
			if w, ok := freeze[i]; ok {
				ts[i].Weight, ts[i].Clamped = w, true
				remaining = remaining.Sub(w)
			} else {
				next = append(next, i)
			}
		}
		free = next
	}
}

// PositionStatus flags band violations of one actual weight.
type PositionStatus struct {
	BelowFloor bool // < 1.5%: "top up or exit"
	AboveCap   bool // > 12%: trim regardless of target
	BelowLower bool
	AboveUpper bool
}

// Status evaluates an actual weight against a target. Everything is false in
// bootstrap.
func (p Params) Status(actual decimal.Decimal, t Target, bootstrap bool) PositionStatus {
	if bootstrap {
		return PositionStatus{}
	}
	return PositionStatus{
		BelowFloor: actual.LessThan(p.HardFloor),
		AboveCap:   actual.GreaterThan(p.HardCap),
		BelowLower: actual.LessThan(t.Lower),
		AboveUpper: actual.GreaterThan(t.Upper),
	}
}
