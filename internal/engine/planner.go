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
	Score        decimal.Decimal // weighted score, breaks ties between equal gaps
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

// Skip and note reason codes.
const (
	ReasonBelowMinOrder   = "below_min_order"
	ReasonBelowOneShare   = "below_one_share"
	ReasonNoRoom          = "no_room"
	ReasonMaxNewPositions = "max_new_positions"
	NoteNoEligible        = "no_eligible_assets"
	NoteTargetsReached    = "targets_reached"
	NotePendingUS         = "pending_us"
)

// Order is one plan line, in execution order.
type Order struct {
	Seq      int             `json:"seq"`
	Kind     OrderKind       `json:"kind"`
	AssetID  string          `json:"asset_id"`
	Market   Market          `json:"market"`
	Step     string          `json:"step"` // "opportunity" or "gap" for buys/cash
	Quantity decimal.Decimal `json:"quantity"`
	Price    decimal.Decimal `json:"price"`
	Amount   decimal.Decimal `json:"amount"`
	// Allocations splits a hold_us or remit order per US asset.
	Allocations []USAllocation `json:"allocations,omitempty"`
	// RampStageAfter is the ramp stage once this order is executed. The
	// caller advances stored ramp state only after the trade is recorded.
	RampStageAfter int `json:"ramp_stage_after"`
}

// USAllocation is BRL assigned to one asset inside a hold_us or remit order.
type USAllocation struct {
	AssetID string          `json:"asset_id"`
	Amount  decimal.Decimal `json:"amount"`
}

// Skip documents why an asset got nothing.
type Skip struct {
	AssetID string `json:"asset_id"`
	// Reason is below_min_order, below_one_share, no_room or max_new_positions.
	Reason string `json:"reason"`
	// Amount is the BRL the plan tried to place when the reason is a size
	// problem (zero otherwise).
	Amount decimal.Decimal `json:"amount"`
}

// PlanNote explains a part of the plan the orders do not show: where US money is
// held, or why part of the contribution is not allocated.
type PlanNote struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Amount  decimal.Decimal `json:"amount"`
}

// Plan is the planner output.
type Plan struct {
	Orders []Order `json:"orders"`
	// NewMoney is what this plan places out of Amount (orders plus held US
	// money). Always <= Amount. Remittance top-up from the existing pending
	// bucket is on top of it.
	NewMoney       decimal.Decimal `json:"new_money"`
	Unallocated    decimal.Decimal `json:"unallocated"` // Amount - NewMoney, stays as cash
	PendingUSAfter decimal.Decimal `json:"pending_us_after"`
	Skipped        []Skip          `json:"skipped"`
	// Notes say where US money is held and why any amount is unallocated.
	Notes []PlanNote `json:"notes"`
}

type planState struct {
	a      PlanAsset
	qty    decimal.Decimal
	amt    decimal.Decimal
	step   string
	seq    int
	funded bool
	done   bool
	fresh  bool // no position yet
}

// PlanContribution implements spec §8.3-§8.4.
//
// Sizing uses the post-contribution value V' = PortfolioValue + Amount.
// New positions (nothing held yet) are limited to MaxNewPositions per plan,
// chosen opportunities first (by discount, catalyst-only last) and then by
// score, so a small contribution fills one or two orders above the minimum
// instead of many tiny ones; a pick that cannot reach the minimum order is
// replaced by the next candidate. In bootstrap the targets of the names left
// out are shared among the names being built.
// Steps: (1) opportunities sorted by discount, each up to its upper band; (2)
// repeatedly the largest relative gap (target-actual)/target among eligible
// stocks and cash (ties: higher score), funded up to target. Ramp-in caps a
// position at tranche (stage+1)/3 of its target per plan; it is disabled in
// bootstrap (tranches of a tiny portfolio would sit below the minimum order).
// B3 orders are whole shares (floor) of at least MinOrderB3; skipped money
// rolls on. US money accumulates in the pending bucket and is released as a
// remittance plus fractional (4 dp) buys once bucket + new US allocation >=
// USBatchMin. Notes explain held US money and any unallocated amount.
func (p Params) PlanContribution(in PlanInput) Plan {
	empty := Plan{PendingUSAfter: in.PendingUS, Unallocated: in.Amount, NewMoney: zero, Skipped: []Skip{}, Notes: []PlanNote{}}
	if !in.Amount.IsPositive() {
		return empty
	}
	dropped := map[string]Skip{}
	var r planRun
	for {
		r = p.planOnce(in, dropped)
		if len(r.failedNew) == 0 || !r.spare {
			break
		}
		for _, f := range r.failedNew {
			dropped[f.AssetID] = f
		}
	}
	plan := r.plan
	for _, id := range sortedKeys(dropped) {
		plan.Skipped = append(plan.Skipped, dropped[id])
	}
	p.addNotes(&plan, in, r)
	return plan
}

func sortedKeys(m map[string]Skip) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type planRun struct {
	plan      Plan
	failedNew []Skip // new positions that could not be sized
	spare     bool   // candidates left over that could replace a failed pick
	fundable  int    // assets that could receive money
}

func (p Params) planOnce(in PlanInput, dropped map[string]Skip) planRun {
	plan := Plan{PendingUSAfter: in.PendingUS, Unallocated: in.Amount, NewMoney: zero, Skipped: []Skip{}, Notes: []PlanNote{}}
	vNew := in.PortfolioValue.Add(in.Amount)
	remaining := in.Amount
	var run planRun

	states := make([]*planState, 0, len(in.Assets))
	totalT := zero // target mass of every fundable asset, before any is left out
	for _, a := range in.Assets {
		if a.Tactical || !a.Eligible || !a.Price.IsPositive() || !a.Target.IsPositive() {
			continue
		}
		totalT = totalT.Add(a.Target)
		if _, ok := dropped[a.ID]; ok {
			continue
		}
		states = append(states, &planState{a: a, fresh: a.ActualValue.IsZero()})
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].a.ID < states[j].a.ID })
	run.fundable = len(states) + len(dropped)

	// Limit new positions to the best MaxNewPositions candidates.
	var fresh []*planState
	for _, s := range states {
		if s.fresh {
			fresh = append(fresh, s)
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool { return freshBefore(fresh[i].a, fresh[j].a) })
	if p.MaxNewPositions > 0 && len(fresh) > p.MaxNewPositions {
		run.spare = true
		out := map[*planState]bool{}
		for _, s := range fresh[p.MaxNewPositions:] {
			out[s] = true
			plan.Skipped = append(plan.Skipped, Skip{AssetID: s.a.ID, Reason: ReasonMaxNewPositions, Amount: zero})
		}
		kept := states[:0:0]
		for _, s := range states {
			if !out[s] {
				kept = append(kept, s)
			}
		}
		states = kept
	}
	if in.Bootstrap && len(states) > 0 {
		// No bands in bootstrap: the names left out hand their target to the rest.
		sumKept := zero
		for _, s := range states {
			sumKept = sumKept.Add(s.a.Target)
		}
		if k := totalT.Div(sumKept); !k.Equal(one) {
			for _, s := range states {
				s.a.Target, s.a.Upper = s.a.Target.Mul(k), s.a.Upper.Mul(k)
			}
		}
	}

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
	skip := func(s *planState, reason string, amt decimal.Decimal) {
		sk := Skip{AssetID: s.a.ID, Reason: reason, Amount: amt}
		plan.Skipped = append(plan.Skipped, sk)
		if s.fresh && !s.funded {
			run.failedNew = append(run.failedNew, sk)
		}
	}
	// fund tries to buy up to limit (BRL value cap) for one asset.
	fund := func(s *planState, limit decimal.Decimal, step string) {
		s.done = true
		room := limit.Sub(s.a.ActualValue).Sub(s.amt)
		if !room.IsPositive() {
			skip(s, ReasonNoRoom, zero)
			return
		}
		amt := decimal.Min(room, remaining)
		var qty decimal.Decimal
		if s.a.Market == MarketUS {
			qty = amt.Div(s.a.Price).Truncate(4)
			amt = qty.Mul(s.a.Price).Truncate(2)
			if !qty.IsPositive() || !amt.IsPositive() {
				skip(s, ReasonBelowOneShare, amt)
				return
			}
		} else {
			qty = amt.Div(s.a.Price).Floor()
			if !qty.IsPositive() {
				skip(s, ReasonBelowOneShare, amt)
				return
			}
			amt = qty.Mul(s.a.Price).Round(2)
			if amt.LessThan(p.MinOrderB3) {
				skip(s, ReasonBelowMinOrder, amt)
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
	sort.SliceStable(opps, func(i, j int) bool { return freshBefore(opps[i].a, opps[j].a) })
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
			tie := c == 0 && ((bestIsCash && !isCash) || (!bestIsCash && !isCash && s.a.Score.GreaterThan(best.a.Score)))
			if c > 0 || tie {
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
			remit := Order{Kind: OrderRemit, Market: MarketUS, Amount: total}
			var buys []Order
			for _, s := range us {
				amt := s.amt.Add(in.PendingUS.Mul(s.amt).Div(usAlloc)).Truncate(2)
				qty := amt.Div(s.a.Price).Truncate(4)
				buys = append(buys, p.buyOrder(s, qty.Mul(s.a.Price).Truncate(2), qty, in.Bootstrap))
				remit.Allocations = append(remit.Allocations, USAllocation{AssetID: s.a.ID, Amount: amt})
			}
			plan.Orders = append(plan.Orders, remit)
			plan.Orders = append(plan.Orders, buys...)
			plan.PendingUSAfter = zero
		} else {
			hold := Order{Kind: OrderHold, Market: MarketUS, Amount: usAlloc}
			for _, s := range us {
				hold.Allocations = append(hold.Allocations, USAllocation{AssetID: s.a.ID, Amount: s.amt})
			}
			plan.Orders = append(plan.Orders, hold)
			plan.PendingUSAfter = total
		}
	}
	for i := range plan.Orders {
		plan.Orders[i].Seq = i + 1
	}
	plan.NewMoney = in.Amount.Sub(remaining)
	plan.Unallocated = remaining
	run.plan = plan
	return run
}

// freshBefore orders candidates: opportunities first (catalyst-only last, then
// deeper discount), then the rest by score.
func freshBefore(a, b PlanAsset) bool {
	if a.Opportunity != b.Opportunity {
		return a.Opportunity
	}
	if a.Opportunity {
		if a.CatalystOnly != b.CatalystOnly {
			return !a.CatalystOnly
		}
		if c := a.Discount.Cmp(b.Discount); c != 0 {
			return c > 0
		}
	}
	if c := a.Score.Cmp(b.Score); c != 0 {
		return c > 0
	}
	return a.ID < b.ID
}

func brl(d decimal.Decimal) string { return "R$ " + d.StringFixed(2) }

// addNotes explains held US money and any unallocated amount.
func (p Params) addNotes(plan *Plan, in PlanInput, r planRun) {
	for _, o := range plan.Orders {
		if o.Kind == OrderHold {
			plan.Notes = append(plan.Notes, PlanNote{Code: NotePendingUS, Amount: o.Amount, Message: "" +
				brl(o.Amount) + " for US assets is held in the pending US bucket (" + brl(plan.PendingUSAfter) +
				" after this plan; a remittance is proposed at " + brl(p.USBatchMin) + ")."})
		}
	}
	if !plan.Unallocated.IsPositive() {
		return
	}
	left := brl(plan.Unallocated)
	var sizeSkips, maxNew int
	for _, s := range plan.Skipped {
		switch s.Reason {
		case ReasonBelowMinOrder, ReasonBelowOneShare:
			sizeSkips++
		case ReasonMaxNewPositions:
			maxNew++
		}
	}
	add := func(code, msg string) {
		plan.Notes = append(plan.Notes, PlanNote{Code: code, Message: msg, Amount: plan.Unallocated})
	}
	switch r.fundable {
	case 0:
		add(NoteNoEligible, left+" stays in cash: no asset passes the buy gate with a target and a price.")
	default:
		if sizeSkips > 0 {
			add(ReasonBelowMinOrder, left+" stays in cash: "+itoa(sizeSkips)+" order(s) were below the minimum B3 order ("+brl(p.MinOrderB3)+") or one share and the money rolls over.")
		}
		if maxNew > 0 {
			add(ReasonMaxNewPositions, left+" stays in cash: at most "+itoa(p.MaxNewPositions)+" new position(s) are opened per contribution; "+itoa(maxNew)+" eligible name(s) wait for the next one.")
		}
		if sizeSkips == 0 && maxNew == 0 {
			add(NoteTargetsReached, left+" stays in cash: every eligible asset is at its target or its ramp tranche for this contribution.")
		}
	}
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

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
