package engine

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// TrimReason names the trigger behind a suggestion (spec §9.2).
type TrimReason string

// Trim reasons.
const (
	ReasonOverweight        TrimReason = "overweight"
	ReasonOvervalued        TrimReason = "overvalued"
	ReasonThesisBroken      TrimReason = "thesis_broken"
	ReasonRaiseCash         TrimReason = "raise_cash"
	ReasonBetterOpportunity TrimReason = "better_opportunity"
)

// TrimHolding is one open position.
type TrimHolding struct {
	ID           string
	Market       Market
	Tactical     bool
	Score        decimal.Decimal
	Quantity     decimal.Decimal
	Price        decimal.Decimal // BRL
	Target       Target          // ignored for tactical
	FairValue    *decimal.Decimal
	ThesisBroken bool
	LastSell     *time.Time // most recent sell of this asset
}

// Value is quantity x price.
func (h TrimHolding) Value() decimal.Decimal { return h.Quantity.Mul(h.Price) }

// BetterCandidate is a watchlist name that passed the buy gate.
type BetterCandidate struct {
	ID    string
	Score decimal.Decimal
	// Upside is (FV-price)/price as a fraction.
	Upside decimal.Decimal
	// FirstTranche is the BRL needed for its first ramp tranche.
	FirstTranche decimal.Decimal
}

// TrimInput feeds PlanTrims.
type TrimInput struct {
	Holdings       []TrimHolding
	PortfolioValue decimal.Decimal // total, BRL
	CashWeight     decimal.Decimal // actual
	CashTarget     decimal.Decimal // dynamic target
	Bootstrap      bool
	Candidates     []BetterCandidate
	Now            time.Time
}

// TrimSuggestion is a suggested sell.
type TrimSuggestion struct {
	AssetID  string          `json:"asset_id"`
	Reason   TrimReason      `json:"reason"`  // primary (largest amount)
	Reasons  []TrimReason    `json:"reasons"` // all triggers that fired
	Quantity decimal.Decimal `json:"quantity"`
	Price    decimal.Decimal `json:"price"`
	Amount   decimal.Decimal `json:"amount"`
	FullExit bool            `json:"full_exit"`
	// PairedCandidate is set for better_opportunity swaps.
	PairedCandidate string `json:"paired_candidate"`
}

// TrimSkip records a suppressed suggestion (cooldown).
type TrimSkip struct {
	AssetID string     `json:"asset_id"`
	Reason  TrimReason `json:"reason"`
	Until   time.Time  `json:"until"`
}

// TrimPlan is the output of PlanTrims.
type TrimPlan struct {
	Suggestions []TrimSuggestion `json:"suggestions"` // overweight / overvalued / thesis_broken
	RaiseCash   []TrimSuggestion `json:"raise_cash"`  // ordered: overweight, overvalued, lowest score
	// RaiseCashShortfall is the part of the needed cash the suggestions do
	// not cover.
	RaiseCashShortfall decimal.Decimal  `json:"raise_cash_shortfall"`
	Swaps              []TrimSuggestion `json:"swaps"` // better opportunity
	Skipped            []TrimSkip       `json:"skipped"`
}

func (p Params) inCooldown(h TrimHolding, now time.Time) (time.Time, bool) {
	if h.LastSell == nil {
		return time.Time{}, false
	}
	until := h.LastSell.AddDate(0, 0, p.CooldownDays)
	return until, now.Before(until)
}

// sellOf converts a BRL amount into a rounded-down quantity: whole shares on
// B3, 4 decimals for US. Returns false when it rounds to nothing. Amount is
// recomputed as quantity x price, and the sale is capped at the holding.
func sellOf(h TrimHolding, amount decimal.Decimal) (decimal.Decimal, decimal.Decimal, bool) {
	if !h.Price.IsPositive() {
		return zero, zero, false
	}
	qty := amount.Div(h.Price)
	if h.Market == MarketUS {
		qty = qty.Truncate(4)
	} else {
		qty = qty.Floor()
	}
	qty = decimal.Min(qty, h.Quantity)
	if !qty.IsPositive() {
		return zero, zero, false
	}
	return qty, qty.Mul(h.Price).Round(2), true
}

func fullExit(h TrimHolding, r TrimReason) TrimSuggestion {
	return TrimSuggestion{
		AssetID: h.ID, Reason: r, Reasons: []TrimReason{r}, Quantity: h.Quantity,
		Price: h.Price, Amount: h.Value().Round(2), FullExit: true,
	}
}

func (p Params) weightOf(h TrimHolding, v decimal.Decimal) decimal.Decimal {
	return h.Value().Div(v)
}

// overweightExcess returns the amount basis for an overweight trigger
// (weight above the upper band or hard cap): actual - target in BRL. For
// tactical positions only the 12% cap applies and the basis is actual - cap.
func (p Params) overweightExcess(h TrimHolding, v decimal.Decimal) (decimal.Decimal, bool) {
	w := p.weightOf(h, v)
	if h.Tactical {
		if w.GreaterThan(p.HardCap) {
			return w.Sub(p.HardCap).Mul(v), true
		}
		return zero, false
	}
	if w.GreaterThan(h.Target.Upper) || w.GreaterThan(p.HardCap) {
		return w.Sub(h.Target.Weight).Mul(v), true
	}
	return zero, false
}

// overvaluedExcess: price > FV x 1.3 on a core holding; basis actual - lower
// band in BRL.
func (p Params) overvaluedExcess(h TrimHolding, v decimal.Decimal) (decimal.Decimal, bool) {
	if h.Tactical || !p.Overvalued(h.Price, h.FairValue) {
		return zero, false
	}
	w := p.weightOf(h, v)
	if w.GreaterThan(h.Target.Lower) {
		return w.Sub(h.Target.Lower).Mul(v), true
	}
	return zero, false
}

// PlanTrims implements spec §9 and §10.2. In bootstrap bands do not exist, so
// only thesis-broken exits (and better-opportunity swaps) are produced. A
// thesis-broken exit ignores the cooldown; every other suggestion respects the
// 30-day cooldown from the last sell. Tactical holdings only trigger on the
// 12% cap (basis actual - cap) and on thesis_broken.
func (p Params) PlanTrims(in TrimInput) TrimPlan {
	var plan TrimPlan
	plan.RaiseCashShortfall = zero
	hs := append([]TrimHolding(nil), in.Holdings...)
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].ID < hs[j].ID })
	if !in.PortfolioValue.IsPositive() {
		return plan
	}
	v := in.PortfolioValue

	skipped := map[string]bool{}
	cool := func(h TrimHolding, r TrimReason) bool {
		until, ok := p.inCooldown(h, in.Now)
		if ok && !skipped[h.ID+string(r)] {
			skipped[h.ID+string(r)] = true
			plan.Skipped = append(plan.Skipped, TrimSkip{AssetID: h.ID, Reason: r, Until: until})
		}
		return ok
	}

	// Triggers.
	for _, h := range hs {
		if h.ThesisBroken {
			plan.Suggestions = append(plan.Suggestions, fullExit(h, ReasonThesisBroken))
			continue
		}
		if in.Bootstrap {
			continue
		}
		var reasons []TrimReason
		best := zero
		var primary TrimReason
		if ex, ok := p.overweightExcess(h, v); ok {
			reasons = append(reasons, ReasonOverweight)
			if amt := ex.Mul(p.TrimFraction); amt.GreaterThan(best) {
				best, primary = amt, ReasonOverweight
			}
		}
		if ex, ok := p.overvaluedExcess(h, v); ok {
			reasons = append(reasons, ReasonOvervalued)
			if amt := ex.Mul(p.TrimFraction); amt.GreaterThan(best) {
				best, primary = amt, ReasonOvervalued
			}
		}
		if len(reasons) == 0 {
			continue
		}
		if cool(h, primary) {
			continue
		}
		qty, amt, ok := sellOf(h, best)
		if !ok {
			continue
		}
		plan.Suggestions = append(plan.Suggestions, TrimSuggestion{
			AssetID: h.ID, Reason: primary, Reasons: reasons, Quantity: qty, Price: h.Price, Amount: amt,
		})
	}

	p.raiseCash(&plan, hs, in, cool)
	p.betterOpportunity(&plan, hs, in, cool)
	return plan
}

// raiseCash implements §9.3. Positions appear once, in tier order:
// overweight (up to the excess over target), overvalued (up to the excess over
// the lower band), then lowest score (one trim step of 25% of the position).
func (p Params) raiseCash(plan *TrimPlan, hs []TrimHolding, in TrimInput, cool func(TrimHolding, TrimReason) bool) {
	if p.CashBand(in.CashWeight, in.Bootstrap) != CashRaise {
		return
	}
	v := in.PortfolioValue
	need := in.CashTarget.Sub(in.CashWeight).Mul(v)
	if !need.IsPositive() {
		return
	}
	type cand struct {
		h   TrimHolding
		cap decimal.Decimal
		key decimal.Decimal // tier sort key, descending excess or ascending score
	}
	used := map[string]bool{}
	var t1, t2, t3 []cand
	for _, h := range hs {
		if h.ThesisBroken {
			continue
		}
		if ex, ok := p.overweightExcess(h, v); ok {
			t1 = append(t1, cand{h, ex, ex})
			continue
		}
		if h.Tactical {
			continue
		}
		if ex, ok := p.overvaluedExcess(h, v); ok {
			t2 = append(t2, cand{h, ex, ex})
			continue
		}
		t3 = append(t3, cand{h, h.Value().Mul(p.TrimFraction), h.Score})
	}
	byKeyDesc := func(c []cand) {
		sort.SliceStable(c, func(i, j int) bool {
			if k := c[i].key.Cmp(c[j].key); k != 0 {
				return k > 0
			}
			return c[i].h.ID < c[j].h.ID
		})
	}
	byKeyDesc(t1)
	byKeyDesc(t2)
	sort.SliceStable(t3, func(i, j int) bool {
		if k := t3[i].key.Cmp(t3[j].key); k != 0 {
			return k < 0
		}
		return t3[i].h.ID < t3[j].h.ID
	})
	remaining := need
	for _, tier := range [][]cand{t1, t2, t3} {
		for _, c := range tier {
			if !remaining.IsPositive() {
				break
			}
			if used[c.h.ID] || cool(c.h, ReasonRaiseCash) {
				continue
			}
			amt := decimal.Min(c.cap, remaining)
			qty, got, ok := sellOf(c.h, amt)
			if !ok {
				continue
			}
			used[c.h.ID] = true
			remaining = remaining.Sub(got)
			plan.RaiseCash = append(plan.RaiseCash, TrimSuggestion{
				AssetID: c.h.ID, Reason: ReasonRaiseCash, Reasons: []TrimReason{ReasonRaiseCash},
				Quantity: qty, Price: c.h.Price, Amount: got,
			})
		}
	}
	if remaining.IsPositive() {
		plan.RaiseCashShortfall = remaining
	}
}

// betterOpportunity pairs candidates with the weakest beaten holding (§9.4).
func (p Params) betterOpportunity(plan *TrimPlan, hs []TrimHolding, in TrimInput, cool func(TrimHolding, TrimReason) bool) {
	cands := append([]BetterCandidate(nil), in.Candidates...)
	sort.SliceStable(cands, func(i, j int) bool {
		if c := cands[i].Upside.Cmp(cands[j].Upside); c != 0 {
			return c > 0
		}
		return cands[i].ID < cands[j].ID
	})
	used := map[string]bool{}
	for _, c := range cands {
		var pick *TrimHolding
		var pickUp decimal.Decimal
		for i := range hs {
			h := hs[i]
			if used[h.ID] || h.ThesisBroken || h.FairValue == nil || !h.FairValue.IsPositive() || !h.Price.IsPositive() {
				continue
			}
			up := h.FairValue.Sub(h.Price).Div(h.Price)
			if c.Score.LessThan(h.Score.Add(p.BetterScoreDelta)) || c.Upside.LessThan(up.Add(p.BetterUpsideDelta)) {
				continue
			}
			if pick == nil || h.Score.LessThan(pick.Score) || (h.Score.Equal(pick.Score) && up.LessThan(pickUp)) {
				hc := h
				pick, pickUp = &hc, up
			}
		}
		if pick == nil || cool(*pick, ReasonBetterOpportunity) {
			continue
		}
		used[pick.ID] = true
		step := pick.Value().Mul(p.TrimFraction)
		var s TrimSuggestion
		if step.GreaterThanOrEqual(c.FirstTranche) {
			qty, amt, ok := sellOf(*pick, step)
			if !ok {
				continue
			}
			s = TrimSuggestion{AssetID: pick.ID, Quantity: qty, Price: pick.Price, Amount: amt}
		} else {
			s = fullExit(*pick, ReasonBetterOpportunity)
		}
		s.Reason, s.Reasons, s.PairedCandidate = ReasonBetterOpportunity, []TrimReason{ReasonBetterOpportunity}, c.ID
		plan.Swaps = append(plan.Swaps, s)
	}
}
