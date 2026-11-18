package tracking

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func md(s string) money.Decimal    { return money.New(dec(s)) }

// fakeBR is the only fake: the CVM source. It serves fixed annual and quarterly
// figures; everything else is the real services on a temp SQLite database.
type fakeBR struct {
	facts    marketdata.FinancialFacts
	quarters []marketdata.QuarterFact
}

func (f *fakeBR) AnnualFinancials(context.Context, string, []int) (marketdata.FinancialFacts, error) {
	return f.facts, nil
}

func (f *fakeBR) QuarterlyFinancials(context.Context, string, []int) ([]marketdata.QuarterFact, []string, error) {
	return f.quarters, nil, nil
}

type env struct {
	ctx      context.Context
	br       *fakeBR
	assets   *assets.Service
	alerts   *alerts.Service
	val      *valuation.Service
	thesis   *thesis.Service
	fund     *fundamentals.Service
	settings *settings.Service
	svc      *Service
	asset    int64
}

// The base year is FY2024: revenue 1000, net income 100 (10% margin), 100 shares.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	e := &env{ctx: ctx}
	e.assets = assets.NewService(conn)
	e.alerts = alerts.New(conn, clock)
	e.val = valuation.NewService(conn, e.assets, e.alerts, clock)
	e.thesis = thesis.NewService(conn, e.assets, e.alerts, clock)
	e.settings = settings.NewService(conn, nil)
	e.br = &fakeBR{facts: marketdata.FinancialFacts{Source: "cvm", Currency: "BRL", Periods: []marketdata.FinancialPeriod{{
		End: marketdata.Date(2024, 12, 31), Type: marketdata.PeriodAnnual, Filed: "v1",
		Values: map[string]money.Decimal{
			marketdata.ConceptRevenue: md("1000"), marketdata.ConceptNetIncome: md("100"), marketdata.ConceptShares: md("100"),
		},
	}}}}
	e.fund = fundamentals.NewService(conn, e.assets, fundamentals.Sources{BR: e.br}, clock)
	e.svc = NewService(conn, e.assets, e.val, e.alerts, e.thesis, e.settings)
	a, err := e.assets.Create(ctx, assets.CreateInput{Ticker: "TEST3", Market: assets.MarketB3, Kind: assets.KindStock, CNPJ: "33.000.167/0001-01"})
	if err != nil {
		t.Fatal(err)
	}
	e.asset = a.ID
	return e
}

// scenarioVersion stores a scenario DCF version dated asOf whose base scenario
// grows 10% a year to a 10% terminal margin, on 1000 of revenue and 100 shares.
func (e *env) scenarioVersion(t *testing.T, asOf string) valuation.FairValue {
	t.Helper()
	g := dec("0.1")
	sc := func(name, prob string) engine.Scenario {
		return engine.Scenario{Name: name, RevenueGrowth: [engine.DCFYears]decimal.Decimal{g, g, g, g, g},
			TerminalMargin: dec("0.1"), ExitMultiple: dec("10"), DiscountRate: dec("0.1"), Probability: dec(prob)}
	}
	fv, err := e.val.Create(e.ctx, e.asset, valuation.CreateInput{
		Method: valuation.MethodScenarioDCF, AsOf: asOf, Sources: []string{"test"},
		DCF: &engine.DCFInput{BaseRevenue: dec("1000"), Shares: dec("100"), Scenarios: []engine.Scenario{
			sc(engine.ScenarioBear, "0.25"), sc(engine.ScenarioBase, "0.5"), sc(engine.ScenarioBull, "0.25")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fv
}

// expected is the base scenario's expectation for the n-th quarter after FY2024.
func expected(t *testing.T, n int) engine.ExpectedQuarter {
	t.Helper()
	g := dec("0.1")
	m := dec("0.1")
	got, err := engine.ExpectedFor(engine.ExpectedPathInput{
		BaseRevenue: dec("1000"), BaseNetMargin: &m, Shares: dec("100"),
		Base: engine.Scenario{RevenueGrowth: [engine.DCFYears]decimal.Decimal{g, g, g, g, g}, TerminalMargin: dec("0.1")},
	}, n)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// quarterAt builds a stored quarter n quarters after FY2024 whose revenue and
// EPS are the expectation times the given factors.
func quarterAt(t *testing.T, n int, revFactor, epsFactor string) marketdata.QuarterFact {
	t.Helper()
	ends := map[int]time.Time{ // quarters 3 to 7 after 2024-12-31
		3: marketdata.Date(2025, 9, 30), 4: marketdata.Date(2025, 12, 31), 5: marketdata.Date(2026, 3, 31),
		6: marketdata.Date(2026, 6, 30), 7: marketdata.Date(2026, 9, 30),
	}
	e := expected(t, n)
	qn := (n-1)%4 + 1
	return marketdata.QuarterFact{
		End: ends[n], Quarter: qn, Filed: ends[n].AddDate(0, 1, 15).Format("2006-01-02"),
		Values: map[string]money.Decimal{
			marketdata.ConceptRevenue:    money.New(e.Revenue.Mul(dec(revFactor))),
			marketdata.ConceptEPSDiluted: money.New(e.EPS.Mul(dec(epsFactor))),
			marketdata.ConceptNetIncome:  md("1"),
			marketdata.ConceptShares:     md("100"),
		},
	}
}

// load stores the fake quarters through the real refresh (Q4 derivation included).
func (e *env) load(t *testing.T, qs ...marketdata.QuarterFact) {
	t.Helper()
	e.br.quarters = qs
	if _, err := e.fund.Refresh(e.ctx, e.asset); err != nil {
		t.Fatal(err)
	}
}

func (e *env) openAlerts(t *testing.T) []alerts.Alert {
	t.Helper()
	out, err := e.alerts.List(e.ctx, alerts.Filter{Type: AlertMiss})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) report(t *testing.T) Comparison {
	t.Helper()
	r, err := e.svc.Get(e.ctx, e.asset)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *env) evaluate(t *testing.T) {
	t.Helper()
	if err := e.svc.Evaluate(e.ctx); err != nil {
		t.Fatal(err)
	}
}

func near(t *testing.T, name string, got *money.Decimal, want decimal.Decimal) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is missing, want %s", name, want)
		return
	}
	if got.Sub(want).Abs().GreaterThan(dec("0.01")) {
		t.Errorf("%s = %s, want %s", name, got.String(), want)
	}
}

func TestNoScenarioVersionMeansNoComparison(t *testing.T) {
	e := setup(t)
	e.load(t, quarterAt(t, 3, "0.5", "0.5"), quarterAt(t, 4, "0.5", "0.5"))
	// A manual version is not a scenario DCF: it gives no path either.
	if _, err := e.val.Create(e.ctx, e.asset, valuation.CreateInput{Method: valuation.MethodManual, ValuePerShare: ptr(md("10")), AsOf: "2025-06-30"}); err != nil {
		t.Fatal(err)
	}
	r := e.report(t)
	if r.Status != StatusNoFairValue || r.Message == "" {
		t.Errorf("status %q message %q", r.Status, r.Message)
	}
	if len(r.Quarters) != 2 {
		t.Fatalf("quarters: %d", len(r.Quarters))
	}
	for _, q := range r.Quarters {
		if q.ExpectedRevenue != nil || q.FairValueID != nil || q.Miss || q.Note == "" || q.ActualRevenue == nil {
			t.Errorf("quarter %s: %+v", q.PeriodEnd, q)
		}
		if q.RevenueStatus != OutcomeNA || q.EPSStatus != OutcomeNA {
			t.Errorf("quarter %s statuses %s %s", q.PeriodEnd, q.RevenueStatus, q.EPSStatus)
		}
	}
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 0 {
		t.Errorf("alerts without a scenario: %d", len(got))
	}
}

func ptr(v money.Decimal) *money.Decimal { return &v }

func TestComparisonUsesVersionBeforePeriodEnd(t *testing.T) {
	e := setup(t)
	old := e.scenarioVersion(t, "2025-02-01") // before Q1 2025 would end, so it applies to every quarter
	e.load(t, quarterAt(t, 3, "1", "1"), quarterAt(t, 4, "1", "1"))
	// A newer version dated between the two quarters applies from Q4 on.
	newer := e.scenarioVersion(t, "2025-11-01")
	r := e.report(t)
	if len(r.Quarters) != 2 {
		t.Fatalf("quarters: %d", len(r.Quarters))
	}
	q3, q4 := r.Quarters[0], r.Quarters[1]
	if q3.FairValueID == nil || *q3.FairValueID != old.ID || q3.FairValueAsOf != "2025-02-01" {
		t.Errorf("Q3 version: %+v", q3.FairValueID)
	}
	if q4.FairValueID == nil || *q4.FairValueID != newer.ID {
		t.Errorf("Q4 version: %+v", q4.FairValueID)
	}
	// FY2024 is the base for both; the expected path is the engine's.
	near(t, "Q3 expected revenue", q3.ExpectedRevenue, expected(t, 3).Revenue)
	near(t, "Q3 expected EPS", q3.ExpectedEPS, *expected(t, 3).EPS)
	if q3.BasePeriodEnd != "2024-12-31" || q3.QuartersAfter != 3 || q3.BaseRevenue == nil || q3.BaseRevenue.String() != "1000" {
		t.Errorf("Q3 base: %s %d %v", q3.BasePeriodEnd, q3.QuartersAfter, q3.BaseRevenue)
	}
	if q3.RevenueStatus != OutcomeMet || q3.EPSStatus != OutcomeMet || q3.Miss {
		t.Errorf("on-path quarter flagged: %+v", q3)
	}
	if q3.FiscalYear != 2025 || q3.FiscalQuarter != 3 {
		t.Errorf("Q3 label: %d Q%d", q3.FiscalYear, q3.FiscalQuarter)
	}
	// A version dated after the quarter ended does not apply to it.
	if q4.FairValueAsOf != "2025-11-01" {
		t.Errorf("Q4 version date %s", q4.FairValueAsOf)
	}
}

func TestToleranceBoundary(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	// Default tolerance 2%: 1.5% below is met, 3% below is a miss.
	e.load(t, quarterAt(t, 3, "0.985", "1"), quarterAt(t, 4, "0.97", "1"))
	r := e.report(t)
	if r.Quarters[0].RevenueStatus != OutcomeMet || r.Quarters[1].RevenueStatus != OutcomeMiss {
		t.Errorf("statuses %s %s", r.Quarters[0].RevenueStatus, r.Quarters[1].RevenueStatus)
	}
	delta := r.Quarters[1].RevenueDelta
	if delta == nil || delta.Sub(dec("-0.03")).Abs().GreaterThan(dec("0.0001")) {
		t.Errorf("delta = %v, want -0.03", delta)
	}
	// With no tolerance the 1.5% shortfall is a miss too.
	if _, err := e.settings.SetExpectations(e.ctx, settings.Expectations{MissTolerance: md("0"), ConsecutiveQuarters: 2}); err != nil {
		t.Fatal(err)
	}
	r = e.report(t)
	if r.Quarters[0].RevenueStatus != OutcomeMiss {
		t.Errorf("zero tolerance: %s", r.Quarters[0].RevenueStatus)
	}
}

func TestOneMissRaisesNoAlert(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	e.load(t, quarterAt(t, 3, "1", "1"), quarterAt(t, 4, "1.05", "1"), quarterAt(t, 5, "0.9", "1"))
	r := e.report(t)
	if r.RevenueMissStreak != 1 || r.EPSMissStreak != 0 || r.Alerting {
		t.Errorf("streaks %d %d alerting %v", r.RevenueMissStreak, r.EPSMissStreak, r.Alerting)
	}
	if !r.Quarters[2].Miss {
		t.Error("latest quarter should be a miss")
	}
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 0 {
		t.Errorf("alerts after one miss: %d", len(got))
	}
}

func TestTwoConsecutiveMissesAlertAndBeatResolves(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	if _, _, err := e.thesis.Put(e.ctx, e.asset, thesis.PutInput{Text: "Growth compounder"}); err != nil {
		t.Fatal(err)
	}
	e.load(t, quarterAt(t, 3, "1", "1"), quarterAt(t, 4, "0.9", "1"), quarterAt(t, 5, "0.9", "1"))
	r := e.report(t)
	if r.RevenueMissStreak != 2 || !r.Alerting {
		t.Fatalf("streak %d alerting %v", r.RevenueMissStreak, r.Alerting)
	}
	e.evaluate(t)
	open := e.openAlerts(t)
	if len(open) != 1 || open[0].AssetID == nil || *open[0].AssetID != e.asset {
		t.Fatalf("alerts: %+v", open)
	}
	var p struct {
		Ticker        string            `json:"ticker"`
		RevenueStreak int               `json:"revenue_miss_streak"`
		Required      int               `json:"required_quarters"`
		Quarters      []json.RawMessage `json:"quarters"`
		ThesisStatus  string            `json:"thesis_status"`
		Message       string            `json:"message"`
	}
	if err := json.Unmarshal(open[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Ticker != "TEST3" || p.RevenueStreak != 2 || p.Required != 2 || len(p.Quarters) != 2 || p.Message == "" {
		t.Errorf("payload: %s", open[0].Payload)
	}
	// The first alert prompts the review: an active thesis moves to under_review.
	th, err := e.thesis.Get(e.ctx, e.asset)
	if err != nil {
		t.Fatal(err)
	}
	if th.Status != thesis.StatusUnderReview {
		t.Errorf("thesis status %s", th.Status)
	}

	// Evaluating again dedupes: still one alert.
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 1 {
		t.Errorf("alerts after re-evaluate: %d", len(got))
	}
	// A review returns the thesis to active; the open alert does not flip it back.
	if _, err := e.thesis.Review(e.ctx, e.asset, thesis.ReviewInput{Note: "still fine"}); err != nil {
		t.Fatal(err)
	}
	e.evaluate(t)
	if th, _ := e.thesis.Get(e.ctx, e.asset); th.Status != thesis.StatusActive {
		t.Errorf("thesis status after review and re-evaluate: %s", th.Status)
	}

	// The next quarter beats the base: the streak resets and the alert resolves.
	e.load(t, quarterAt(t, 3, "1", "1"), quarterAt(t, 4, "0.9", "1"), quarterAt(t, 5, "0.9", "1"), quarterAt(t, 6, "1.02", "1"))
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 0 {
		t.Errorf("alert still open after a beat: %d", len(got))
	}
	all, err := e.alerts.List(e.ctx, alerts.Filter{State: alerts.StateResolved, Type: AlertMiss})
	if err != nil || len(all) != 1 {
		t.Errorf("resolved alerts: %d %v", len(all), err)
	}
}

func TestEPSMissAloneAlerts(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	// Revenue is on path, EPS is 20% short two quarters in a row.
	e.load(t, quarterAt(t, 4, "1", "0.8"), quarterAt(t, 5, "1", "0.8"))
	r := e.report(t)
	if r.RevenueMissStreak != 0 || r.EPSMissStreak != 2 || !r.Alerting {
		t.Fatalf("streaks %d %d alerting %v", r.RevenueMissStreak, r.EPSMissStreak, r.Alerting)
	}
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 1 {
		t.Errorf("alerts: %d", len(got))
	}
}

func TestConsecutiveQuartersSetting(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	e.load(t, quarterAt(t, 4, "0.9", "1"), quarterAt(t, 5, "0.9", "1"))
	if _, err := e.settings.SetExpectations(e.ctx, settings.Expectations{MissTolerance: md("0.02"), ConsecutiveQuarters: 3}); err != nil {
		t.Fatal(err)
	}
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 0 {
		t.Errorf("alert with 2 misses and 3 required: %d", len(got))
	}
	e.load(t, quarterAt(t, 4, "0.9", "1"), quarterAt(t, 5, "0.9", "1"), quarterAt(t, 6, "0.9", "1"))
	e.evaluate(t)
	if got := e.openAlerts(t); len(got) != 1 {
		t.Errorf("alerts with 3 misses: %d", len(got))
	}
}

func TestVersionAfterQuartersGivesNoExpectationForThem(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2026-08-01") // dated after Q3 2025 to Q2 2026 ended
	e.load(t, quarterAt(t, 3, "0.5", "0.5"), quarterAt(t, 6, "0.5", "0.5"), quarterAt(t, 7, "0.5", "0.5"))
	r := e.report(t)
	if r.Status != StatusOK {
		t.Errorf("status %s", r.Status)
	}
	for i, wantExpected := range []bool{false, false, true} {
		q := r.Quarters[i]
		if (q.ExpectedRevenue != nil) != wantExpected {
			t.Errorf("quarter %s: expected revenue %v, want present %v (note %q)", q.PeriodEnd, q.ExpectedRevenue, wantExpected, q.Note)
		}
		if !wantExpected && q.Note == "" {
			t.Errorf("quarter %s has no note", q.PeriodEnd)
		}
	}
	// Only the last quarter is compared, so there is no streak to alert on.
	if r.Alerting {
		t.Error("alerting with a single comparable quarter")
	}
}

func TestFourthQuarterIsDerivedFromTheAnnualFigure(t *testing.T) {
	e := setup(t)
	e.scenarioVersion(t, "2025-02-01")
	// FY2025: annual revenue 1100 and net income 110; Q3 year to date 800 and 80.
	e.br.facts.Periods = append(e.br.facts.Periods, marketdata.FinancialPeriod{
		End: marketdata.Date(2025, 12, 31), Type: marketdata.PeriodAnnual, Filed: "v1",
		Values: map[string]money.Decimal{
			marketdata.ConceptRevenue: md("1100"), marketdata.ConceptNetIncome: md("110"), marketdata.ConceptShares: md("100"),
		},
	})
	q3 := quarterAt(t, 3, "1", "1")
	q3.YTD = map[string]money.Decimal{marketdata.ConceptRevenue: md("800"), marketdata.ConceptNetIncome: md("80")}
	e.load(t, q3)
	r := e.report(t)
	if len(r.Quarters) != 2 {
		t.Fatalf("quarters: %d", len(r.Quarters))
	}
	q4 := r.Quarters[1]
	if q4.PeriodEnd != "2025-12-31" || q4.FiscalQuarter != 4 || !q4.Derived {
		t.Errorf("Q4: %+v", q4)
	}
	near(t, "Q4 revenue", q4.ActualRevenue, dec("300")) // 1100 - 800
	near(t, "Q4 EPS", q4.ActualEPS, dec("0.3"))         // (110 - 80) / 100 shares
	// FY2025 is now the newest year, but versions are matched to the base at
	// their own date: the 2025-02-01 version still uses FY2024.
	if q4.BasePeriodEnd != "2024-12-31" {
		t.Errorf("Q4 base %s", q4.BasePeriodEnd)
	}
}

func TestUnsupportedAndEmpty(t *testing.T) {
	e := setup(t)
	r := e.report(t)
	if r.Status != StatusNoQuarters || r.Message == "" || len(r.Quarters) != 0 {
		t.Errorf("empty: %s %q", r.Status, r.Message)
	}
	etf, err := e.assets.Create(e.ctx, assets.CreateInput{Ticker: "BOVA11", Market: assets.MarketB3, Kind: assets.KindETF})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Get(e.ctx, etf.ID)
	if err != nil || got.Status != StatusUnsupported {
		t.Errorf("etf: %s %v", got.Status, err)
	}
}
