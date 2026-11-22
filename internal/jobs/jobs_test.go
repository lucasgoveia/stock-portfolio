package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

func m(s string) money.Decimal { return money.New(decimal.RequireFromString(s)) }

func date(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC) }

type fakeFX struct{}

func (fakeFX) USDBRL(context.Context, time.Time, time.Time) ([]marketdata.FXRate, error) {
	return []marketdata.FXRate{{Date: date(2026, 11, 17), Sell: m("5.31")}, {Date: date(2026, 11, 16), Sell: m("5.30")}}, nil
}

type fakeCDI struct{}

func (fakeCDI) CDI(context.Context, time.Time, time.Time) ([]marketdata.RatePoint, error) {
	return []marketdata.RatePoint{{Date: date(2026, 11, 17), Value: m("0.055131")}}, nil
}

type fakeTesouro struct{}

func (fakeTesouro) Prices(context.Context, marketdata.TesouroFilter) (marketdata.TesouroResult, error) {
	base := date(2026, 11, 17)
	return marketdata.TesouroResult{Reference: base, Rows: []marketdata.TesouroRow{
		{Title: "Tesouro Selic", BaseDate: base, Maturity: date(2031, 3, 1), SellPU: m("17900.10")},
		{Title: "Tesouro Selic", BaseDate: base, Maturity: date(2029, 3, 1), SellPU: m("17950.55")},
		{Title: "Tesouro Selic", BaseDate: base, Maturity: date(2026, 3, 1), SellPU: m("1")}, // matured
	}}, nil
}

type fakeQuotes struct{ fail map[string]bool }

func (f *fakeQuotes) LatestQuote(_ context.Context, t string) (marketdata.Quote, error) {
	if f.fail[t] {
		return marketdata.Quote{}, errors.New("boom")
	}
	return marketdata.Quote{Ticker: t, Price: m("40.10"), Date: date(2026, 11, 18)}, nil
}

type fakeCotahist struct {
	published map[string]bool // yyyy-mm-dd of published daily files
	years     []int
}

func (f *fakeCotahist) Scan(_ context.Context, kind marketdata.CotahistFileKind, t time.Time, flt marketdata.CotahistFilter, fn func(marketdata.CotahistRecord) error) error {
	if kind == marketdata.CotahistYearly {
		f.years = append(f.years, t.Year())
		for _, tk := range flt.Tickers {
			for _, d := range []time.Time{date(t.Year(), 1, 5), date(t.Year(), 6, 5)} {
				if err := fn(marketdata.CotahistRecord{Date: d, Ticker: tk, Market: "010", Close: m("10")}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if !f.published[t.Format(dateLayout)] {
		return &marketdata.HTTPError{Source: "cotahist", Status: http.StatusNotFound}
	}
	for _, tk := range flt.Tickers {
		if err := fn(marketdata.CotahistRecord{Date: t, Ticker: tk, Market: "010", Close: m("39.98")}); err != nil {
			return err
		}
	}
	return nil
}

type fakeIndex struct{}

func (fakeIndex) Closes(context.Context, time.Time) ([]marketdata.IndexPoint, error) {
	return []marketdata.IndexPoint{{Date: date(2026, 11, 17), Close: m("150000")}}, nil
}

type env struct {
	conn   *sql.DB
	alerts *alerts.Service
	deps   Deps
	quotes *fakeQuotes
	cot    *fakeCotahist
	runner *Runner
	evals  *int
}

// now is Wednesday 2026-11-18 19:00 BRT.
var now = time.Date(2026, 11, 18, 22, 0, 0, 0, time.UTC)

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	clock := func() time.Time { return now }
	as := assets.NewService(conn)
	for _, in := range []assets.CreateInput{
		{Ticker: "WEGE3", Market: "B3", Kind: "stock"},
		{Ticker: "ITUB4", Market: "B3", Kind: "stock"},
		{Ticker: "MSFT", Market: "US", Kind: "stock"},
	} {
		if _, err := as.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	al := alerts.New(conn, clock)
	evals := 0
	e := &env{conn: conn, alerts: al, quotes: &fakeQuotes{fail: map[string]bool{}}, evals: &evals,
		cot: &fakeCotahist{published: map[string]bool{"2026-11-17": true}}}
	e.deps = Deps{
		Conn: conn, Assets: as, Rates: cash.NewRateStore(conn), FX: fakeFX{}, CDI: fakeCDI{}, Tesouro: fakeTesouro{},
		Quotes: e.quotes, Cotahist: e.cot, IBOV: fakeIndex{}, SP500: fakeIndex{}, Now: clock,
		Evaluators: []Evaluator{{Name: "count", Run: func(context.Context) error { evals++; return nil }}},
	}
	e.runner = NewRunner(conn, al, DailySteps(e.deps), clock)
	return e
}

func (e *env) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := e.conn.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func stepStatus(r RunResult) map[string]string {
	out := map[string]string{}
	for _, s := range r.Steps {
		out[s.Name] = s.Status
	}
	return out
}

func TestDailyRunPersistsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	res, err := e.runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"fx": "ok", "cdi": "ok", "tesouro_selic": "ok", "b3_quotes": "ok", "cotahist": "ok",
		"us_prices": "skipped", "indices": "ok", "beta": "skipped", "evaluate": "ok"}
	got := stepStatus(res)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("step %s = %s, want %s (%+v)", k, got[k], v, res.Steps)
		}
	}
	if res.Status != RunOK || res.RunDate != "2026-11-18" {
		t.Fatalf("run = %+v", res)
	}
	counts := func() [5]int {
		return [5]int{
			e.count(t, `SELECT COUNT(*) FROM asset_prices`),
			e.count(t, `SELECT COUNT(*) FROM fx_rates`),
			e.count(t, `SELECT COUNT(*) FROM cdi_rates`),
			e.count(t, `SELECT COUNT(*) FROM tesouro_selic_prices`),
			e.count(t, `SELECT COUNT(*) FROM index_points`),
		}
	}
	first := counts()
	// 2 B3 assets x (brapi + cotahist) = 4 prices, plus the 2-year backfill:
	// 2 assets x (2025-01-05, 2025-06-05, 2026-01-05, 2026-06-05); 2 fx; 1 cdi;
	// 1 tesouro date; 2 indices.
	if first != [5]int{12, 2, 1, 1, 2} {
		t.Fatalf("counts = %v", first)
	}
	var pu string
	if err := e.conn.QueryRow(`SELECT price FROM tesouro_selic_prices`).Scan(&pu); err != nil || pu != "17950.55" {
		t.Fatalf("tesouro PU = %s, %v (want shortest open maturity)", pu, err)
	}
	if _, err := e.runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if counts() != first {
		t.Fatalf("second run changed counts: %v vs %v", counts(), first)
	}
	if *e.evals != 2 {
		t.Fatalf("evaluators ran %d times", *e.evals)
	}
	last, err := e.runner.Last(ctx)
	if err != nil || last.Status != RunOK || len(last.Steps) != 13 || last.FinishedAt == "" {
		t.Fatalf("last = %+v, %v", last, err)
	}
}

func TestFailingStepIsIsolatedAndAlerted(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.quotes.fail["ITUB4"] = true
	res, err := e.runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != RunPartial || stepStatus(res)["b3_quotes"] != StepFailed || stepStatus(res)["evaluate"] != StepOK {
		t.Fatalf("run = %+v", res)
	}
	open, err := e.alerts.List(ctx, alerts.Filter{Type: AlertFetchFailed})
	if err != nil || len(open) != 1 {
		t.Fatalf("alerts = %+v, %v", open, err)
	}
	// WEGE3 still got its quote.
	if n := e.count(t, `SELECT COUNT(*) FROM asset_prices WHERE source = 'brapi'`); n != 1 {
		t.Fatalf("brapi prices = %d", n)
	}
	delete(e.quotes.fail, "ITUB4")
	if res, _ := e.runner.Run(ctx); res.Status != RunOK {
		t.Fatalf("second run = %+v", res)
	}
	if open, _ := e.alerts.List(ctx, alerts.Filter{Type: AlertFetchFailed}); len(open) != 0 {
		t.Fatalf("alert should resolve: %+v", open)
	}
}

func TestCotahistWalksBackToLatestFile(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	// Nothing for the 18th or 17th; the 16th (Monday) is the latest file.
	e.cot.published = map[string]bool{"2026-11-16": true}
	res, err := e.runner.Run(ctx)
	if err != nil || stepStatus(res)["cotahist"] != StepOK {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM asset_prices WHERE source = 'cotahist' AND ref_date = '2026-11-16'`); n != 2 {
		t.Fatalf("cotahist rows = %d", n)
	}
	e.cot.published = map[string]bool{}
	res, _ = e.runner.Run(ctx)
	if stepStatus(res)["cotahist"] != StepFailed {
		t.Fatalf("no file in lookback must fail: %+v", res.Steps)
	}
}

func TestLatestPricePrefersRawSource(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.cot.published = map[string]bool{"2026-11-18": true}
	if _, err := e.runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var src string
	if err := e.conn.QueryRow(`SELECT source FROM asset_prices p WHERE p.rowid = (
		SELECT q.rowid FROM asset_prices q WHERE q.asset_id = 1 ORDER BY q.ref_date DESC,
		CASE q.source WHEN 'manual' THEN 0 WHEN 'cotahist' THEN 1 WHEN 'tiingo' THEN 2 ELSE 3 END LIMIT 1)`).Scan(&src); err != nil {
		t.Fatal(err)
	}
	if src != "cotahist" {
		t.Fatalf("latest source = %s", src)
	}
}

func TestBackfill(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	res, err := Backfill(ctx, e.deps, date(2025, 3, 1))
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, r := range res {
		st[r.Name] = r.Status
	}
	if st["cotahist"] != StepOK || st["us_prices"] != StepSkipped {
		t.Fatalf("backfill = %+v", res)
	}
	if len(e.cot.years) != 2 || e.cot.years[0] != 2025 || e.cot.years[1] != 2026 {
		t.Fatalf("years = %v", e.cot.years)
	}
	// 2025-01-05 is before `from` and dropped: 2 tickers x (2025-06-05, 2026-01-05, 2026-06-05).
	if n := e.count(t, `SELECT COUNT(*) FROM asset_prices WHERE source = 'cotahist'`); n != 6 {
		t.Fatalf("cotahist rows = %d", n)
	}
}

func TestNextRun(t *testing.T) {
	loc := SaoPaulo()
	tests := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2026, 11, 18, 18, 0, 0, 0, loc), time.Date(2026, 11, 18, 19, 0, 0, 0, loc)},
		{time.Date(2026, 11, 18, 19, 0, 0, 0, loc), time.Date(2026, 11, 19, 19, 0, 0, 0, loc)},
		{time.Date(2026, 11, 18, 23, 0, 0, 0, time.UTC), time.Date(2026, 11, 19, 19, 0, 0, 0, loc)},
	}
	for _, tc := range tests {
		if got := NextRun(tc.now, 19, 0); !got.Equal(tc.want) {
			t.Errorf("NextRun(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
	if _, _, err := parseClock("7pm"); err == nil {
		t.Error("bad clock must fail")
	}
}

type fakeBetas struct{ got map[int64]string }

func (f *fakeBetas) ApplyBeta(_ context.Context, id int64, ix string, b decimal.Decimal, weeks int) (bool, error) {
	f.got[id] = ix + " " + b.StringFixed(2)
	return true, nil
}

func TestBetaStep(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	fb := &fakeBetas{got: map[int64]string{}}
	e.deps.Betas = fb
	// 70 weekly Fridays of ITUB4 (asset 2) moving exactly 2x IBOV.
	start := date(2025, 7, 18)
	a, ix := 30.0, 100000.0
	for i := 0; i < 70; i++ {
		day := start.AddDate(0, 0, 7*i).Format(dateLayout)
		r := 0.01
		if i%2 == 0 {
			r = -0.012
		}
		ix *= 1 + r
		a *= 1 + 2*r
		if _, err := e.conn.Exec(`INSERT INTO asset_prices (asset_id, ref_date, source, close, fetched_at) VALUES (2, ?, 'cotahist', ?, 'x')`, day, decimal.NewFromFloat(a).String()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.conn.Exec(`INSERT INTO index_points (index_code, ref_date, close) VALUES ('IBOV', ?, ?)`, day, decimal.NewFromFloat(ix).String()); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	res, err := r.Run(ctx)
	if err != nil || stepStatus(res)["beta"] != StepOK {
		t.Fatalf("run = %+v, %v", res.Steps, err)
	}
	// ITUB4 (asset 2) has history; WEGE3 and MSFT do not.
	if len(fb.got) != 1 || fb.got[2][:5] != "IBOV " {
		t.Fatalf("betas = %v", fb.got)
	}
}

type fakeFeed struct{}

func (fakeFeed) CorporateActions(_ context.Context, ticker string, _, _ time.Time) (marketdata.CorporateActions, error) {
	pay := date(2026, 12, 5)
	return marketdata.CorporateActions{Ticker: ticker, StockDividends: []marketdata.StockDividend{
		{Label: "DESDOBRAMENTO", Factor: m("2"), CompleteFactor: "2 para 1", LastDatePrior: date(2026, 10, 23)},
		{Label: "SUBSCRICAO", Factor: m("1")},
	}, CashDividends: []marketdata.CashDividend{
		{Label: "JCP", Rate: m("0.5"), LastDatePrior: date(2026, 11, 10), PaymentDate: &pay},
		{Label: "DIVIDENDO", Rate: m("0.2"), LastDatePrior: date(2026, 11, 10)},
	}}, nil
}

type fakeSink struct{ got []string }

func (f *fakeSink) Detect(_ context.Context, id int64, kind, rec, pay string, ps decimal.Decimal, src string) (bool, error) {
	f.got = append(f.got, fmt.Sprintf("%d %s %s %s %s %s", id, kind, rec, pay, ps, src))
	return true, nil
}

func TestDividendStep(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sink := &fakeSink{}
	d := e.deps
	d.DividendFeed, d.Dividends = fakeFeed{}, sink
	p := &pipeline{d: d, q: db.New(d.Conn)}
	// No holdings yet: skipped.
	if _, err := p.dividends(ctx, date(2026, 11, 18)); !errors.Is(err, ErrSkipped) {
		t.Fatalf("no holdings: %v", err)
	}
	if _, err := e.conn.Exec(`UPDATE assets SET status = 'holding' WHERE market = 'B3'`); err != nil {
		t.Fatal(err)
	}
	note, err := p.dividends(ctx, date(2026, 11, 18))
	if err != nil || len(sink.got) == 0 || !strings.Contains(sink.got[0], "jcp 2026-11-10 2026-12-05 0.5 brapi") ||
		!strings.Contains(sink.got[1], "dividend 2026-11-10  0.2 brapi") {
		t.Fatalf("detect: %q %v %v", note, sink.got, err)
	}
}

type fakeActions struct{ got []string }

func (f *fakeActions) Detect(_ context.Context, id int64, kind, ex string, factor decimal.Decimal, src, note string) (bool, error) {
	f.got = append(f.got, fmt.Sprintf("%d %s %s %s %s %s", id, kind, ex, factor, src, note))
	return true, nil
}

func TestCorporateActionStep(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sink := &fakeActions{}
	d := e.deps
	d.DividendFeed, d.Actions = fakeFeed{}, sink
	p := &pipeline{d: d, q: db.New(d.Conn)}
	if _, err := e.conn.Exec(`UPDATE assets SET status = 'holding' WHERE ticker = 'WEGE3'`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.corporateActions(ctx, date(2026, 11, 18)); err != nil {
		t.Fatal(err)
	}
	// Friday 2026-10-23 is the last day with the right: ex-date Monday 10-26.
	if len(sink.got) != 1 || !strings.Contains(sink.got[0], "split 2026-10-26 2 brapi DESDOBRAMENTO 2 para 1") {
		t.Fatalf("detected = %v", sink.got)
	}
}

type fakeFundamentals struct {
	calls int
	err   error
}

func (f *fakeFundamentals) RefreshStale(context.Context, time.Time) (string, error) {
	f.calls++
	return "1 refreshed, 0 fresh, 0 without CNPJ", f.err
}

func TestFundamentalsStep(t *testing.T) {
	e := setup(t)
	if st := stepStatus(runWith(t, e, nil))["fundamentals"]; st != StepSkipped {
		t.Fatalf("without a refresher the step is %q", st)
	}
	f := &fakeFundamentals{}
	if st := stepStatus(runWith(t, e, f))["fundamentals"]; st != StepOK || f.calls != 1 {
		t.Fatalf("step %q after %d calls", st, f.calls)
	}
	f.err = errors.New("PETR4: cvm down")
	res := runWith(t, e, f)
	if st := stepStatus(res)["fundamentals"]; st != StepFailed || res.Status != RunPartial {
		t.Fatalf("failed refresh: step %q run %q", st, res.Status)
	}
}

func runWith(t *testing.T, e *env, f FundamentalsRefresher) RunResult {
	t.Helper()
	e.deps.Fundamentals = f
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// brapiGated serves the free-plan 403 for WEGE3 (and every ticker when
// allGated) and an empty dividends list for the rest.
func brapiGated(t *testing.T, allGated bool) *marketdata.Brapi {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "marketdata", "testdata", "brapi", "dividends_free_plan_403.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sym := r.URL.Query().Get("symbols")
		if allGated || sym == "WEGE3" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write(body)
			return
		}
		_, _ = fmt.Fprintf(w, `{"results":[{"requestedSymbol":%q,"symbol":%q,"data":{"cashDividends":[],"stockDividends":[],"subscriptions":[]}}]}`, sym, sym)
	}))
	t.Cleanup(srv.Close)
	return marketdata.NewBrapi(srv.Client(), srv.URL, "t")
}

func TestPlanGatedDividendsAreSkippedNotFailed(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if _, err := e.conn.Exec(`UPDATE assets SET status = 'holding' WHERE market = 'B3'`); err != nil {
		t.Fatal(err)
	}
	feed := brapiGated(t, true)
	e.deps.DividendFeed, e.deps.Dividends, e.deps.Actions = feed, &fakeSink{}, &fakeActions{}
	runner := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	res, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st := stepStatus(res)
	if res.Status != RunOK || st["dividends"] != StepSkipped || st["corporate_actions"] != StepSkipped {
		t.Fatalf("run = %+v", res)
	}
	for _, s := range res.Steps {
		if s.Name == "dividends" && (s.Error != "" || !strings.Contains(s.Note, "brapi plan lacks dividends for 2 tickers; enter dividends and corporate actions manually")) {
			t.Fatalf("dividends step = %+v", s)
		}
	}
	if open, _ := e.alerts.List(ctx, alerts.Filter{Type: AlertFetchFailed}); len(open) != 0 {
		t.Fatalf("no data_fetch_failed expected: %+v", open)
	}
}

func TestPartiallyGatedStepStillProcessesOtherTickers(t *testing.T) {
	e := setup(t)
	if _, err := e.conn.Exec(`UPDATE assets SET status = 'holding' WHERE market = 'B3'`); err != nil {
		t.Fatal(err)
	}
	d := e.deps
	d.DividendFeed, d.Dividends = brapiGated(t, false), &fakeSink{}
	p := &pipeline{d: d, q: db.New(d.Conn)}
	note, err := p.dividends(context.Background(), date(2026, 11, 18))
	if err != nil || !strings.Contains(note, "0 new payments to confirm; brapi plan lacks dividends for 1 tickers") {
		t.Fatalf("note %q err %v", note, err)
	}
}

func TestStepErrorsAreSummarized(t *testing.T) {
	long := errors.New(strings.Repeat("x", 5000))
	if got := summarize(long); len([]rune(got)) != maxStepError {
		t.Fatalf("len = %d", len([]rune(got)))
	}
	if got := summarize(errors.New("a\n  b")); got != "a b" {
		t.Fatalf("got %q", got)
	}
}

type fakeUS struct{ froms map[string][]time.Time }

func (f *fakeUS) DailyPrices(_ context.Context, tk string, from, _ time.Time) ([]marketdata.DailyBar, error) {
	f.froms[tk] = append(f.froms[tk], from)
	return []marketdata.DailyBar{{Date: date(2026, 11, 17), Close: m("400"), AdjClose: m("399")}}, nil
}

type recIndex struct{ froms []time.Time }

func (r *recIndex) Closes(_ context.Context, from time.Time) ([]marketdata.IndexPoint, error) {
	r.froms = append(r.froms, from)
	return []marketdata.IndexPoint{{Date: date(2026, 11, 17), Close: m("150000")}}, nil
}

func TestUSPricesBackfillOnceThenShortWindow(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	us := &fakeUS{froms: map[string][]time.Time{}}
	e.deps.US = us
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	day := date(2026, 11, 18)
	res, err := r.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Steps[5]; got.Name != "us_prices" || got.Note != "1/1 tickers (1 backfilled)" {
		t.Fatalf("step = %+v", got)
	}
	if got := us.froms["MSFT"]; len(got) != 1 || !got[0].Equal(day.AddDate(-2, 0, 0)) {
		t.Fatalf("first run from = %v, want 2y window in one request", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM history_backfills WHERE subject = 'asset:3' AND source = 'tiingo'`); n != 1 {
		t.Fatalf("marker rows = %d", n)
	}
	res, err = r.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Steps[5].Note; got != "1/1 tickers" {
		t.Fatalf("second note = %q", got)
	}
	if got := us.froms["MSFT"]; len(got) != 2 || !got[1].Equal(day.AddDate(0, 0, -7)) {
		t.Fatalf("second run from = %v, want short window", got)
	}
}

func TestIndexBackfillOnce(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	ibov, sp := &recIndex{}, &recIndex{}
	e.deps.IBOV, e.deps.SP500 = ibov, sp
	// SP500 already holds 2 years of history; IBOV does not.
	if _, err := e.conn.Exec(`INSERT INTO index_points (index_code, ref_date, close) VALUES ('SP500', '2024-11-20', '4500')`); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	day := date(2026, 11, 18)
	for i := 0; i < 2; i++ {
		if _, err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(ibov.froms) != 2 || !ibov.froms[0].Equal(day.AddDate(-2, 0, 0)) || !ibov.froms[1].Equal(day.AddDate(0, 0, -14)) {
		t.Fatalf("IBOV froms = %v", ibov.froms)
	}
	for _, f := range sp.froms {
		if !f.Equal(day.AddDate(0, 0, -14)) {
			t.Fatalf("SP500 with full history must use the short window: %v", sp.froms)
		}
	}
	if n := e.count(t, `SELECT COUNT(*) FROM history_backfills WHERE source = 'index'`); n != 1 {
		t.Fatalf("index markers = %d", n)
	}
}

type histCotahist struct {
	yearly    map[int][]string
	monthly   []time.Time
	noYearly  map[int]bool
	published map[string]bool
}

func (f *histCotahist) Scan(_ context.Context, kind marketdata.CotahistFileKind, t time.Time, flt marketdata.CotahistFilter, fn func(marketdata.CotahistRecord) error) error {
	notFound := &marketdata.HTTPError{Source: "cotahist", Status: http.StatusNotFound}
	switch kind {
	case marketdata.CotahistYearly:
		if f.noYearly[t.Year()] {
			return notFound
		}
		f.yearly[t.Year()] = flt.Tickers
	case marketdata.CotahistMonthly:
		f.monthly = append(f.monthly, t)
		if t.Month() != time.March {
			return notFound
		}
	default:
		if !f.published[t.Format(dateLayout)] {
			return notFound
		}
	}
	for _, tk := range flt.Tickers {
		if err := fn(marketdata.CotahistRecord{Date: date(t.Year(), t.Month(), 10), Ticker: tk, Market: "010", Close: m("12")}); err != nil {
			return err
		}
	}
	return nil
}

func TestCotahistBackfillScansShortHistoryTickersOnly(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	// WEGE3 (asset 1) was backfilled before.
	if _, err := e.conn.Exec(`INSERT INTO history_backfills (subject, source, from_date, done_at) VALUES ('asset:1', 'cotahist', '2024-11-18', 'x')`); err != nil {
		t.Fatal(err)
	}
	hc := &histCotahist{yearly: map[int][]string{}, noYearly: map[int]bool{2026: true}, published: map[string]bool{"2026-11-17": true}}
	e.deps.Cotahist = hc
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	res, err := r.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Steps[4]; got.Name != "cotahist" || got.Status != StepOK || !strings.HasSuffix(got.Note, "(1 backfilled)") {
		t.Fatalf("step = %+v", got)
	}
	for _, y := range []int{2024, 2025} {
		if tk := hc.yearly[y]; len(tk) != 1 || tk[0] != "ITUB4" {
			t.Fatalf("year %d tickers = %v", y, tk)
		}
	}
	// 2026 yearly is missing: monthly files Jan..Nov are tried, missing ones skipped.
	if len(hc.monthly) != 11 {
		t.Fatalf("monthly scans = %d", len(hc.monthly))
	}
	var n int
	if err := e.conn.QueryRow(`SELECT COUNT(*) FROM asset_prices WHERE asset_id = 2 AND source = 'cotahist' AND ref_date = '2026-03-10'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("march close = %d, %v", n, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM asset_prices WHERE asset_id = 1 AND source = 'cotahist' AND ref_date < '2026-11-01'`); n != 0 {
		t.Fatalf("backfilled asset must not be rescanned: %d rows", n)
	}
	// Second run: everything is marked, no yearly scan happens.
	hc.yearly = map[int][]string{}
	if _, err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(hc.yearly) != 0 {
		t.Fatalf("second run rescanned years: %v", hc.yearly)
	}
}

func TestCotahistBackfillFailureKeepsDailyClose(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	e.deps.Cotahist = failYearly{e.cot}
	r := NewRunner(e.conn, e.alerts, DailySteps(e.deps), func() time.Time { return now })
	res, _ := r.Run(ctx)
	if stepStatus(res)["cotahist"] != StepFailed {
		t.Fatalf("steps = %+v", res.Steps)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM asset_prices WHERE source = 'cotahist' AND ref_date = '2026-11-17'`); n != 2 {
		t.Fatalf("daily closes = %d", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM history_backfills WHERE source = 'cotahist'`); n != 0 {
		t.Fatalf("failed backfill must not be marked: %d", n)
	}
}

type failYearly struct{ *fakeCotahist }

func (f failYearly) Scan(ctx context.Context, kind marketdata.CotahistFileKind, t time.Time, flt marketdata.CotahistFilter, fn func(marketdata.CotahistRecord) error) error {
	if kind == marketdata.CotahistYearly {
		return errors.New("boom")
	}
	return f.fakeCotahist.Scan(ctx, kind, t, flt, fn)
}
