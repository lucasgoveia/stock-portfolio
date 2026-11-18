// Package settings stores the editable rule parameters (docs/spec.md: every
// number is a default, editable). Each group is one JSON value in the
// settings table, merged over the built-in defaults when read: a missing key
// is its default and an unknown key is ignored.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/config"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// ErrValidation is returned for values that break a rule; the message lists
// each offending field as "field: reason".
var ErrValidation = errors.New("settings validation error")

// ErrNotFound is returned for an unknown group.
var ErrNotFound = errors.New("settings group not found")

// Group names.
const (
	GroupAllocation = "allocation"
	GroupPlanner    = "planner"
	GroupTax        = "tax"
	GroupTactical   = "tactical"
	GroupAI         = "ai"
)

// Groups lists every group name.
var Groups = []string{GroupAllocation, GroupPlanner, GroupTax, GroupTactical, GroupAI}

func key(group string) string { return "params." + group }

// Service reads and writes the parameter groups.
type Service struct {
	q    *db.Queries
	keys []config.KeyStatus
	// usage reports this month's AI spend; nil means zero.
	usage func(context.Context) (decimal.Decimal, error)
}

// UseUsage sets where Status reads the AI analyst's month-to-date spend from.
func (s *Service) UseUsage(f func(context.Context) (decimal.Decimal, error)) { s.usage = f }

// NewService builds a Service. keys is the API key status reported by
// Status (from config.Config.KeyStatuses); it may be nil.
func NewService(conn db.DBTX, keys []config.KeyStatus) *Service {
	return &Service{q: db.New(conn), keys: keys}
}

// Dec is a decimal setting (JSON string).
type Dec = money.Decimal

func d(s string) Dec { return money.New(decimal.RequireFromString(s)) }

// Allocation holds the target, band, cash, gate and trim rules (spec §6-§9).
// Percentages are fractions.
type Allocation struct {
	RawBase            Dec `json:"raw_base" doc:"Raw target weight at score 1 (fraction)"`
	RawSlope           Dec `json:"raw_slope" doc:"Raw weight added per score point (fraction)"`
	ClampMin           Dec `json:"clamp_min" doc:"Lowest target weight (fraction)"`
	ClampMax           Dec `json:"clamp_max" doc:"Highest target weight (fraction)"`
	BandLowerMult      Dec `json:"band_lower_mult" doc:"Lower band as a multiple of target"`
	BandUpperMult      Dec `json:"band_upper_mult" doc:"Upper band as a multiple of target"`
	BandUpperCap       Dec `json:"band_upper_cap" doc:"Upper band ceiling (fraction)"`
	HardFloor          Dec `json:"hard_floor" doc:"Weight under which a position is flagged (fraction)"`
	HardCap            Dec `json:"hard_cap" doc:"Weight above which a position is flagged (fraction)"`
	CashBase           Dec `json:"cash_base" doc:"Cash target at the lowest overvaluation (fraction)"`
	CashSpan           Dec `json:"cash_span" doc:"Cash target added across the overvaluation range (fraction)"`
	OvervaluedMult     Dec `json:"overvalued_mult" doc:"Price / fair value above which an asset counts as overvalued"`
	CashBandMin        Dec `json:"cash_band_min" doc:"Lowest cash target (fraction)"`
	CashBandMax        Dec `json:"cash_band_max" doc:"Highest cash target (fraction)"`
	MinScore           Dec `json:"min_score" doc:"Buy gate: minimum weighted score"`
	FairValueMaxMonths int `json:"fair_value_max_months" doc:"Buy gate: fair value older than this blocks buys"`
	OpportunityMult    Dec `json:"opportunity_mult" doc:"Price / fair value at or below which an opportunity opens"`
	RampStages         int `json:"ramp_stages" doc:"Tranches a new position is built in"`
	TrimFraction       Dec `json:"trim_fraction" doc:"Share of the excess sold per trim step (fraction)"`
	CooldownDays       int `json:"cooldown_days" doc:"Days between trims of the same asset"`
	BetterScoreDelta   Dec `json:"better_score_delta" doc:"Score lead a better alternative needs"`
	BetterUpsideDelta  Dec `json:"better_upside_delta" doc:"Upside lead (fraction) a better alternative needs"`
}

// Planner holds the bootstrap and order-size rules (spec §6.3, §8.3).
type Planner struct {
	BootstrapBelow Dec `json:"bootstrap_below" doc:"Portfolio value (BRL) below which bootstrap mode applies"`
	MinOrderB3     Dec `json:"min_order_b3" doc:"Smallest B3 order (BRL); smaller ones are skipped"`
	USBatchMin     Dec `json:"us_batch_min" doc:"BRL accumulated before a US purchase is proposed"`
}

// Tax holds the B3 tax and dividend withholding rules (spec §8.5, §11.2).
type Tax struct {
	ExemptionLimit            Dec    `json:"exemption_limit" doc:"Monthly stock sales exempt from tax (BRL)"`
	SwingRate                 Dec    `json:"swing_rate" doc:"Swing trade rate (fraction)"`
	DayTradeRate              Dec    `json:"day_trade_rate" doc:"Day trade rate (fraction)"`
	DARFMin                   Dec    `json:"darf_min" doc:"Smaller DARF amounts roll into the next month (BRL)"`
	DARFCode                  string `json:"darf_code" doc:"Swing trade DARF code"`
	DayTradeDARFCode          string `json:"day_trade_darf_code" doc:"Day trade DARF code"`
	DayTradeDARFCodeConfirmed bool   `json:"day_trade_darf_code_confirmed" doc:"True once a contador confirmed the day trade code"`
	JCPRate                   Dec    `json:"jcp_rate" doc:"IRRF on JCP (fraction)"`
	DividendRate              Dec    `json:"dividend_rate" doc:"IRRF on dividends above the monthly limit (fraction)"`
	DividendMonthly           Dec    `json:"dividend_monthly" doc:"Monthly dividends from one payer above which IRRF applies (BRL)"`
	USRate                    Dec    `json:"us_rate" doc:"US dividend withholding (fraction)"`
}

// Tactical holds the tactical sleeve sizing rules (spec §10).
type Tactical struct {
	MaxLoss Dec `json:"max_loss" doc:"Portfolio fraction lost at the stop"`
	MinSize Dec `json:"min_size" doc:"Smallest position (fraction)"`
	MaxSize Dec `json:"max_size" doc:"Largest position (fraction)"`
	MaxOpen int `json:"max_open" doc:"Open tactical positions before a warning"`
}

// AI holds the AI analyst settings.
type AI struct {
	Model         string `json:"model" doc:"Model used by the AI analyst"`
	MonthlyCapUSD Dec    `json:"monthly_cap_usd" doc:"Spending cap per month (USD)"`
}

// DefaultAllocation returns the spec defaults.
func DefaultAllocation() Allocation {
	p := engine.DefaultParams()
	return Allocation{
		RawBase: money.New(p.RawBase), RawSlope: money.New(p.RawSlope), ClampMin: money.New(p.ClampMin),
		ClampMax: money.New(p.ClampMax), BandLowerMult: money.New(p.BandLowerMult), BandUpperMult: money.New(p.BandUpperMult),
		BandUpperCap: money.New(p.BandUpperCap), HardFloor: money.New(p.HardFloor), HardCap: money.New(p.HardCap),
		CashBase: money.New(p.CashBase), CashSpan: money.New(p.CashSpan), OvervaluedMult: money.New(p.OvervaluedMult),
		CashBandMin: money.New(p.CashBandMin), CashBandMax: money.New(p.CashBandMax), MinScore: money.New(p.MinScore),
		FairValueMaxMonths: p.FairValueMaxMonths, OpportunityMult: money.New(p.OpportunityMult), RampStages: p.RampStages,
		TrimFraction: money.New(p.TrimFraction), CooldownDays: p.CooldownDays, BetterScoreDelta: money.New(p.BetterScoreDelta),
		BetterUpsideDelta: money.New(p.BetterUpsideDelta),
	}
}

// DefaultPlanner returns the spec defaults.
func DefaultPlanner() Planner {
	p := engine.DefaultParams()
	return Planner{BootstrapBelow: money.New(p.BootstrapBelow), MinOrderB3: money.New(p.MinOrderB3), USBatchMin: money.New(p.USBatchMin)}
}

// DefaultTax returns the spec defaults.
func DefaultTax() Tax {
	t, v := engine.DefaultTaxParams(), engine.DefaultDividendParams()
	return Tax{
		ExemptionLimit: money.New(t.ExemptionLimit), SwingRate: money.New(t.SwingRate), DayTradeRate: money.New(t.DayTradeRate),
		DARFMin: money.New(t.DARFMin), DARFCode: t.DARFCode, DayTradeDARFCode: t.DayTradeDARFCode,
		DayTradeDARFCodeConfirmed: t.DayTradeDARFCodeConfirmed,
		JCPRate:                   money.New(v.JCPRate), DividendRate: money.New(v.DividendRate),
		DividendMonthly: money.New(v.DividendMonthly), USRate: money.New(v.USRate),
	}
}

// DefaultTactical returns the spec defaults.
func DefaultTactical() Tactical {
	p := engine.DefaultParams()
	return Tactical{MaxLoss: money.New(p.TacticalMaxLoss), MinSize: money.New(p.TacticalMinSize),
		MaxSize: money.New(p.TacticalMaxSize), MaxOpen: p.TacticalMaxOpen}
}

// DefaultAI returns the defaults.
func DefaultAI() AI { return AI{Model: "claude-sonnet-5-5", MonthlyCapUSD: d("20")} }

// check collects field errors.
type check struct{ errs []string }

func (c *check) add(field, format string, args ...any) {
	c.errs = append(c.errs, field+": "+fmt.Sprintf(format, args...))
}

func (c *check) err() error {
	if len(c.errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrValidation, strings.Join(c.errs, "; "))
}

var (
	zero = decimal.Zero
	one  = decimal.NewFromInt(1)
)

// frac requires 0 <= v <= 1 (or 0 < v when positive).
func (c *check) frac(field string, v Dec, positive bool) {
	if v.LessThan(zero) || v.GreaterThan(one) || (positive && !v.IsPositive()) {
		if positive {
			c.add(field, "must be above 0%% and at most 100%%")
		} else {
			c.add(field, "must be between 0%% and 100%%")
		}
	}
}

func (c *check) nonNeg(field string, v Dec) {
	if v.LessThan(zero) {
		c.add(field, "must not be negative")
	}
}

func (c *check) atLeast(field string, v, min decimal.Decimal) {
	if v.LessThan(min) {
		c.add(field, "must be at least %s", min)
	}
}

func (c *check) intAtLeast(field string, v, min int) {
	if v < min {
		c.add(field, "must be at least %d", min)
	}
}

// Validate checks ranges and ordering.
func (a Allocation) Validate() error {
	var c check
	for _, f := range []struct {
		n string
		v Dec
	}{{"raw_base", a.RawBase}, {"raw_slope", a.RawSlope}, {"clamp_min", a.ClampMin}, {"clamp_max", a.ClampMax},
		{"band_upper_cap", a.BandUpperCap}, {"hard_floor", a.HardFloor}, {"hard_cap", a.HardCap}, {"cash_base", a.CashBase},
		{"cash_span", a.CashSpan}, {"cash_band_min", a.CashBandMin}, {"cash_band_max", a.CashBandMax},
		{"better_upside_delta", a.BetterUpsideDelta}} {
		c.frac(f.n, f.v, false)
	}
	c.frac("trim_fraction", a.TrimFraction, true)
	if !a.BandLowerMult.IsPositive() || a.BandLowerMult.GreaterThan(one) {
		c.add("band_lower_mult", "must be above 0 and at most 1")
	}
	c.atLeast("band_upper_mult", a.BandUpperMult.Decimal, one)
	c.atLeast("overvalued_mult", a.OvervaluedMult.Decimal, one)
	if !a.OpportunityMult.IsPositive() || a.OpportunityMult.GreaterThan(one) {
		c.add("opportunity_mult", "must be above 0 and at most 1")
	}
	if a.MinScore.LessThan(one) || a.MinScore.GreaterThan(decimal.NewFromInt(5)) {
		c.add("min_score", "must be between 1 and 5")
	}
	c.nonNeg("better_score_delta", a.BetterScoreDelta)
	c.intAtLeast("fair_value_max_months", a.FairValueMaxMonths, 1)
	c.intAtLeast("ramp_stages", a.RampStages, 1)
	c.intAtLeast("cooldown_days", a.CooldownDays, 0)
	if a.ClampMin.GreaterThan(a.ClampMax.Decimal) {
		c.add("clamp_min", "must not exceed the highest target")
	}
	if a.HardFloor.GreaterThan(a.ClampMin.Decimal) {
		c.add("hard_floor", "must not exceed the lowest target")
	}
	if a.HardCap.LessThan(a.ClampMax.Decimal) {
		c.add("hard_cap", "must not be below the highest target")
	}
	if a.BandUpperCap.LessThan(a.ClampMax.Decimal) {
		c.add("band_upper_cap", "must not be below the highest target")
	}
	if a.CashBandMin.GreaterThan(a.CashBandMax.Decimal) {
		c.add("cash_band_min", "must not exceed the highest cash target")
	}
	return c.err()
}

// Validate checks ranges.
func (p Planner) Validate() error {
	var c check
	c.nonNeg("bootstrap_below", p.BootstrapBelow)
	c.nonNeg("min_order_b3", p.MinOrderB3)
	c.nonNeg("us_batch_min", p.USBatchMin)
	return c.err()
}

func validCode(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Validate checks ranges.
func (t Tax) Validate() error {
	var c check
	c.nonNeg("exemption_limit", t.ExemptionLimit)
	c.nonNeg("darf_min", t.DARFMin)
	c.nonNeg("dividend_monthly", t.DividendMonthly)
	for _, f := range []struct {
		n string
		v Dec
	}{{"swing_rate", t.SwingRate}, {"day_trade_rate", t.DayTradeRate}, {"jcp_rate", t.JCPRate},
		{"dividend_rate", t.DividendRate}, {"us_rate", t.USRate}} {
		c.frac(f.n, f.v, false)
	}
	if !validCode(t.DARFCode) {
		c.add("darf_code", "must be 4 digits")
	}
	if !validCode(t.DayTradeDARFCode) {
		c.add("day_trade_darf_code", "must be 4 digits")
	}
	return c.err()
}

// Validate checks ranges and ordering.
func (t Tactical) Validate() error {
	var c check
	c.frac("max_loss", t.MaxLoss, true)
	c.frac("min_size", t.MinSize, true)
	c.frac("max_size", t.MaxSize, true)
	if t.MinSize.GreaterThan(t.MaxSize.Decimal) {
		c.add("min_size", "must not exceed the largest size")
	}
	c.intAtLeast("max_open", t.MaxOpen, 1)
	return c.err()
}

// Validate checks the model and cap.
func (a AI) Validate() error {
	var c check
	if m := strings.TrimSpace(a.Model); m == "" || len(m) > 100 {
		c.add("model", "is required (at most 100 characters)")
	}
	c.nonNeg("monthly_cap_usd", a.MonthlyCapUSD)
	return c.err()
}

// load merges the stored JSON for group over def.
func load[T any](ctx context.Context, q *db.Queries, group string, def T) (T, error) {
	raw, err := q.GetSetting(ctx, key(group))
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("get setting %s: %w", group, err)
	}
	out := def
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return def, fmt.Errorf("setting %s: %w", group, err)
	}
	return out, nil
}

func (s *Service) store(ctx context.Context, group string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode setting %s: %w", group, err)
	}
	if err := s.q.SetSetting(ctx, db.SetSettingParams{Key: key(group), Value: string(b)}); err != nil {
		return fmt.Errorf("set setting %s: %w", group, err)
	}
	return nil
}

// Allocation returns the stored allocation rules over defaults.
func (s *Service) Allocation(ctx context.Context) (Allocation, error) {
	return load(ctx, s.q, GroupAllocation, DefaultAllocation())
}

// Planner returns the stored planner rules over defaults.
func (s *Service) Planner(ctx context.Context) (Planner, error) {
	return load(ctx, s.q, GroupPlanner, DefaultPlanner())
}

// Tax returns the stored tax rules over defaults.
func (s *Service) Tax(ctx context.Context) (Tax, error) {
	return load(ctx, s.q, GroupTax, DefaultTax())
}

// Tactical returns the stored tactical rules over defaults.
func (s *Service) Tactical(ctx context.Context) (Tactical, error) {
	return load(ctx, s.q, GroupTactical, DefaultTactical())
}

// AI returns the stored AI analyst settings over defaults.
func (s *Service) AI(ctx context.Context) (AI, error) { return load(ctx, s.q, GroupAI, DefaultAI()) }

// SetAllocation validates and stores the allocation rules.
func (s *Service) SetAllocation(ctx context.Context, v Allocation) (Allocation, error) {
	return set(ctx, s, GroupAllocation, v)
}

// SetPlanner validates and stores the planner rules.
func (s *Service) SetPlanner(ctx context.Context, v Planner) (Planner, error) {
	return set(ctx, s, GroupPlanner, v)
}

// SetTax validates and stores the tax rules.
func (s *Service) SetTax(ctx context.Context, v Tax) (Tax, error) { return set(ctx, s, GroupTax, v) }

// SetTactical validates and stores the tactical rules.
func (s *Service) SetTactical(ctx context.Context, v Tactical) (Tactical, error) {
	return set(ctx, s, GroupTactical, v)
}

// SetAI validates and stores the AI analyst settings.
func (s *Service) SetAI(ctx context.Context, v AI) (AI, error) { return set(ctx, s, GroupAI, v) }

func set[T interface{ Validate() error }](ctx context.Context, s *Service, group string, v T) (T, error) {
	if err := v.Validate(); err != nil {
		return v, err
	}
	return v, s.store(ctx, group, v)
}

// Reset removes the stored values of group so defaults apply again.
func (s *Service) Reset(ctx context.Context, group string) error {
	known := false
	for _, g := range Groups {
		known = known || g == group
	}
	if !known {
		return fmt.Errorf("%w: %s", ErrNotFound, group)
	}
	if err := s.q.DeleteSetting(ctx, key(group)); err != nil {
		return fmt.Errorf("reset setting %s: %w", group, err)
	}
	return nil
}

// Params assembles the engine rule parameters from the stored groups.
// Factor weights are left at their defaults: they live with the ratings.
func (s *Service) Params(ctx context.Context) (engine.Params, error) {
	a, err := s.Allocation(ctx)
	if err != nil {
		return engine.Params{}, err
	}
	pl, err := s.Planner(ctx)
	if err != nil {
		return engine.Params{}, err
	}
	t, err := s.Tactical(ctx)
	if err != nil {
		return engine.Params{}, err
	}
	return engine.Params{
		Weights: engine.DefaultWeights(),
		RawBase: a.RawBase.Decimal, RawSlope: a.RawSlope.Decimal, ClampMin: a.ClampMin.Decimal, ClampMax: a.ClampMax.Decimal,
		BandLowerMult: a.BandLowerMult.Decimal, BandUpperMult: a.BandUpperMult.Decimal, BandUpperCap: a.BandUpperCap.Decimal,
		HardFloor: a.HardFloor.Decimal, HardCap: a.HardCap.Decimal, BootstrapBelow: pl.BootstrapBelow.Decimal,
		CashBase: a.CashBase.Decimal, CashSpan: a.CashSpan.Decimal, OvervaluedMult: a.OvervaluedMult.Decimal,
		CashBandMin: a.CashBandMin.Decimal, CashBandMax: a.CashBandMax.Decimal,
		MinScore: a.MinScore.Decimal, FairValueMaxMonths: a.FairValueMaxMonths, OpportunityMult: a.OpportunityMult.Decimal,
		MinOrderB3: pl.MinOrderB3.Decimal, USBatchMin: pl.USBatchMin.Decimal, RampStages: a.RampStages,
		TrimFraction: a.TrimFraction.Decimal, CooldownDays: a.CooldownDays,
		BetterScoreDelta: a.BetterScoreDelta.Decimal, BetterUpsideDelta: a.BetterUpsideDelta.Decimal,
		TacticalMaxLoss: t.MaxLoss.Decimal, TacticalMinSize: t.MinSize.Decimal, TacticalMaxSize: t.MaxSize.Decimal,
		TacticalMaxOpen: t.MaxOpen,
	}, nil
}

// TaxParams returns the B3 tax rules.
func (s *Service) TaxParams(ctx context.Context) (engine.TaxParams, error) {
	t, err := s.Tax(ctx)
	if err != nil {
		return engine.TaxParams{}, err
	}
	return engine.TaxParams{
		ExemptionLimit: t.ExemptionLimit.Decimal, SwingRate: t.SwingRate.Decimal, DayTradeRate: t.DayTradeRate.Decimal,
		DARFMin: t.DARFMin.Decimal, DARFCode: t.DARFCode, DayTradeDARFCode: t.DayTradeDARFCode,
		DayTradeDARFCodeConfirmed: t.DayTradeDARFCodeConfirmed,
	}, nil
}

// DividendParams returns the dividend withholding rules.
func (s *Service) DividendParams(ctx context.Context) (engine.DividendParams, error) {
	t, err := s.Tax(ctx)
	if err != nil {
		return engine.DividendParams{}, err
	}
	return engine.DividendParams{JCPRate: t.JCPRate.Decimal, DividendRate: t.DividendRate.Decimal,
		DividendMonthly: t.DividendMonthly.Decimal, USRate: t.USRate.Decimal}, nil
}

// Group is a parameter group with its defaults.
type Group[T any] struct {
	Values   T `json:"values"`
	Defaults T `json:"defaults"`
}

// Overview is every group with current values and defaults.
type Overview struct {
	Allocation Group[Allocation] `json:"allocation"`
	Planner    Group[Planner]    `json:"planner"`
	Tax        Group[Tax]        `json:"tax"`
	Tactical   Group[Tactical]   `json:"tactical"`
	AI         Group[AI]         `json:"ai"`
}

// All returns every group.
func (s *Service) All(ctx context.Context) (Overview, error) {
	var out Overview
	var err error
	if out.Allocation.Values, err = s.Allocation(ctx); err != nil {
		return out, err
	}
	if out.Planner.Values, err = s.Planner(ctx); err != nil {
		return out, err
	}
	if out.Tax.Values, err = s.Tax(ctx); err != nil {
		return out, err
	}
	if out.Tactical.Values, err = s.Tactical(ctx); err != nil {
		return out, err
	}
	if out.AI.Values, err = s.AI(ctx); err != nil {
		return out, err
	}
	out.Allocation.Defaults, out.Planner.Defaults, out.Tax.Defaults = DefaultAllocation(), DefaultPlanner(), DefaultTax()
	out.Tactical.Defaults, out.AI.Defaults = DefaultTactical(), DefaultAI()
	return out, nil
}

// StatusReport is the key status shown on the settings page.
type StatusReport struct {
	Keys []config.KeyStatus `json:"keys"`
	// AIUsageUSD is the AI analyst spend this month, as a decimal string.
	AIUsageUSD Dec `json:"ai_usage_usd"`
}

// Status reports which keys are configured (never their values) and the AI
// analyst's month-to-date spend.
func (s *Service) Status(ctx context.Context) (StatusReport, error) {
	keys := append([]config.KeyStatus{}, s.keys...)
	spent := zero
	if s.usage != nil {
		v, err := s.usage(ctx)
		if err != nil {
			return StatusReport{}, err
		}
		spent = v
	}
	return StatusReport{Keys: keys, AIUsageUSD: money.New(spent)}, nil
}
