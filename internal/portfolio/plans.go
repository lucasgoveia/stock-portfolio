package portfolio

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
)

// ErrValidation is returned for bad planner input.
var ErrValidation = errors.New("portfolio validation error")

// ContributionPlan is an engine plan plus ticker lookup.
type ContributionPlan struct {
	engine.Plan
	Amount  decimal.Decimal   `json:"amount"`
	Tickers map[string]string `json:"tickers"`
}

func (s *Service) params(ctx context.Context) (engine.Params, error) {
	w, err := s.d.Ratings.Weights(ctx)
	if err != nil {
		return engine.Params{}, err
	}
	p := s.d.Params
	p.Weights = w
	return p, nil
}

func tickers(snap Snapshot) map[string]string {
	out := map[string]string{engine.CashAssetID: "CASH"}
	for _, x := range append(append([]Position(nil), snap.Holdings...), snap.Watchlist...) {
		out[key(x.AssetID)] = x.Ticker
	}
	return out
}

// PlanContribution plans where `amount` BRL of new money goes (spec §8.4).
func (s *Service) PlanContribution(ctx context.Context, amount decimal.Decimal) (ContributionPlan, error) {
	if !amount.IsPositive() {
		return ContributionPlan{}, fmt.Errorf("%w: amount must be positive", ErrValidation)
	}
	p, err := s.params(ctx)
	if err != nil {
		return ContributionPlan{}, err
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return ContributionPlan{}, err
	}
	in := engine.PlanInput{
		Amount: amount, PortfolioValue: snap.TotalValue, CashValue: snap.CashValue, CashTarget: snap.CashTarget,
		UndeployedWeight: snap.Undeployed, PendingUS: snap.PendingUS, Bootstrap: snap.Bootstrap,
	}
	for _, x := range append(append([]Position(nil), snap.Holdings...), snap.Watchlist...) {
		if x.Target == nil || x.Price == nil {
			continue
		}
		pa := engine.PlanAsset{
			ID: key(x.AssetID), Market: engine.Market(x.Market), Price: x.PriceBRL, ActualValue: x.ValueBRL,
			Target: x.Target.Weight, Upper: x.Target.Upper, RampStage: x.RampStage,
			Tactical: x.Sleeve == assets.SleeveTactical,
		}
		if x.Gate != nil {
			pa.Eligible, pa.Opportunity, pa.Discount, pa.CatalystOnly = x.Gate.Passed, x.Gate.Opportunity, x.Gate.Discount, x.Gate.CatalystOnly
		}
		in.Assets = append(in.Assets, pa)
	}
	return ContributionPlan{Plan: p.PlanContribution(in), Amount: amount, Tickers: tickers(snap)}, nil
}

// TrimsResult is an engine trim plan plus ticker lookup.
type TrimsResult struct {
	engine.TrimPlan
	Tickers map[string]string `json:"tickers"`
}

// Trims evaluates every sell trigger (spec §9).
func (s *Service) Trims(ctx context.Context) (TrimsResult, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return TrimsResult{}, err
	}
	p, err := s.params(ctx)
	if err != nil {
		return TrimsResult{}, err
	}
	return TrimsResult{TrimPlan: p.PlanTrims(s.trimInput(snap, p)), Tickers: tickers(snap)}, nil
}

func (s *Service) trimInput(snap Snapshot, p engine.Params) engine.TrimInput {
	in := engine.TrimInput{PortfolioValue: snap.TotalValue, CashWeight: snap.CashWeight, CashTarget: snap.CashTarget,
		Bootstrap: snap.Bootstrap, Now: s.d.Now()}
	fxOf := func(x Position) decimal.Decimal {
		if x.Currency == "USD" {
			return snap.USDBRL
		}
		return decimal.NewFromInt(1)
	}
	for _, x := range snap.Holdings {
		if x.Price == nil || !x.Quantity.IsPositive() {
			continue
		}
		h := engine.TrimHolding{ID: key(x.AssetID), Market: engine.Market(x.Market), Tactical: x.Sleeve == assets.SleeveTactical,
			Quantity: x.Quantity, Price: x.PriceBRL, ThesisBroken: x.ThesisStatus == thesis.StatusBroken, Score: decimal.Zero}
		if x.Score != nil {
			h.Score = *x.Score
		}
		if x.Target != nil {
			h.Target = *x.Target
		} else if !h.Tactical {
			continue // unrated core holding: no target to trim against
		}
		if x.FairValue != nil {
			fv := x.FairValue.Mul(fxOf(x))
			h.FairValue = &fv
		}
		if x.LastSell != nil {
			if t, err := time.Parse(dateLayout, *x.LastSell); err == nil {
				h.LastSell = &t
			}
		}
		in.Holdings = append(in.Holdings, h)
	}
	for _, x := range snap.Watchlist {
		if x.Gate == nil || !x.Gate.Passed || x.Score == nil || x.Target == nil {
			continue
		}
		stages := decimal.NewFromInt(int64(p.RampStages))
		in.Candidates = append(in.Candidates, engine.BetterCandidate{ID: key(x.AssetID), Score: *x.Score,
			Upside: x.Gate.Upside, FirstTranche: x.Target.Weight.Mul(snap.TotalValue).Div(stages)})
	}
	return in
}

// SetPendingUS records the BRL held for the next US remittance.
func (s *Service) SetPendingUS(ctx context.Context, v decimal.Decimal) error {
	if v.IsNegative() {
		return fmt.Errorf("%w: pending US must not be negative", ErrValidation)
	}
	return s.setSetting(ctx, SettingPendingUS, v)
}

// Alert types raised by Evaluate.
var evaluatedTypes = []string{
	"overweight", "overvalued", "raise_cash", "deploy_cash", "better_opportunity",
	"below_floor", "undeployed_budget", "too_many_positions", "opportunity",
}

// Evaluate raises the portfolio alerts and resolves cleared ones. Run it from
// the daily job after prices are fresh.
func (s *Service) Evaluate(ctx context.Context) error {
	if s.d.Alerts == nil {
		return nil
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	p, err := s.params(ctx)
	if err != nil {
		return err
	}
	tk := tickers(snap)
	active := map[string][]string{}
	up := func(typ string, assetID *int64, payload map[string]any) error {
		if _, err := s.d.Alerts.Upsert(ctx, typ, assetID, "", payload); err != nil {
			return err
		}
		active[typ] = append(active[typ], alerts.Key(typ, assetID, ""))
		return nil
	}
	idOf := func(k string) *int64 {
		v, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return nil
		}
		return &v
	}

	plan := p.PlanTrims(s.trimInput(snap, p))
	for _, sug := range plan.Suggestions {
		for _, r := range sug.Reasons {
			if r != engine.ReasonOverweight && r != engine.ReasonOvervalued {
				continue // thesis_broken is raised by the thesis service
			}
			if err := up(string(r), idOf(sug.AssetID), map[string]any{
				"ticker": tk[sug.AssetID], "suggestion": sug,
			}); err != nil {
				return err
			}
		}
	}
	switch snap.CashSignal {
	case "raise":
		if err := up("raise_cash", nil, map[string]any{"cash_weight": snap.CashWeight, "cash_target": snap.CashTarget,
			"trims": plan.RaiseCash, "shortfall": plan.RaiseCashShortfall, "tickers": tk}); err != nil {
			return err
		}
	case "deploy":
		if err := up("deploy_cash", nil, map[string]any{"cash_weight": snap.CashWeight, "cash_target": snap.CashTarget}); err != nil {
			return err
		}
	}
	for _, sw := range plan.Swaps {
		if err := up("better_opportunity", idOf(sw.AssetID), map[string]any{
			"ticker": tk[sw.AssetID], "candidate": tk[sw.PairedCandidate], "suggestion": sw,
		}); err != nil {
			return err
		}
	}
	for _, x := range snap.Holdings {
		id := x.AssetID
		if x.Band.BelowFloor {
			if err := up("below_floor", &id, map[string]any{"ticker": x.Ticker, "weight": x.Weight}); err != nil {
				return err
			}
		}
	}
	for _, x := range append(append([]Position(nil), snap.Holdings...), snap.Watchlist...) {
		id := x.AssetID
		if x.Gate != nil && x.Gate.Opportunity {
			if err := up("opportunity", &id, map[string]any{"ticker": x.Ticker, "discount": x.Gate.Discount,
				"catalyst_only": x.Gate.CatalystOnly}); err != nil {
				return err
			}
		}
	}
	if snap.TooFew && !snap.Bootstrap {
		if err := up("undeployed_budget", nil, map[string]any{"undeployed": snap.Undeployed}); err != nil {
			return err
		}
	}
	if snap.TooMany && !snap.Bootstrap {
		if err := up("too_many_positions", nil, map[string]any{"holdings": len(snap.Holdings)}); err != nil {
			return err
		}
	}
	for _, typ := range evaluatedTypes {
		if _, err := s.d.Alerts.ResolveMissing(ctx, typ, active[typ]); err != nil {
			return err
		}
	}
	// bootstrap_exit is informational: raised once on crossing, never auto-resolved.
	_, seenErr := s.q.GetSetting(ctx, SettingLastValue)
	prev := s.setting(ctx, SettingLastValue)
	if seenErr == nil && p.BootstrapExit(prev, snap.TotalValue) {
		if _, err := s.d.Alerts.Upsert(ctx, "bootstrap_exit", nil, "", map[string]any{
			"total_value": snap.TotalValue, "threshold": p.BootstrapBelow,
		}); err != nil {
			return err
		}
	}
	return s.setSetting(ctx, SettingLastValue, snap.TotalValue)
}
