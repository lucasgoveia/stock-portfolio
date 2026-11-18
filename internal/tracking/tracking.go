// Package tracking compares each reported quarter with the base scenario of
// the fair value that was current before it (spec §12.4) and raises
// expected_vs_actual_miss after consecutive misses. The math is in
// internal/engine (ExpectedFor, Compare, MissStreak).
package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// AlertMiss is the alert type this package raises.
const AlertMiss = "expected_vs_actual_miss"

// Comparison statuses.
const (
	StatusOK            = "ok"
	StatusUnsupported   = "unsupported"   // ETFs and BDRs have no fundamentals
	StatusNoQuarters    = "no_quarters"   // no quarterly actuals stored yet
	StatusNoFairValue   = "no_fair_value" // quarters exist but no scenario DCF version
	dateLayout          = "2006-01-02"
	revenueDisplayScale = 2
)

// Outcomes of one metric in one quarter.
const (
	OutcomeMet  = string(engine.OutcomeMet)
	OutcomeMiss = string(engine.OutcomeMiss)
	OutcomeNA   = string(engine.OutcomeNA)
)

// ParamSource supplies the editable rules (settings.Service).
type ParamSource interface {
	Expectations(ctx context.Context) (settings.Expectations, error)
}

// Service builds the comparison and raises the alert.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	val    *valuation.Service
	alerts *alerts.Service
	thesis *thesis.Service
	params ParamSource
}

// NewService builds a Service. alerts and thesis may be nil (no alert, no
// thesis status change); a nil params source uses the defaults.
func NewService(conn db.DBTX, a *assets.Service, v *valuation.Service, al *alerts.Service, th *thesis.Service, p ParamSource) *Service {
	return &Service{q: db.New(conn), assets: a, val: v, alerts: al, thesis: th, params: p}
}

// QuarterComparison is one reported quarter against the base scenario of the fair value
// version that preceded it.
type QuarterComparison struct {
	PeriodEnd     string `json:"period_end"`
	FiscalQuarter int    `json:"fiscal_quarter" minimum:"1" maximum:"4"`
	FiscalYear    int    `json:"fiscal_year" doc:"Year the fiscal year of this quarter ends"`
	Source        string `json:"source"`
	Filed         string `json:"filed" doc:"Filing date, or the document version when unknown"`
	Derived       bool   `json:"derived" doc:"Q4 computed as the annual figure minus the Q3 year to date"`

	ActualRevenue   *money.Decimal `json:"actual_revenue,omitempty"`
	ExpectedRevenue *money.Decimal `json:"expected_revenue,omitempty"`
	RevenueDelta    *money.Decimal `json:"revenue_delta,omitempty" doc:"(actual - expected) / expected, as a fraction"`
	RevenueStatus   string         `json:"revenue_status" enum:"met,miss,na"`

	ActualEPS   *money.Decimal `json:"actual_eps,omitempty"`
	ExpectedEPS *money.Decimal `json:"expected_eps,omitempty"`
	EPSDelta    *money.Decimal `json:"eps_delta,omitempty" doc:"(actual - expected) / |expected|, as a fraction"`
	EPSStatus   string         `json:"eps_status" enum:"met,miss,na"`

	// Miss is true when revenue or EPS is below the base scenario by more than the tolerance.
	Miss bool `json:"miss"`

	FairValueID   *int64         `json:"fair_value_id,omitempty" doc:"Scenario DCF version the expectation comes from"`
	FairValueAsOf string         `json:"fair_value_as_of,omitempty"`
	BasePeriodEnd string         `json:"base_period_end,omitempty" doc:"Fiscal year end of the base revenue"`
	BaseRevenue   *money.Decimal `json:"base_revenue,omitempty" doc:"Annual revenue at the version date, from fundamentals"`
	QuartersAfter int            `json:"quarters_after_base,omitempty" doc:"Quarter number on the 5-year path (1 to 20)"`
	Note          string         `json:"note,omitempty" doc:"Why there is no comparison, or what is missing"`
}

// Comparison is the expected-versus-actual view of one asset.
type Comparison struct {
	AssetID  int64  `json:"asset_id"`
	Ticker   string `json:"ticker"`
	Currency string `json:"currency"`
	Status   string `json:"status" enum:"ok,unsupported,no_quarters,no_fair_value"`
	Message  string `json:"message"`

	MissTolerance       money.Decimal `json:"miss_tolerance" doc:"Fraction below the base scenario still counted as met"`
	ConsecutiveQuarters int           `json:"consecutive_quarters" doc:"Consecutive misses that raise the alert"`
	RevenueMissStreak   int           `json:"revenue_miss_streak" doc:"Consecutive revenue misses ending at the latest quarter"`
	EPSMissStreak       int           `json:"eps_miss_streak" doc:"Consecutive EPS misses ending at the latest quarter"`
	Alerting            bool          `json:"alerting" doc:"True when a streak reached the required number of quarters"`

	Quarters []QuarterComparison `json:"quarters" doc:"Oldest first"`
	Notes    []string            `json:"notes"`
}

func (s *Service) rules(ctx context.Context) (settings.Expectations, error) {
	if s.params == nil {
		return settings.DefaultExpectations(), nil
	}
	return s.params.Expectations(ctx)
}

// annual is one stored fiscal year.
type annual struct {
	end       string
	revenue   decimal.Decimal
	netIncome *decimal.Decimal
}

func (s *Service) annuals(ctx context.Context, assetID int64) ([]annual, error) {
	rows, err := s.q.ListFundamentalFacts(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list fundamental facts: %w", err)
	}
	byEnd := map[string]*annual{}
	var order []string
	for _, r := range rows {
		if r.PeriodType != "annual" || (r.Concept != "revenue" && r.Concept != "net_income") {
			continue
		}
		v, err := decimal.NewFromString(r.Value)
		if err != nil {
			continue
		}
		a, ok := byEnd[r.PeriodEnd]
		if !ok {
			a = &annual{end: r.PeriodEnd}
			byEnd[r.PeriodEnd] = a
			order = append(order, r.PeriodEnd)
		}
		if r.Concept == "revenue" {
			a.revenue = v
		} else {
			a.netIncome = &v
		}
	}
	out := make([]annual, 0, len(order))
	for _, e := range order { // ListFundamentalFacts is ordered by period_end
		if byEnd[e].revenue.IsPositive() {
			out = append(out, *byEnd[e])
		}
	}
	return out, nil
}

// baseAt returns the latest fiscal year ending on or before asOf.
func baseAt(as []annual, asOf string) (annual, bool) {
	for i := len(as) - 1; i >= 0; i-- {
		if as[i].end <= asOf {
			return as[i], true
		}
	}
	return annual{}, false
}

// dcfVersion is a scenario DCF version with its base scenario decoded.
type dcfVersion struct {
	id    int64
	asOf  string
	dcf   engine.DCFInput
	base  engine.Scenario
	valid bool
}

func (s *Service) scenarioVersions(ctx context.Context, assetID int64) ([]dcfVersion, error) {
	list, err := s.val.List(ctx, assetID) // newest first
	if err != nil {
		return nil, err
	}
	var out []dcfVersion
	for _, fv := range list {
		if fv.Method != valuation.MethodScenarioDCF {
			continue
		}
		v := dcfVersion{id: fv.ID, asOf: fv.AsOf}
		var in struct {
			DCF *engine.DCFInput `json:"dcf"`
		}
		if json.Unmarshal(fv.Inputs, &in) == nil && in.DCF != nil {
			for _, sc := range in.DCF.Scenarios {
				if sc.Name == engine.ScenarioBase {
					v.dcf, v.base, v.valid = *in.DCF, sc, true
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func parseDec(v string) (decimal.Decimal, bool) {
	if v == "" {
		return decimal.Zero, false
	}
	d, err := decimal.NewFromString(v)
	return d, err == nil
}

func roundPtr(d decimal.Decimal, places int32) *money.Decimal {
	v := money.New(d.Round(places))
	return &v
}

// Get returns the comparison of every stored quarter of an asset.
func (s *Service) Get(ctx context.Context, assetID int64) (Comparison, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return Comparison{}, err
	}
	rules, err := s.rules(ctx)
	if err != nil {
		return Comparison{}, err
	}
	r := Comparison{
		AssetID: a.ID, Ticker: a.Ticker, Currency: a.Currency, MissTolerance: rules.MissTolerance,
		ConsecutiveQuarters: rules.ConsecutiveQuarters, Quarters: []QuarterComparison{}, Notes: []string{},
	}
	if a.Kind != assets.KindStock {
		r.Status, r.Message = StatusUnsupported, "Fundamentals cover stocks only (not ETFs or BDRs)."
		return r, nil
	}
	rows, err := s.q.ListFundamentalQuarters(ctx, a.ID)
	if err != nil {
		return Comparison{}, fmt.Errorf("list quarters: %w", err)
	}
	if len(rows) == 0 {
		r.Status, r.Message = StatusNoQuarters, "No quarterly results stored yet. Refresh the fundamentals to load them."
		return r, nil
	}
	versions, err := s.scenarioVersions(ctx, a.ID)
	if err != nil {
		return Comparison{}, err
	}
	annuals, err := s.annuals(ctx, a.ID)
	if err != nil {
		return Comparison{}, err
	}
	r.Status = StatusOK
	if len(versions) == 0 {
		r.Status = StatusNoFairValue
		r.Message = "No scenario DCF fair value version exists. Create one to compare reported quarters with its base scenario."
	}

	var rev, eps []engine.Outcome
	for _, row := range rows {
		q := s.compare(row, versions, annuals, rules.MissTolerance.Decimal)
		r.Quarters = append(r.Quarters, q)
		rev = append(rev, engine.Outcome(q.RevenueStatus))
		eps = append(eps, engine.Outcome(q.EPSStatus))
	}
	r.RevenueMissStreak, r.EPSMissStreak = engine.MissStreak(rev), engine.MissStreak(eps)
	r.Alerting = r.RevenueMissStreak >= rules.ConsecutiveQuarters || r.EPSMissStreak >= rules.ConsecutiveQuarters
	if r.Status == StatusOK && !hasComparison(r.Quarters) {
		r.Message = "No stored quarter ends after a scenario DCF version date yet."
	}
	return r, nil
}

func hasComparison(qs []QuarterComparison) bool {
	for _, q := range qs {
		if q.FairValueID != nil && q.ExpectedRevenue != nil {
			return true
		}
	}
	return false
}

// fiscalYear is the year in which the fiscal year of a quarter ends.
func fiscalYear(end time.Time, quarter int) int {
	return end.AddDate(0, (4-quarter)*3, 0).Year()
}

// compare builds one quarter. The expectation comes from the latest scenario
// DCF version dated before the quarter's period end.
func (s *Service) compare(row db.FundamentalQuarter, versions []dcfVersion, annuals []annual, tol decimal.Decimal) QuarterComparison {
	end, _ := time.Parse(dateLayout, row.PeriodEnd)
	q := QuarterComparison{
		PeriodEnd: row.PeriodEnd, FiscalQuarter: int(row.FiscalQuarter), FiscalYear: fiscalYear(end, int(row.FiscalQuarter)),
		Source: row.Source, Filed: row.Filed, Derived: row.Derived == 1,
		RevenueStatus: OutcomeNA, EPSStatus: OutcomeNA,
	}
	actualRev, haveRev := parseDec(row.Revenue)
	actualEPS, haveEPS := parseDec(row.Eps)
	if haveRev {
		q.ActualRevenue = roundPtr(actualRev, revenueDisplayScale)
	}
	if haveEPS {
		q.ActualEPS = roundPtr(actualEPS, 4)
	}

	var ver *dcfVersion
	for i := range versions {
		if versions[i].asOf < row.PeriodEnd {
			ver = &versions[i]
			break
		}
	}
	switch {
	case len(versions) == 0:
		q.Note = "No scenario DCF fair value version."
		return q
	case ver == nil:
		q.Note = "No scenario DCF version dated before this quarter ended."
		return q
	case !ver.valid:
		q.Note = fmt.Sprintf("The scenario DCF version of %s has no readable base scenario.", ver.asOf)
		return q
	}
	q.FairValueID, q.FairValueAsOf = &ver.id, ver.asOf
	base, ok := baseAt(annuals, ver.asOf)
	if !ok {
		q.Note = "No annual revenue stored for a fiscal year ending before the version date."
		return q
	}
	baseEnd, _ := time.Parse(dateLayout, base.end)
	q.BasePeriodEnd, q.BaseRevenue = base.end, roundPtr(base.revenue, revenueDisplayScale)
	n := engine.QuartersAfter(int(end.Sub(baseEnd).Hours() / 24))
	q.QuartersAfter = n

	in := engine.ExpectedPathInput{BaseRevenue: base.revenue, Shares: ver.dcf.Shares, Base: ver.base}
	if base.netIncome != nil {
		m := base.netIncome.Div(base.revenue)
		in.BaseNetMargin = &m
	}
	exp, err := engine.ExpectedFor(in, n)
	if err != nil {
		if n < 1 {
			q.Note = "This quarter is not after the base fiscal year."
		} else {
			q.Note = fmt.Sprintf("Quarter %d is beyond the 5-year path of the scenario.", n)
		}
		return q
	}
	if haveRev {
		q.ExpectedRevenue = roundPtr(exp.Revenue, revenueDisplayScale)
		delta, out := engine.Compare(actualRev, exp.Revenue, tol)
		q.RevenueStatus, q.RevenueDelta = string(out), deltaPtr(delta)
	}
	switch {
	case exp.EPS == nil:
		q.Note = "No expected EPS: the base year has no net income or the version has no share count."
	case haveEPS:
		q.ExpectedEPS = roundPtr(*exp.EPS, 4)
		delta, out := engine.Compare(actualEPS, *exp.EPS, tol)
		q.EPSStatus, q.EPSDelta = string(out), deltaPtr(delta)
	default:
		q.ExpectedEPS = roundPtr(*exp.EPS, 4)
	}
	q.Miss = q.RevenueStatus == OutcomeMiss || q.EPSStatus == OutcomeMiss
	return q
}

func deltaPtr(d *decimal.Decimal) *money.Decimal {
	if d == nil {
		return nil
	}
	v := money.New(*d)
	return &v
}

// Evaluate raises expected_vs_actual_miss for holdings and watchlist assets
// whose latest quarters missed the base scenario for the required number of
// consecutive quarters, and resolves the alerts whose condition cleared. The
// first time the alert opens, an active thesis moves to under_review so the
// review is prompted (spec §12.4).
func (s *Service) Evaluate(ctx context.Context) error {
	if s.alerts == nil {
		return nil
	}
	var active []string
	var firstErr error
	for _, status := range []string{assets.StatusHolding, assets.StatusWatchlist} {
		list, err := s.assets.List(ctx, assets.Filter{Status: status})
		if err != nil {
			return err
		}
		for _, a := range list {
			if a.Kind != assets.KindStock {
				continue
			}
			rep, err := s.Get(ctx, a.ID)
			if err != nil {
				slog.Warn("tracking: comparison failed", "asset", a.ID, "err", err)
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", a.Ticker, err)
				}
				// Keep an existing alert rather than resolving it on a transient error.
				id := a.ID
				active = append(active, alerts.Key(AlertMiss, &id, ""))
				continue
			}
			if !rep.Alerting {
				continue
			}
			id := a.ID
			fresh, err := s.isNew(ctx, id)
			if err != nil {
				return err
			}
			if _, err := s.alerts.Upsert(ctx, AlertMiss, &id, "", s.payload(ctx, a, rep)); err != nil {
				return err
			}
			active = append(active, alerts.Key(AlertMiss, &id, ""))
			if fresh {
				s.markUnderReview(ctx, id)
			}
		}
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertMiss, active); err != nil {
		return err
	}
	return firstErr
}

// isNew reports whether the asset has no open alert of this type yet.
func (s *Service) isNew(ctx context.Context, assetID int64) (bool, error) {
	list, err := s.alerts.List(ctx, alerts.Filter{State: alerts.StateAll, Type: AlertMiss, AssetID: &assetID})
	if err != nil {
		return false, err
	}
	for _, al := range list {
		if al.State != alerts.StateResolved {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) markUnderReview(ctx context.Context, assetID int64) {
	if s.thesis == nil {
		return
	}
	t, err := s.thesis.Get(ctx, assetID)
	if err != nil {
		if !errors.Is(err, thesis.ErrNotFound) {
			slog.Warn("tracking: reading thesis", "asset", assetID, "err", err)
		}
		return
	}
	if t.Status != thesis.StatusActive {
		return
	}
	if _, err := s.thesis.SetStatus(ctx, assetID, thesis.StatusUnderReview); err != nil {
		slog.Warn("tracking: setting thesis under review", "asset", assetID, "err", err)
	}
}

// payload documents the alert: the streaks and the quarters that count.
func (s *Service) payload(ctx context.Context, a db.Asset, rep Comparison) map[string]any {
	keep := rep.ConsecutiveQuarters
	if rep.RevenueMissStreak > keep {
		keep = rep.RevenueMissStreak
	}
	if rep.EPSMissStreak > keep {
		keep = rep.EPSMissStreak
	}
	if keep > len(rep.Quarters) {
		keep = len(rep.Quarters)
	}
	recent := rep.Quarters[len(rep.Quarters)-keep:]
	var parts []string
	if rep.RevenueMissStreak >= rep.ConsecutiveQuarters {
		parts = append(parts, fmt.Sprintf("revenue %d quarters", rep.RevenueMissStreak))
	}
	if rep.EPSMissStreak >= rep.ConsecutiveQuarters {
		parts = append(parts, fmt.Sprintf("EPS %d quarters", rep.EPSMissStreak))
	}
	p := map[string]any{
		"asset_id": a.ID, "ticker": a.Ticker, "currency": a.Currency,
		"revenue_miss_streak": rep.RevenueMissStreak, "eps_miss_streak": rep.EPSMissStreak,
		"required_quarters": rep.ConsecutiveQuarters, "miss_tolerance": rep.MissTolerance.String(),
		"quarters": recent,
		"message":  "Reported results are below the base scenario for " + strings.Join(parts, " and ") + " in a row. Review the thesis and the fair value.",
	}
	if s.thesis != nil {
		if t, err := s.thesis.Get(ctx, a.ID); err == nil {
			p["thesis_status"] = t.Status
		}
	}
	return p
}
