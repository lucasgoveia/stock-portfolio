package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
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
	// 2 B3 assets x (brapi + cotahist) = 4 prices; 2 fx; 1 cdi; 1 tesouro date; 2 indices.
	if first != [5]int{4, 2, 1, 1, 2} {
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
	if err != nil || last.Status != RunOK || len(last.Steps) != 11 || last.FinishedAt == "" {
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
