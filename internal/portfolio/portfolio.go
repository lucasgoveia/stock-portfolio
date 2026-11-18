// Package portfolio joins stored data (positions, prices, ratings, fair
// values, theses, research, cash) with the pure rule engine: it builds the
// current snapshot (weights, targets, bands, cash target, buy gate), plans
// contributions and trims, and raises the portfolio alerts of spec §16.2.
package portfolio

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/dividends"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
	"github.com/lucasgoveia/stock-portfolio/internal/tax"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

const dateLayout = "2006-01-02"

// Setting keys.
const (
	SettingPendingUS = "pending_us_brl"   // BRL held for the next US remittance
	SettingLastValue = "last_total_value" // previous total, for bootstrap_exit
)

// ErrNoFX is returned when US assets or USD cash exist but no USDBRL rate does.
var ErrNoFX = errors.New("no USDBRL rate available")

// Deps are the services the portfolio reads.
type Deps struct {
	Conn      db.DBTX
	Assets    *assets.Service
	Trades    *trades.Service
	Cash      *cash.Service
	Rates     cash.Rates
	Ratings   *ratings.Service
	Valuation *valuation.Service
	Thesis    *thesis.Service
	Research  *research.Service
	Alerts    *alerts.Service
	Tactical  *tactical.Service  // optional: tactical setups (spec §10)
	Tax       *tax.Service       // optional: B3 tax impact of trims (spec §11.2)
	Dividends *dividends.Service // optional: undeployed dividends and yield on cost (spec §8.5)
	// Params are the rules used when Settings is nil.
	Params engine.Params
	// Settings (optional) supplies the rules at call time, so an edit applies
	// to the next evaluation or plan.
	Settings ParamsSource
	Now      func() time.Time
}

// ParamsSource supplies the editable rule parameters (internal/settings).
type ParamsSource interface {
	Params(ctx context.Context) (engine.Params, error)
}

// Service builds snapshots and plans.
type Service struct {
	d Deps
	q *db.Queries
}

// NewService builds a Service. Zero Params use engine.DefaultParams; a nil
// clock uses time.Now.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Params.ClampMax.IsZero() {
		d.Params = engine.DefaultParams()
	}
	return &Service{d: d, q: db.New(d.Conn)}
}

// Position is one holding or watchlist asset in the snapshot. Prices are in
// the asset's currency unless suffixed BRL.
type Position struct {
	AssetID   int64  `json:"asset_id"`
	Ticker    string `json:"ticker"`
	Market    string `json:"market"`
	Kind      string `json:"kind"`
	Currency  string `json:"currency"`
	Sleeve    string `json:"sleeve"`
	Status    string `json:"status"`
	Benchmark string `json:"benchmark"`

	Quantity decimal.Decimal `json:"quantity"`
	AvgCost  decimal.Decimal `json:"avg_cost"`
	// CostBRL is the BRL cost basis of the shares held.
	CostBRL decimal.Decimal `json:"cost_brl"`
	// DividendsTTM is net dividends received in the last 12 months (BRL);
	// YieldOnCost is DividendsTTM / CostBRL.
	DividendsTTM decimal.Decimal  `json:"dividends_ttm"`
	YieldOnCost  decimal.Decimal  `json:"yield_on_cost"`
	Price        *decimal.Decimal `json:"price"`
	PriceDate    string           `json:"price_date,omitempty"`
	PriceBRL     decimal.Decimal  `json:"price_brl"`
	ValueBRL     decimal.Decimal  `json:"value_brl"`
	Weight       decimal.Decimal  `json:"weight"`

	Score         *decimal.Decimal `json:"score"`
	FairValue     *decimal.Decimal `json:"fair_value"`
	FairValueDate string           `json:"fair_value_date,omitempty"`
	Overvalued    bool             `json:"overvalued"`
	ThesisStatus  string           `json:"thesis_status,omitempty"`
	Catalyst      bool             `json:"catalyst"`

	Target    *engine.Target        `json:"target,omitempty"`
	Band      engine.PositionStatus `json:"band"`
	Gate      *engine.GateResult    `json:"gate,omitempty"`
	GateError string                `json:"gate_error,omitempty"`
	RampStage int                   `json:"ramp_stage"`
	LastSell  *string               `json:"last_sell,omitempty"`
	Tactical  *TacticalView         `json:"tactical,omitempty"`
	factors   *engine.Factors       // latest rating
	breaks    int                   // break conditions
	fvDate    time.Time             // fair value date
}

// TacticalView is a tactical position's setup with what it implies today.
type TacticalView struct {
	Setup tactical.Setup `json:"setup"`
	// SuggestedValueBRL is the entry size applied to the current portfolio value.
	SuggestedValueBRL decimal.Decimal         `json:"suggested_value_brl"`
	Signals           []engine.TacticalSignal `json:"signals"`
}

// Snapshot is the portfolio at a moment.
type Snapshot struct {
	AsOf            string          `json:"as_of"`
	USDBRL          decimal.Decimal `json:"usdbrl"`
	USDBRLDate      string          `json:"usdbrl_date,omitempty"`
	TotalValue      decimal.Decimal `json:"total_value"`
	EquityValue     decimal.Decimal `json:"equity_value"`
	CashValue       decimal.Decimal `json:"cash_value"`
	CashWeight      decimal.Decimal `json:"cash_weight"`
	CashTarget      decimal.Decimal `json:"cash_target"`
	CashSignal      string          `json:"cash_signal"`
	OvervaluedShare decimal.Decimal `json:"overvalued_share"`
	TacticalWeight  decimal.Decimal `json:"tactical_weight"`
	TacticalOpen    int             `json:"tactical_open"`
	TacticalMaxOpen int             `json:"tactical_max_open"`
	Bootstrap       bool            `json:"bootstrap"`
	CoreBudget      decimal.Decimal `json:"core_budget"`
	Undeployed      decimal.Decimal `json:"undeployed"`
	TooFew          bool            `json:"too_few"`
	TooMany         bool            `json:"too_many"`
	PendingUS       decimal.Decimal `json:"pending_us"`
	// UndeployedDividends is net dividend money not yet put into a plan.
	UndeployedDividends decimal.Decimal `json:"undeployed_dividends"`
	Holdings            []Position      `json:"holdings"`
	Watchlist           []Position      `json:"watchlist"`
	// Unpriced and Unrated list tickers left out of the rule math.
	Unpriced []string     `json:"unpriced"`
	Unrated  []string     `json:"unrated"`
	Cash     cash.Summary `json:"cash"`
}

func key(id int64) string { return strconv.FormatInt(id, 10) }

func (s *Service) setting(ctx context.Context, k string) decimal.Decimal {
	v, err := s.q.GetSetting(ctx, k)
	if err != nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func (s *Service) setSetting(ctx context.Context, k string, v decimal.Decimal) error {
	return s.q.SetSetting(ctx, db.SetSettingParams{Key: k, Value: v.String()})
}

// Snapshot computes the current portfolio.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	today := s.d.Now().Format(dateLayout)
	snap := Snapshot{AsOf: today, Unpriced: []string{}, Unrated: []string{}, Holdings: []Position{}, Watchlist: []Position{}}
	snap.PendingUS = s.setting(ctx, SettingPendingUS)
	p, err := s.params(ctx)
	if err != nil {
		return snap, err
	}
	snap.TacticalMaxOpen = p.TacticalMaxOpen
	if s.d.Dividends != nil {
		u, err := s.d.Dividends.Undeployed(ctx)
		if err != nil {
			return snap, err
		}
		snap.UndeployedDividends = u
	}

	var list []db.Asset
	for _, st := range []string{assets.StatusHolding, assets.StatusWatchlist} {
		l, err := s.d.Assets.List(ctx, assets.Filter{Status: st})
		if err != nil {
			return snap, err
		}
		list = append(list, l...)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Ticker < list[j].Ticker })

	positions, err := s.d.Trades.Positions(ctx)
	if err != nil {
		return snap, err
	}
	pos := map[int64]trades.Position{}
	for _, x := range positions {
		pos[x.AssetID] = x
	}
	priceRows, err := s.q.LatestAssetPrices(ctx)
	if err != nil {
		return snap, fmt.Errorf("latest prices: %w", err)
	}
	prices := map[int64]db.AssetPrice{}
	for _, r := range priceRows {
		prices[r.AssetID] = r
	}
	fvs, err := s.d.Valuation.LatestAll(ctx)
	if err != nil {
		return snap, err
	}
	opps, err := s.d.Research.ActiveOpportunities(ctx, today, nil)
	if err != nil {
		return snap, err
	}
	catalyst := map[int64]bool{}
	for _, n := range opps {
		catalyst[n.AssetID] = true
	}
	ttm := map[int64]decimal.Decimal{}
	if s.d.Dividends != nil {
		if ttm, err = s.d.Dividends.TrailingNetBRL(ctx, s.d.Now()); err != nil {
			return snap, err
		}
	}
	setups := map[int64]tactical.Setup{}
	if s.d.Tactical != nil {
		if setups, err = s.d.Tactical.All(ctx); err != nil {
			return snap, err
		}
	}
	cs, err := s.d.Cash.Summary(ctx, today)
	if err != nil {
		return snap, err
	}
	snap.Cash, snap.CashValue = cs, cs.TotalBRL

	needFX := !cs.USDBalance.IsZero()
	for _, a := range list {
		if a.Currency == "USD" {
			needFX = true
		}
	}
	snap.USDBRL = decimal.NewFromInt(1)
	if needFX {
		q, err := s.d.Rates.USDBRL(ctx, today)
		if err != nil {
			return snap, fmt.Errorf("%w: %v", ErrNoFX, err)
		}
		snap.USDBRL, snap.USDBRLDate = q.Value, q.Date
	}
	toBRL := func(cur string, v decimal.Decimal) decimal.Decimal {
		if cur == "USD" {
			return v.Mul(snap.USDBRL)
		}
		return v
	}

	all := make([]*Position, 0, len(list))
	for _, a := range list {
		x := &Position{AssetID: a.ID, Ticker: a.Ticker, Market: a.Market, Kind: a.Kind, Currency: a.Currency, Sleeve: a.Sleeve,
			Status: a.Status, Benchmark: a.Benchmark, Quantity: decimal.Zero, AvgCost: decimal.Zero,
			PriceBRL: decimal.Zero, ValueBRL: decimal.Zero, Weight: decimal.Zero, Catalyst: catalyst[a.ID],
			CostBRL: decimal.Zero, DividendsTTM: ttm[a.ID], YieldOnCost: decimal.Zero}
		if ps, ok := pos[a.ID]; ok {
			x.Quantity, x.AvgCost, x.CostBRL = ps.Quantity, ps.AvgCost, ps.TotalCostBRL
			x.YieldOnCost = engine.YieldOnCost(x.DividendsTTM, x.CostBRL)
		}
		if pr, ok := prices[a.ID]; ok {
			if v, err := decimal.NewFromString(pr.Close); err == nil {
				x.Price, x.PriceDate = &v, pr.RefDate
				x.PriceBRL = toBRL(a.Currency, v)
				x.ValueBRL = x.Quantity.Mul(x.PriceBRL)
			}
		}
		if x.Price == nil {
			snap.Unpriced = append(snap.Unpriced, a.Ticker)
		}
		if fv, ok := fvs[a.ID]; ok {
			v := fv.ValuePerShare.Decimal
			if fv.Currency != a.Currency { // convert to the asset's currency
				if fv.Currency == "USD" {
					v = v.Mul(snap.USDBRL)
				} else if !snap.USDBRL.IsZero() {
					v = v.Div(snap.USDBRL)
				}
			}
			x.FairValue, x.FairValueDate = &v, fv.AsOf
			x.fvDate, _ = time.Parse(dateLayout, fv.AsOf)
		}
		if r, err := s.d.Ratings.Latest(ctx, a.ID); err == nil {
			f := r.Factors()
			x.factors = &f
			if sc, err := p.Score(f); err == nil {
				x.Score = &sc
			}
		} else if !errors.Is(err, ratings.ErrNotFound) {
			return snap, err
		}
		if x.factors == nil {
			snap.Unrated = append(snap.Unrated, a.Ticker)
		}
		if th, err := s.d.Thesis.Get(ctx, a.ID); err == nil {
			x.ThesisStatus, x.breaks = th.Status, len(th.BreakConditions)
		} else if !errors.Is(err, thesis.ErrNotFound) {
			return snap, err
		}
		ts, err := s.d.Trades.List(ctx, a.ID)
		if err != nil {
			return snap, err
		}
		x.RampStage, x.LastSell = rampAndLastSell(ts, p.RampStages)
		if x.Price != nil && x.FairValue != nil {
			x.Overvalued = p.Overvalued(*x.Price, x.FairValue)
		}
		all = append(all, x)
		snap.EquityValue = snap.EquityValue.Add(x.ValueBRL)
	}
	snap.TotalValue = snap.EquityValue.Add(snap.CashValue)
	snap.Bootstrap = p.IsBootstrap(snap.TotalValue)
	if snap.TotalValue.IsPositive() {
		snap.CashWeight = snap.CashValue.Div(snap.TotalValue)
		for _, x := range all {
			x.Weight = x.ValueBRL.Div(snap.TotalValue)
		}
	}

	// Gate for every rated asset.
	now := s.d.Now()
	for _, x := range all {
		if x.factors == nil || x.Price == nil {
			continue
		}
		in := engine.GateInput{
			Ratings: *x.factors, Tactical: x.Sleeve == assets.SleeveTactical, BreakConditions: x.breaks,
			FairValue: x.FairValue, FairValueDate: x.fvDate, Price: *x.Price, ActiveCatalyst: x.Catalyst, Now: now,
		}
		if in.Tactical {
			// Tactical entries are judged against the setup's target price (spec §10.1).
			in.FairValue, in.FairValueDate = nil, time.Time{}
			if st, ok := setups[x.AssetID]; ok {
				tp := st.TargetPrice.Decimal
				in.FairValue = &tp
				in.FairValueDate, _ = time.Parse(time.RFC3339, st.UpdatedAt)
			}
		}
		g, err := p.EvaluateGate(in)
		if err != nil {
			x.GateError = err.Error()
			continue
		}
		x.Gate = &g
	}

	// Cash target from core holdings.
	var core []engine.CoreWeight
	for _, x := range all {
		if x.Status == assets.StatusHolding && x.Sleeve == assets.SleeveCore && x.Price != nil {
			core = append(core, engine.CoreWeight{ID: key(x.AssetID), Weight: x.Weight, Price: *x.Price, FairValue: x.FairValue})
		}
		if x.Status == assets.StatusHolding && x.Sleeve == assets.SleeveTactical {
			snap.TacticalWeight = snap.TacticalWeight.Add(x.Weight)
			snap.TacticalOpen++
		}
		if st, ok := setups[x.AssetID]; ok && x.Sleeve == assets.SleeveTactical {
			v := &TacticalView{Setup: st, SuggestedValueBRL: st.Sizing.Size.Mul(snap.TotalValue), Signals: []engine.TacticalSignal{}}
			if x.Price != nil && x.Status == assets.StatusHolding {
				v.Signals = engine.TacticalSignals(*x.Price, st.TargetPrice.Decimal, st.StopPrice.Decimal, st.ExpiresOn, today)
			}
			x.Tactical = v
		}
	}
	cr := p.CashTarget(core)
	snap.CashTarget, snap.OvervaluedShare = cr.Target, cr.OvervaluedShare
	snap.CashSignal = cashSignal(p.CashBand(snap.CashWeight, snap.Bootstrap))

	// Targets: rated core holdings plus watchlist names that pass the gate.
	var targeted []engine.CoreHolding
	for _, x := range all {
		if x.Sleeve != assets.SleeveCore || x.Score == nil {
			continue
		}
		if x.Status == assets.StatusHolding || (x.Gate != nil && x.Gate.Passed) {
			targeted = append(targeted, engine.CoreHolding{ID: key(x.AssetID), Score: *x.Score})
		}
	}
	tr := p.ComputeTargets(engine.TargetInput{Holdings: targeted, PortfolioValue: snap.TotalValue,
		CashTarget: snap.CashTarget, TacticalWeight: snap.TacticalWeight})
	snap.CoreBudget, snap.Undeployed, snap.TooFew, snap.TooMany = tr.CoreBudget, tr.Undeployed, tr.TooFew, tr.TooMany
	byID := map[string]engine.Target{}
	for _, t := range tr.Targets {
		byID[t.ID] = t
	}
	for _, x := range all {
		if t, ok := byID[key(x.AssetID)]; ok {
			tt := t
			x.Target = &tt
			if x.Status == assets.StatusHolding {
				x.Band = p.Status(x.Weight, tt, snap.Bootstrap)
			}
		}
		if x.Status == assets.StatusHolding {
			snap.Holdings = append(snap.Holdings, *x)
		} else {
			snap.Watchlist = append(snap.Watchlist, *x)
		}
	}
	roundSnapshot(&snap)
	return snap, nil
}

func cashSignal(c engine.CashSignal) string {
	switch c {
	case engine.CashRaise:
		return "raise"
	case engine.CashDeploy:
		return "deploy"
	}
	return "ok"
}

// rampAndLastSell derives the ramp stage (buys since the position was last
// flat, capped) and the last sell date from trades sorted by date.
func rampAndLastSell(ts []trades.Trade, maxStage int) (int, *string) {
	sorted := append([]trades.Trade(nil), ts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Date != sorted[j].Date {
			return sorted[i].Date < sorted[j].Date
		}
		return sorted[i].ID < sorted[j].ID
	})
	qty, buys := decimal.Zero, 0
	var last *string
	for _, t := range sorted {
		if t.Side == trades.SideBuy {
			if qty.IsZero() {
				buys = 0
			}
			buys++
			qty = qty.Add(t.Quantity)
			continue
		}
		qty = qty.Sub(t.Quantity)
		d := t.Date
		last = &d
	}
	if qty.IsZero() {
		buys = 0
	}
	if buys > maxStage {
		buys = maxStage
	}
	return buys, last
}
