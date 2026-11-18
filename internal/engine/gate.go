package engine

import (
	"time"

	"github.com/shopspring/decimal"
)

// GateInput is everything the buy gate needs. For a tactical entry set
// Tactical, put the target price in FairValue (and its date in FairValueDate)
// and the score is computed without the horizon factor.
type GateInput struct {
	Ratings         Factors
	Tactical        bool
	BreakConditions int // number of break conditions on the thesis
	FairValue       *decimal.Decimal
	FairValueDate   time.Time
	Price           decimal.Decimal
	ActiveCatalyst  bool // non-expired research note flagged opportunity
	Now             time.Time
}

// GateResult reports each check individually for the UI checklist.
type GateResult struct {
	Score decimal.Decimal

	ScoreOK        bool // score >= 2.5
	ThesisOK       bool // >= 1 break condition
	FairValueSet   bool
	FairValueFresh bool            // set and not older than 12 months
	PriceOK        bool            // price <= fair value (false when no fair value)
	Passed         bool            // all four checks
	Opportunity    bool            // Passed and (price <= FV x 0.8 or active catalyst)
	DeepDiscount   bool            // price <= FV x 0.8
	CatalystOnly   bool            // opportunity solely through the catalyst
	Discount       decimal.Decimal // (FV-price)/FV, 0 when no fair value
	Upside         decimal.Decimal // (FV-price)/price, 0 when no fair value
}

// EvaluateGate runs the buy gate (spec §8.1, §10.1) and the opportunity test
// (§8.2).
func (p Params) EvaluateGate(in GateInput) (GateResult, error) {
	var (
		score decimal.Decimal
		err   error
	)
	if in.Tactical {
		score, err = p.TacticalScore(in.Ratings)
	} else {
		score, err = p.Score(in.Ratings)
	}
	if err != nil {
		return GateResult{}, err
	}
	r := GateResult{Score: score, Discount: zero, Upside: zero}
	r.ScoreOK = score.GreaterThanOrEqual(p.MinScore)
	r.ThesisOK = in.BreakConditions >= 1
	if in.FairValue != nil && in.FairValue.IsPositive() {
		fv := *in.FairValue
		r.FairValueSet = true
		r.FairValueFresh = !in.Now.After(in.FairValueDate.AddDate(0, p.FairValueMaxMonths, 0))
		r.PriceOK = in.Price.LessThanOrEqual(fv)
		r.DeepDiscount = in.Price.LessThanOrEqual(fv.Mul(p.OpportunityMult))
		r.Discount = fv.Sub(in.Price).Div(fv)
		if in.Price.IsPositive() {
			r.Upside = fv.Sub(in.Price).Div(in.Price)
		}
	}
	r.Passed = r.ScoreOK && r.ThesisOK && r.FairValueFresh && r.PriceOK
	r.Opportunity = r.Passed && (r.DeepDiscount || in.ActiveCatalyst)
	r.CatalystOnly = r.Opportunity && !r.DeepDiscount
	return r, nil
}
