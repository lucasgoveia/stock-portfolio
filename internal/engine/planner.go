package engine

import (
	"sort"

	"github.com/shopspring/decimal"
)

// Market of a planned asset.
type Market string

// Markets.
const (
	MarketB3 Market = "B3"
	MarketUS Market = "US"
)

// CashAssetID is the pseudo asset id used for cash orders.
const CashAssetID = "CASH"

// PlanAsset is one candidate stock for the contribution planner. All money is
// BRL; a US asset's Price must already be converted to BRL.
type PlanAsset struct {
	ID           string
	Market       Market
	Price        decimal.Decimal
	ActualValue  decimal.Decimal
	Target       decimal.Decimal // weight fraction from ComputeTargets
	Upper        decimal.Decimal // upper band weight (== Target in bootstrap)
	Eligible     bool            // passed the buy gate
	Opportunity  bool
	Discount     decimal.Decimal // (FV-price)/FV, for opportunity ordering
	CatalystOnly bool            // opportunity solely by catalyst
	RampStage    int             // tranches completed: 0..3 (3 = established)
	Tactical     bool            // never funded
}

// PlanInput feeds PlanContribution.
type PlanInput struct {
	// Amount is the new money to place: contribution plus undeployed cash
	// from dividends.
	Amount         decimal.Decimal
	PortfolioValue decimal.Decimal // before the contribution, BRL
	CashValue      decimal.Decimal // actual cash, BRL
	CashTarget     decimal.Decimal // fraction
	// UndeployedWeight is TargetResult.Undeployed; it is added to the cash
	// target so the money is routed to cash.
	UndeployedWeight decimal.Decimal
	// PendingUS is BRL already earmarked for US assets and held as cash.
	PendingUS decimal.Decimal
	Bootstrap bool // ramp-in is skipped in bootstrap (see PlanContribution)
	Assets    []PlanAsset
}

// OrderKind classifies a plan line.
type OrderKind string

// Order kinds.
const (
	OrderBuy   OrderKind = "buy"     // buy shares
	OrderCash  OrderKind = "cash"    // deposit into cash instruments
	OrderRemit OrderKind = "remit"   // BRL -> USD remittance
	OrderHold  OrderKind = "hold_us" // keep in the pending US bucket
)

// Order is one plan line, in execution order.
type Order struct {
	Seq      int
	Kind     OrderKind
	AssetID  string
	Market   Market
	Step     string // "opportunity" or "gap" for buys/cash
	Quantity decimal.Decimal
	Price    decimal.Decimal
	Amount   decimal.Decimal
	// RampStageAfter is the ramp stage once this order is executed. The
	// caller advances stored ramp state only after the trade is recorded.
	RampStageAfter int
}

// Skip documents why an asset got nothing.
type Skip struct {
	AssetID string
	Reason  string // "below_min_order", "below_one_share", "no_room"
}

// Plan is the planner output.
type Plan struct {
	Orders []Order
	// NewMoney is what this plan places out of Amount (orders plus held US
	// money). Always <= Amount. Remittance top-up from the existing pending
	// bucket is on top of it.
	NewMoney       decimal.Decimal
	Unallocated    decimal.Decimal // Amount - NewMoney, stays as cash
	PendingUSAfter decimal.Decimal
	Skipped        []Skip
}

type planState struct {
	a      PlanAsset
	qty    decimal.Decimal
	amt    decimal.Decimal
	step   string
	seq    int
	funded bool
	done   bool
}

// PlanContribution implements spec §8.3-§8.4.
//
// Sizing uses the post-contribution value V' = PortfolioValue + Amount.
// Steps: (1) opportunities sorted by discount (catalyst-only last), each up to
// its upper band; (2) repeatedly the largest relative gap (target-actual)/target
// among eligible stocks and cash, funded up to target. Ramp-in caps a position
// at tranche (stage+1)/3 of its target per plan; it is disabled in bootstrap
// (tranches of a tiny portfolio would sit below the minimum order). B3 orders
// are whole shares (floor) of at least MinOrderB3; skipped money rolls on. US
// money accumulates in the pending bucket and is released as a remittance plus
// fractional (4 dp) buys once bucket + new US allocation >= USBatchMin.
func (p Params) PlanContribution(in PlanInput) Plan {
	plan := Plan{PendingUSAfter: in.PendingUS, Unallocated: in.Amount, NewMoney: zero}
	if !in.Amount.IsPositive() {
		return plan
	}
	vNew := in.PortfolioValue.Add(in.Amount)
	remaining := in.Amount

	states := make([]*planState, 0, len(in.Assets))
	for _, a := range in.Assets {
		if a.Tactical || !a.Eligible || !a.Price.IsPositive() || !a.Target.IsPositive() {
			continue
		}
		states = append(states, &planState{a: a})
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].a.ID < states[j].a.ID })

	stages := decimal.NewFromInt(int64(p.RampStages))
	// rampLimit caps limit at the next ramp tranche (stage+1)/3 of the target.
	// Established positions and bootstrap are not ramped, so an opportunity can
	// still reach its upper band.
	rampLimit := func(a PlanAsset, limit decimal.Decimal) decimal.Decimal {
		if in.Bootstrap || a.RampStage >= p.RampStages {
			return limit
		}
		stage := decimal.NewFromInt(int64(a.RampStage + 1))
		return decimal.Min(limit, a.Target.Mul(vNew).Mul(stage).Div(stages))
	}
	seq := 0
	// fund tries to buy up to limit (BRL value cap) for one asset.
	fund := func(s *planState, limit decimal.Decimal, step string) {
		s.done = true
		room := limit.Sub(s.a.ActualValue).Sub(s.amt)
		if !room.IsPositive() {
			plan.Skipped = append(plan.Skipped, Skip{s.a.ID, "no_room"})
			return
		}
		amt := decimal.Min(room, remaining)
		var qty decimal.Decimal
		if s.a.Market == MarketUS {
			qty = amt.Div(s.a.Price).Truncate(4)
			amt = qty.Mul(s.a.Price).Truncate(2)
			if !qty.IsPositive() || !amt.IsPositive() {
				plan.Skipped = append(plan.Skipped, Skip{s.a.ID, "below_one_share"})
				return
			}
		} else {
			qty = amt.Div(s.a.Price).Floor()
			if !qty.IsPositive() {
				plan.Skipped = append(plan.Skipped, Skip{s.a.ID, "below_one_share"})
				return
			}
			amt = qty.Mul(s.a.Price).Round(2)
			if amt.LessThan(p.MinOrderB3) {
				plan.Skipped = append(plan.Skipped, Skip{s.a.ID, "below_min_order"})
				return
			}
		}
		if !s.funded {
			seq++
			s.seq, s.step, s.funded = seq, step, true
		}
		s.qty, s.amt = s.qty.Add(qty), s.amt.Add(amt)
		remaining = remaining.Sub(amt)
	}

	// Step 1: opportunities.
	var opps []*planState
	for _, s := range states {
		if s.a.Opportunity {
			opps = append(opps, s)
		}
	}
	sort.SliceStable(opps, func(i, j int) bool {
		a, b := opps[i].a, opps[j].a
		if a.CatalystOnly != b.CatalystOnly {
			return !a.CatalystOnly
		}
		if c := a.Discount.Cmp(b.Discount); c != 0 {
			return c > 0
		}
		return a.ID < b.ID
	})
	for _, s := range opps {
		if !remaining.IsPositive() {
			break
		}
		upper := s.a.Upper
		if upper.LessThan(s.a.Target) {
			upper = s.a.Target
		}
		fund(s, rampLimit(s.a, upper.Mul(vNew)), "opportunity")
	}

	for _, s := range states {
		if s.funded {
			s.done = false // may still be topped up in step 2
		}
	}

	// Step 2: largest relative gap among stocks and cash.
	cashTargetVal := in.CashTarget.Add(in.UndeployedWeight).Mul(vNew)
	cashAlloc, cashDone, cashSeq := zero, false, 0
	for remaining.IsPositive() {
		var best *planState
		bestGap, bestIsCash := zero, false
		consider := func(s *planState, isCash bool, gap decimal.Decimal) {
			if !gap.IsPositive() {
				return
			}
			if best == nil && !bestIsCash && bestGap.IsZero() {
				best, bestGap, bestIsCash = s, gap, isCash
				return
			}
			c := gap.Cmp(bestGap)
			if c > 0 || (c == 0 && bestIsCash && !isCash) {
				best, bestGap, bestIsCash = s, gap, isCash
			}
		}
		for _, s := range states {
			if s.done {
				continue
			}
			tv := s.a.Target.Mul(vNew)
			consider(s, false, tv.Sub(s.a.ActualValue).Sub(s.amt).Div(tv))
		}
		if !cashDone && cashTargetVal.IsPositive() {
			consider(nil, true, cashTargetVal.Sub(in.CashValue).Sub(cashAlloc).Div(cashTargetVal))
		}
		if best == nil && !bestIsCash {
			break
		}
		if bestIsCash {
			cashDone = true
			room := cashTargetVal.Sub(in.CashValue).Sub(cashAlloc)
			amt := decimal.Min(room, remaining).Truncate(2)
			if amt.IsPositive() {
				seq++
				cashSeq = seq
				cashAlloc = cashAlloc.Add(amt)
				remaining = remaining.Sub(amt)
			}
			continue
		}
		fund(best, rampLimit(best.a, best.a.Target.Mul(vNew)), "gap")
	}

	// Assemble orders in funding sequence (B3 buys and cash), then US.
	type line struct {
		seq int
		o   Order
	}
	var lines []line
	var us []*planState
	usAlloc := zero
	for _, s := range states {
		if !s.funded {
			continue
		}
		if s.a.Market == MarketUS {
			us = append(us, s)
			usAlloc = usAlloc.Add(s.amt)
			continue
		}
		lines = append(lines, line{s.seq, p.buyOrder(s, s.amt, s.qty, in.Bootstrap)})
	}
	if cashSeq > 0 {
		lines = append(lines, line{cashSeq, Order{Kind: OrderCash, AssetID: CashAssetID, Step: "gap", Amount: cashAlloc}})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].seq < lines[j].seq })
	for _, l := range lines {
		plan.Orders = append(plan.Orders, l.o)
	}
	sort.SliceStable(us, func(i, j int) bool { return us[i].seq < us[j].seq })
	if usAlloc.IsPositive() {
		total := in.PendingUS.Add(usAlloc)
		if total.GreaterThanOrEqual(p.USBatchMin) {
			plan.Orders = append(plan.Orders, Order{Kind: OrderRemit, Market: MarketUS, Amount: total})
			for _, s := range us {
				amt := s.amt.Add(in.PendingUS.Mul(s.amt).Div(usAlloc)).Truncate(2)
				qty := amt.Div(s.a.Price).Truncate(4)
				plan.Orders = append(plan.Orders, p.buyOrder(s, qty.Mul(s.a.Price).Truncate(2), qty, in.Bootstrap))
			}
			plan.PendingUSAfter = zero
		} else {
			plan.Orders = append(plan.Orders, Order{Kind: OrderHold, Market: MarketUS, Amount: usAlloc})
			plan.PendingUSAfter = total
		}
	}
	for i := range plan.Orders {
		plan.Orders[i].Seq = i + 1
	}
	plan.NewMoney = in.Amount.Sub(remaining)
	plan.Unallocated = remaining
	return plan
}

func (p Params) buyOrder(s *planState, amt, qty decimal.Decimal, bootstrap bool) Order {
	after := s.a.RampStage
	switch {
	case bootstrap:
		after = p.RampStages // built in one go: no ramp in bootstrap
	case after < p.RampStages:
		after++
	}
	return Order{
		Kind: OrderBuy, AssetID: s.a.ID, Market: s.a.Market, Step: s.step,
		Quantity: qty, Price: s.a.Price, Amount: amt, RampStageAfter: after,
	}
}
