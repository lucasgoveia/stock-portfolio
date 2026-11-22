package fundamentals

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/capm"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

const petrobrasCNPJ = "33.000.167/0001-01"

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// facts builds annual periods for the given years with simple, hand-checkable
// numbers: revenue 1000, EBIT 150 + 25 per year step, shares 100.
func fakeFacts(source string, years ...int) marketdata.FinancialFacts {
	f := marketdata.FinancialFacts{Source: source, Entity: "ACME S.A.", Currency: "BRL"}
	for i, y := range years {
		v := func(s string) money.Decimal { return money.New(dec(s)) }
		ni := []string{"80", "90", "100", "110", "120"}[i%5]
		f.Periods = append(f.Periods, marketdata.FinancialPeriod{
			End: marketdata.Date(y, 12, 31), Type: marketdata.PeriodAnnual, Filed: "v1",
			Values: map[string]money.Decimal{
				marketdata.ConceptRevenue: v("1000"), marketdata.ConceptEBIT: v("200"), marketdata.ConceptNetIncome: v(ni),
				marketdata.ConceptEquity: v("800"), marketdata.ConceptCash: v("100"), marketdata.ConceptDebt: v("300"),
				marketdata.ConceptOCF: v("180"), marketdata.ConceptCapex: v("80"), marketdata.ConceptDA: v("50"),
				marketdata.ConceptShares: v("100"),
			},
		})
	}
	return f
}

type fakeBR struct {
	facts marketdata.FinancialFacts
	err   error
	calls int
	years [][]int
}

func (f *fakeBR) AnnualFinancials(_ context.Context, cnpj string, years []int) (marketdata.FinancialFacts, error) {
	f.calls++
	f.years = append(f.years, years)
	return f.facts, f.err
}

type fakeUS struct {
	facts    marketdata.FinancialFacts
	cik      string
	cikCalls int
	calls    int
}

func (f *fakeUS) CIKForTicker(context.Context, string) (string, error) {
	f.cikCalls++
	return f.cik, nil
}

func (f *fakeUS) Financials(context.Context, string) (marketdata.FinancialFacts, error) {
	f.calls++
	return f.facts, nil
}

type fakeDivs struct {
	rows []marketdata.CashDividend
	err  error
}

func (f fakeDivs) CorporateActions(context.Context, string, time.Time, time.Time) (marketdata.CorporateActions, error) {
	return marketdata.CorporateActions{CashDividends: f.rows}, f.err
}

type env struct {
	svc    *Service
	assets *assets.Service
	q      *db.Queries
	now    *time.Time
	br     *fakeBR
	us     *fakeUS
	divs   *fakeDivs
}

func setup(t *testing.T) *env {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	e := &env{
		assets: assets.NewService(conn), q: db.New(conn), now: &now,
		br: &fakeBR{facts: fakeFacts("cvm", 2023, 2024, 2025)},
		us: &fakeUS{cik: "0000320193"},
		divs: &fakeDivs{rows: []marketdata.CashDividend{
			{Label: "DIVIDENDO", Rate: money.New(dec("0.7")), LastDatePrior: marketdata.Date(2026, 3, 10)},
			{Label: "JCP", Rate: money.New(dec("0.5")), LastDatePrior: marketdata.Date(2026, 9, 10)},
			{Label: "DIVIDENDO", Rate: money.New(dec("9")), LastDatePrior: marketdata.Date(2025, 6, 10)}, // too old
		}},
	}
	e.svc = NewService(conn, e.assets, Sources{BR: e.br, US: e.us, Dividends: e.divs}, func() time.Time { return *e.now })
	return e
}

func (e *env) asset(t *testing.T, ticker, market, kind, cnpj string) db.Asset {
	t.Helper()
	a, err := e.assets.Create(context.Background(), assets.CreateInput{Ticker: ticker, Market: market, Kind: kind, CNPJ: cnpj})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *env) price(t *testing.T, id int64, date, source, close string) {
	t.Helper()
	if err := e.q.UpsertAssetPrice(context.Background(), db.UpsertAssetPriceParams{
		AssetID: id, RefDate: date, Source: source, Close: close, FetchedAt: "2026-11-18T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
}

func eq(t *testing.T, name string, got *money.Decimal, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is missing, want %s", name, want)
		return
	}
	if !got.Equal(dec(want)) {
		t.Errorf("%s = %s, want %s", name, got.String(), want)
	}
}

func TestRefreshAndMetricsB3(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	e.price(t, a.ID, "2024-12-30", "cotahist", "12") // 2024 year-end within the window
	e.price(t, a.ID, "2025-12-30", "cotahist", "15")

	before, err := e.svc.Get(ctx, a.ID, Params{})
	if err != nil || before.Status != StatusNotFetched {
		t.Fatalf("before refresh: %+v %v", before.Status, err)
	}
	r, err := e.svc.Refresh(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusOK || r.Source != "cvm" || r.AsOf != "2025-12-31" || r.FetchedAt != "2026-11-18T09:00:00Z" {
		t.Fatalf("report header: %+v", r)
	}
	m := r.FundamentalMetrics
	// price 20 x 100 shares = market cap 2000; net debt 300-100 = 200; EV 2200.
	eq(t, "market cap", m.MarketCap, "2000")
	eq(t, "EV", m.EnterpriseValue, "2200")
	eq(t, "EPS", m.EPS, "1")
	eq(t, "P/E", m.PE, "20")
	eq(t, "EV/EBIT", m.EVEBIT, "11")
	eq(t, "FCF", m.FCF, "100")
	eq(t, "FCF yield", m.FCFYield, "0.05")
	eq(t, "EBITDA", m.EBITDA, "250")
	eq(t, "net debt/EBITDA", m.NetDebtEBITDA, "0.8")
	eq(t, "BVPS", m.BVPS, "8")
	// brapi trailing dividends: 0.7 + 0.5 (the 2025-06 payment is outside 12 months).
	eq(t, "DPS", m.DividendsPerShare, "1.2")
	eq(t, "dividend yield", m.DividendYield, "0.06")

	if len(r.History) != 3 {
		t.Fatalf("history: %d", len(r.History))
	}
	last := r.History[2]
	eq(t, "EBIT margin", last.EBITMargin, "0.2")
	eq(t, "net margin", last.NetMargin, "0.1")

	// P/E at 2024: 12 x 100 / 90; 2025: 15 x 100 / 100 = 15; current 20.
	pe := r.Ranges.PE
	if pe == nil || len(pe.Points) != 2 {
		t.Fatalf("PE range: %+v", pe)
	}
	eq(t, "PE min", pe.Min, "13.33")
	eq(t, "PE max", pe.Max, "20")
	eq(t, "PE position", pe.Position, "1")

	b := r.Helpers.Bazin
	eq(t, "Bazin", b.Value, "20") // 1.2 / 6%
	g := r.Helpers.Graham
	eq(t, "Graham", g.Value, "13.4164") // sqrt(22.5 x 1 x 8)
	rd := r.Helpers.ReverseDCF
	if rd.Status != "ok" || rd.ImpliedGrowth == nil {
		t.Fatalf("reverse DCF: %+v", rd)
	}
	eq(t, "default margin", &rd.TerminalMargin, "0.2")

	// First fetch asks for five years plus the current one; the next only two.
	if len(e.br.years[0]) != MaxHistory+1 {
		t.Errorf("first fetch years: %v", e.br.years[0])
	}
}

func TestRefetchUsesTwoYearsOnceHistoryIsFull(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.br.facts = fakeFacts("cvm", 2021, 2022, 2023, 2024, 2025)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	for i := 0; i < 2; i++ {
		if _, err := e.svc.Refresh(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.br.years[0]) != MaxHistory+1 || len(e.br.years[1]) != 2 || e.br.years[1][0] != 2026 {
		t.Errorf("years: %v", e.br.years)
	}
}

func TestParamsOverrideAndDrafts(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	if _, err := e.svc.Refresh(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	y := dec("0.08")
	r, err := e.svc.Get(ctx, a.ID, Params{BazinYield: &y})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "Bazin at 8%", r.Helpers.Bazin.Value, "15")

	d, err := e.svc.Draft(ctx, a.ID, "bazin", Params{BazinYield: &y})
	if err != nil {
		t.Fatal(err)
	}
	if d.Method != "bazin" || !d.ValuePerShare.Equal(dec("15")) || d.Currency != "BRL" || d.AsOf != "2026-11-18" ||
		d.Inputs["dividends_per_share"] != "1.2" || len(d.Sources) != 2 {
		t.Errorf("bazin draft: %+v", d)
	}
	g, err := e.svc.Draft(ctx, a.ID, "graham", Params{})
	if err != nil || !g.ValuePerShare.Equal(dec("13.4164")) || g.Inputs["bvps"] != "8" {
		t.Errorf("graham draft: %+v %v", g, err)
	}
	if _, err := e.svc.Draft(ctx, a.ID, "manual", Params{}); !errors.Is(err, ErrValidation) {
		t.Errorf("unknown method: %v", err)
	}
}

func TestNegativeEarningsLeaveHelpersUndefined(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	f := fakeFacts("cvm", 2025)
	f.Periods[0].Values[marketdata.ConceptNetIncome] = money.New(dec("-50"))
	f.Periods[0].Values[marketdata.ConceptEBIT] = money.New(dec("-10"))
	e.br.facts = f
	e.divs.rows = nil
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	r, err := e.svc.Refresh(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.FundamentalMetrics.PE != nil || r.FundamentalMetrics.EVEBIT != nil || r.Helpers.Graham.Value != nil || r.Helpers.Bazin.Value != nil {
		t.Errorf("expected undefined multiples and helpers: %+v", r.FundamentalMetrics)
	}
	if _, err := e.svc.Draft(ctx, a.ID, "graham", Params{}); !errors.Is(err, ErrValidation) {
		t.Errorf("graham draft should be refused: %v", err)
	}
	if len(r.Notes) == 0 {
		t.Error("expected explanatory notes")
	}
}

func TestIdentifierAndSupportRules(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "WEGE3", "B3", "stock", "")
	r, err := e.svc.Get(ctx, a.ID, Params{})
	if err != nil || r.Status != StatusNeedsIdentifier || r.IdentifierKind != "cnpj" {
		t.Fatalf("missing CNPJ: %+v %v", r.Status, err)
	}
	if _, err := e.svc.Refresh(ctx, a.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("refresh without CNPJ: %v", err)
	}
	etf := e.asset(t, "BOVA11", "B3", "etf", "")
	if r, _ := e.svc.Get(ctx, etf.ID, Params{}); r.Status != StatusUnsupported {
		t.Errorf("etf status %s", r.Status)
	}
	if _, err := e.svc.Refresh(ctx, etf.ID); !errors.Is(err, ErrValidation) {
		t.Errorf("etf refresh: %v", err)
	}
	if _, err := e.svc.Get(ctx, 9999, Params{}); !errors.Is(err, assets.ErrNotFound) {
		t.Errorf("missing asset: %v", err)
	}
}

func TestUSResolvesAndCachesCIK(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	f := fakeFacts("sec", 2023, 2024, 2025)
	for i := range f.Periods {
		f.Periods[i].Values[marketdata.ConceptEPSDiluted] = money.New(dec("2"))
		f.Periods[i].Values[marketdata.ConceptDPS] = money.New(dec("0.5"))
	}
	f.Currency = "USD"
	e.us.facts = f
	a := e.asset(t, "AAPL", "US", "stock", "")
	e.price(t, a.ID, "2026-11-17", "tiingo", "30")
	r, err := e.svc.Refresh(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Identifier != "0000320193" || r.Source != "sec" || e.us.cikCalls != 1 {
		t.Errorf("cik: %q source %q calls %d", r.Identifier, r.Source, e.us.cikCalls)
	}
	// Reported diluted EPS is used (2), annual declared DPS (0.5) feeds the yield.
	eq(t, "P/E", r.FundamentalMetrics.PE, "15")
	eq(t, "dividend yield", r.FundamentalMetrics.DividendYield, "0.0167")
	if _, err := e.svc.Refresh(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if e.us.cikCalls != 1 || e.us.calls != 2 {
		t.Errorf("cik resolved again: cik calls %d, fetches %d", e.us.cikCalls, e.us.calls)
	}
	saved, _ := e.assets.Get(ctx, a.ID)
	if saved.Cik != "0000320193" {
		t.Errorf("cik not saved: %q", saved.Cik)
	}
}

func TestFailedRefreshKeepsOldDataAndShowsError(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	if _, err := e.svc.Refresh(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	e.br.err = errors.New("cvm down")
	*e.now = e.now.Add(time.Hour)
	if _, err := e.svc.Refresh(ctx, a.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
	r, err := e.svc.Get(ctx, a.ID, Params{})
	if err != nil || r.Status != StatusOK || r.AsOf != "2025-12-31" || r.LastError == "" {
		t.Errorf("after failure: status %s as_of %s last_error %q err %v", r.Status, r.AsOf, r.LastError, err)
	}
	// An empty answer is also an error.
	e.br.err, e.br.facts = nil, marketdata.FinancialFacts{Source: "cvm"}
	if _, err := e.svc.Refresh(ctx, a.ID); !errors.Is(err, ErrUpstream) {
		t.Errorf("empty facts: %v", err)
	}
}

func TestDividendFeedFailureFallsBackToLocalDividends(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.divs.err = errors.New("brapi limited")
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	if _, err := e.q.CreateDividend(ctx, db.CreateDividendParams{
		AssetID: a.ID, Kind: "dividend", RecordDate: "2026-05-10", Quantity: "100", PerShare: "0.9", Gross: "90", Net: "90",
		Currency: "BRL", FxRate: "1", Status: "confirmed", Source: "manual",
	}); err != nil {
		t.Fatal(err)
	}
	r, err := e.svc.Refresh(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "DPS from local dividends", r.FundamentalMetrics.DividendsPerShare, "0.9")
	found := false
	for _, n := range r.Notes {
		if n != "" && len(n) > 10 && n[:10] == "trailing d" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a note about the failed dividend feed: %v", r.Notes)
	}
}

func TestRefreshStale(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.asset(t, "WEGE3", "B3", "stock", "") // no CNPJ: skipped
	e.asset(t, "BOVA11", "B3", "etf", "")  // not a stock

	note, err := e.svc.RefreshStale(ctx, *e.now)
	if err != nil || note != "1 refreshed, 0 fresh, 1 without CNPJ" {
		t.Fatalf("first run: %q %v", note, err)
	}
	*e.now = e.now.Add(3 * 24 * time.Hour)
	note, err = e.svc.RefreshStale(ctx, *e.now)
	if err != nil || note != "0 refreshed, 1 fresh, 1 without CNPJ" || e.br.calls != 1 {
		t.Fatalf("inside the week: %q %v calls %d", note, err, e.br.calls)
	}
	*e.now = e.now.Add(5 * 24 * time.Hour)
	if note, err = e.svc.RefreshStale(ctx, *e.now); err != nil || e.br.calls != 2 {
		t.Fatalf("after a week: %q %v calls %d", note, err, e.br.calls)
	}
	// A failure is reported and retried only after a day.
	e.br.err = errors.New("down")
	*e.now = e.now.Add(8 * 24 * time.Hour)
	if _, err = e.svc.RefreshStale(ctx, *e.now); err == nil {
		t.Fatal("expected failure")
	}
	calls := e.br.calls
	*e.now = e.now.Add(2 * time.Hour)
	if _, err = e.svc.RefreshStale(ctx, *e.now); err != nil || e.br.calls != calls {
		t.Errorf("retried too early: %v calls %d vs %d", err, e.br.calls, calls)
	}
}

func TestShortPriceHistoryShowsCurrentOnly(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	e.price(t, a.ID, "2025-12-30", "cotahist", "15") // only one historical year-end
	r, err := e.svc.Refresh(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	pe := r.Ranges.PE
	if pe == nil || pe.Current == nil || pe.Min != nil || pe.Max != nil || len(pe.Points) != 1 {
		t.Errorf("expected current only: %+v", pe)
	}
}

type fakeCAPM struct{ rate string }

func (f fakeCAPM) ForAsset(context.Context, db.Asset) (capm.CostOfEquity, bool, error) {
	if f.rate == "" {
		return capm.CostOfEquity{}, false, nil
	}
	return capm.CostOfEquity{Rate: money.New(dec(f.rate)), RiskFreeDate: "2026-11-17"}, true, nil
}

func TestReverseDCFDiscountRateSource(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	a := e.asset(t, "PETR4", "B3", "stock", petrobrasCNPJ)
	e.price(t, a.ID, "2026-11-17", "cotahist", "20")
	if _, err := e.svc.Refresh(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	rd := func(p Params) (*ReverseDCF, FundamentalsPanel) {
		r, err := e.svc.Get(ctx, a.ID, p)
		if err != nil {
			t.Fatal(err)
		}
		return r.Helpers.ReverseDCF, r
	}
	// No CAPM source: the fixed market default.
	if r, p := rd(Params{}); r.DiscountSource != "default" || !r.DiscountRate.Equal(dec("0.14")) || p.CAPM != nil {
		t.Fatalf("default: %+v capm=%v", r, p.CAPM)
	}
	// CAPM not available (no beta or rf): still the default.
	e.svc.UseCAPM(fakeCAPM{})
	if r, p := rd(Params{}); r.DiscountSource != "default" || p.CAPM != nil {
		t.Fatalf("unavailable: %+v", r)
	}
	e.svc.UseCAPM(fakeCAPM{rate: "0.0770"})
	r, p := rd(Params{})
	if r.DiscountSource != "capm" || !r.DiscountRate.Equal(dec("0.077")) || p.CAPM == nil || p.CAPM.RiskFreeDate != "2026-11-17" {
		t.Fatalf("capm: %+v capm=%v", r, p.CAPM)
	}
	// An explicit rate in the request wins.
	c := dec("0.12")
	if r, _ := rd(Params{DiscountRate: &c}); r.DiscountSource != "custom" || !r.DiscountRate.Equal(c) {
		t.Fatalf("custom: %+v", r)
	}
}
