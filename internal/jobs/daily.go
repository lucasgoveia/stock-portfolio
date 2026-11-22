package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // America/Sao_Paulo without system tzdata

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/corpactions"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

const dateLayout = "2006-01-02"

// SaoPaulo is the market timezone.
func SaoPaulo() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}

func civilDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// CotahistSource scans one COTAHIST file.
type CotahistSource interface {
	Scan(ctx context.Context, kind marketdata.CotahistFileKind, t time.Time, f marketdata.CotahistFilter, fn func(marketdata.CotahistRecord) error) error
}

// RateWriter persists valuation inputs (cash.RateStore).
type RateWriter interface {
	UpsertCDI(ctx context.Context, refDate string, ratePct decimal.Decimal) error
	UpsertTesouroSelicPrice(ctx context.Context, refDate string, price decimal.Decimal) error
	UpsertUSDBRL(ctx context.Context, refDate string, rate decimal.Decimal) error
}

// Deps are the daily pipeline's collaborators. A nil source skips its step.
type Deps struct {
	Conn     db.DBTX
	Assets   *assets.Service
	Rates    RateWriter
	FX       marketdata.FXSource
	CDI      marketdata.CDISource
	Tesouro  marketdata.TesouroSource
	Quotes   marketdata.QuoteSource
	Cotahist CotahistSource
	US       marketdata.USPriceSource
	IBOV     marketdata.IndexSource
	SP500    marketdata.IndexSource
	Betas    BetaWriter
	// DividendFeed announces B3 dividends and JCP; Dividends queues them for
	// confirmation (spec §8.5). Either nil skips the step.
	DividendFeed marketdata.DividendSource
	Dividends    DividendSink
	// Actions queues splits, reverse splits and bonificações from the same feed.
	Actions ActionSink
	// Fundamentals refreshes CVM/SEC statements that are over a week old.
	Fundamentals FundamentalsRefresher
	// Snapshots writes the daily performance snapshot (last step, spec §16.1).
	Snapshots  SnapshotWriter
	Evaluators []Evaluator
	Now        func() time.Time
}

// DividendSink queues detected payments (dividends.Service).
type DividendSink interface {
	Detect(ctx context.Context, assetID int64, kind, recordDate, payDate string, perShare decimal.Decimal, source string) (bool, error)
}

// ActionSink queues detected corporate actions (corpactions.Service).
type ActionSink interface {
	Detect(ctx context.Context, assetID int64, kind, exDate string, factor decimal.Decimal, source, note string) (bool, error)
}

// FundamentalsRefresher refreshes stale fundamentals (fundamentals.Service).
type FundamentalsRefresher interface {
	RefreshStale(ctx context.Context, now time.Time) (string, error)
}

// SnapshotWriter stores daily values for performance (performance.Service).
type SnapshotWriter interface {
	SnapshotToday(ctx context.Context, day time.Time) (string, error)
	Backfill(ctx context.Context, from, to time.Time) (int, error)
}

// BetaWriter stores computed betas (ratings.Service).
type BetaWriter interface {
	ApplyBeta(ctx context.Context, assetID int64, indexCode string, beta decimal.Decimal, weeks int) (bool, error)
}

// Evaluator is a named alert evaluation run at the end of the pipeline.
type Evaluator struct {
	Name string
	Run  func(ctx context.Context) error
}

// CotahistLookback is how many days the COTAHIST step walks back to find the
// latest published daily file (published around 21:00 BRT).
const CotahistLookback = 7

// DailySteps builds the ordered pipeline.
func DailySteps(d Deps) []Step {
	if d.Now == nil {
		d.Now = time.Now
	}
	p := &pipeline{d: d, q: db.New(d.Conn)}
	return []Step{
		{"fx", p.fx},
		{"cdi", p.cdi},
		{"tesouro_selic", p.tesouro},
		{"b3_quotes", p.b3Quotes},
		{"cotahist", p.cotahist},
		{"us_prices", p.usPrices},
		{"indices", p.indices},
		{"dividends", p.dividends},
		{"corporate_actions", p.corporateActions},
		{"fundamentals", p.fundamentals},
		{"beta", p.beta},
		{"evaluate", p.evaluate},
		{"snapshot", p.snapshot},
	}
}

type pipeline struct {
	d Deps
	q *db.Queries
}

func (p *pipeline) stamp() string { return p.d.Now().UTC().Format(tsLayout) }

// tracked returns holding and watchlist assets of a market, sorted by ticker.
func (p *pipeline) tracked(ctx context.Context, market string) ([]db.Asset, error) {
	var out []db.Asset
	for _, st := range []string{assets.StatusHolding, assets.StatusWatchlist} {
		l, err := p.d.Assets.List(ctx, assets.Filter{Status: st, Market: market})
		if err != nil {
			return nil, err
		}
		out = append(out, l...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ticker < out[j].Ticker })
	return out, nil
}

func (p *pipeline) fx(ctx context.Context, day time.Time) (string, error) {
	if p.d.FX == nil {
		return "", ErrSkipped
	}
	rates, err := p.d.FX.USDBRL(ctx, day.AddDate(0, 0, -7), day)
	if err != nil {
		return "", err
	}
	for _, r := range rates {
		if err := p.d.Rates.UpsertUSDBRL(ctx, r.Date.Format(dateLayout), r.Sell.Decimal); err != nil {
			return "", err
		}
	}
	return latestNote(len(rates), func(i int) time.Time { return rates[i].Date }), nil
}

func (p *pipeline) cdi(ctx context.Context, day time.Time) (string, error) {
	if p.d.CDI == nil {
		return "", ErrSkipped
	}
	pts, err := p.d.CDI.CDI(ctx, day.AddDate(0, 0, -10), day)
	if err != nil {
		return "", err
	}
	for _, r := range pts {
		if err := p.d.Rates.UpsertCDI(ctx, r.Date.Format(dateLayout), r.Value.Decimal); err != nil {
			return "", err
		}
	}
	return latestNote(len(pts), func(i int) time.Time { return pts[i].Date }), nil
}

// tesouro stores, per base date, the redemption PU of the shortest Tesouro
// Selic maturity still open (all Selic titles track the same rate closely).
func (p *pipeline) tesouro(ctx context.Context, day time.Time) (string, error) {
	if p.d.Tesouro == nil {
		return "", ErrSkipped
	}
	res, err := p.d.Tesouro.Prices(ctx, marketdata.TesouroFilter{Title: marketdata.TesouroTitleSelic, OnOrAfter: day.AddDate(0, 0, -10)})
	if err != nil {
		return "", err
	}
	best := map[string]marketdata.TesouroRow{}
	for _, r := range res.Rows {
		if !r.Maturity.After(r.BaseDate) {
			continue
		}
		k := r.BaseDate.Format(dateLayout)
		if cur, ok := best[k]; !ok || r.Maturity.Before(cur.Maturity) {
			best[k] = r
		}
	}
	for k, r := range best {
		if err := p.d.Rates.UpsertTesouroSelicPrice(ctx, k, r.SellPU.Decimal); err != nil {
			return "", err
		}
	}
	if len(best) == 0 {
		return "", errors.New("no Tesouro Selic rows in the last 10 days")
	}
	return fmt.Sprintf("%d dates, reference %s", len(best), res.Reference.Format(dateLayout)), nil
}

func (p *pipeline) upsertPrice(ctx context.Context, assetID int64, date time.Time, source string, close, adj string) error {
	return p.q.UpsertAssetPrice(ctx, db.UpsertAssetPriceParams{
		AssetID: assetID, RefDate: date.Format(dateLayout), Source: source, Close: close,
		AdjClose: sql.NullString{String: adj, Valid: adj != ""}, FetchedAt: p.stamp(),
	})
}

func (p *pipeline) b3Quotes(ctx context.Context, _ time.Time) (string, error) {
	if p.d.Quotes == nil {
		return "", ErrSkipped
	}
	list, err := p.tracked(ctx, "B3")
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no B3 assets", ErrSkipped
	}
	var failed []string
	for _, a := range list {
		q, err := p.d.Quotes.LatestQuote(ctx, a.Ticker)
		if err == nil {
			err = p.upsertPrice(ctx, a.ID, q.Date, "brapi", q.Price.String(), "")
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
		}
	}
	note := fmt.Sprintf("%d/%d quotes", len(list)-len(failed), len(list))
	if len(failed) > 0 {
		return note, errors.New(strings.Join(failed, "; "))
	}
	return note, nil
}

// dividendLookback is how far back announcements are read each day.
const dividendLookback = 120 * 24 * time.Hour

// dividends proposes announced B3 dividends and JCP for held assets. brapi
// rates are gross per share on today's split-adjusted scale.
func (p *pipeline) dividends(ctx context.Context, day time.Time) (string, error) {
	if p.d.DividendFeed == nil || p.d.Dividends == nil {
		return "", ErrSkipped
	}
	held, err := p.d.Assets.List(ctx, assets.Filter{Status: assets.StatusHolding, Market: "B3"})
	if err != nil {
		return "", err
	}
	if len(held) == 0 {
		return "no B3 holdings", ErrSkipped
	}
	found, gated := 0, 0
	var failed []string
	for _, a := range held {
		ca, err := p.d.DividendFeed.CorporateActions(ctx, a.Ticker, day.Add(-dividendLookback), time.Time{})
		if errors.Is(err, marketdata.ErrPlanNotAvailable) {
			gated++
			continue
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
			continue
		}
		for _, cd := range ca.CashDividends {
			kind := "dividend"
			if strings.Contains(strings.ToUpper(cd.Label), "JCP") {
				kind = "jcp"
			}
			pay := ""
			if cd.PaymentDate != nil && !cd.PaymentDateIsDeadline {
				pay = cd.PaymentDate.Format("2006-01-02")
			}
			ok, err := p.d.Dividends.Detect(ctx, a.ID, kind, cd.LastDatePrior.Format("2006-01-02"), pay, cd.Rate.Decimal, "brapi")
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
				continue
			}
			if ok {
				found++
			}
		}
	}
	note := fmt.Sprintf("%d new payments to confirm", found)
	return gatedResult(note, "dividends", gated, len(held), failed)
}

func (p *pipeline) fundamentals(ctx context.Context, _ time.Time) (string, error) {
	if p.d.Fundamentals == nil {
		return "", ErrSkipped
	}
	return p.d.Fundamentals.RefreshStale(ctx, p.d.Now())
}

func (p *pipeline) snapshot(ctx context.Context, day time.Time) (string, error) {
	if p.d.Snapshots == nil {
		return "", ErrSkipped
	}
	return p.d.Snapshots.SnapshotToday(ctx, day)
}

// corporateActions queues share events announced for B3 holdings. brapi
// gives the last day with the right; the ex-date is the next business day.
func (p *pipeline) corporateActions(ctx context.Context, day time.Time) (string, error) {
	if p.d.DividendFeed == nil || p.d.Actions == nil {
		return "", ErrSkipped
	}
	held, err := p.d.Assets.List(ctx, assets.Filter{Status: assets.StatusHolding, Market: "B3"})
	if err != nil {
		return "", err
	}
	if len(held) == 0 {
		return "no B3 holdings", ErrSkipped
	}
	found, gated := 0, 0
	var failed []string
	for _, a := range held {
		ca, err := p.d.DividendFeed.CorporateActions(ctx, a.Ticker, day.Add(-dividendLookback), time.Time{})
		if errors.Is(err, marketdata.ErrPlanNotAvailable) {
			gated++
			continue
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
			continue
		}
		for _, sd := range ca.StockDividends {
			kind, ok := corpactions.KindFromLabel(sd.Label)
			if !ok {
				continue
			}
			ex := corpactions.NextBusinessDay(sd.LastDatePrior)
			if sd.ExDate != nil {
				ex = *sd.ExDate
			}
			note := strings.TrimSpace(sd.Label + " " + sd.CompleteFactor)
			ok, err := p.d.Actions.Detect(ctx, a.ID, kind, ex.Format("2006-01-02"), sd.Factor.Decimal, "brapi", note)
			if err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
				continue
			}
			if ok {
				found++
			}
		}
	}
	note := fmt.Sprintf("%d new events to confirm", found)
	return gatedResult(note, "corporate actions", gated, len(held), failed)
}

// cotahist stores raw closes from the latest published daily file, then
// backfills two years of history for assets that have none yet. A failing
// backfill never undoes the daily update; it is reported after it.
func (p *pipeline) cotahist(ctx context.Context, day time.Time) (string, error) {
	if p.d.Cotahist == nil {
		return "", ErrSkipped
	}
	list, err := p.tracked(ctx, "B3")
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no B3 assets", ErrSkipped
	}
	note, err := p.cotahistDaily(ctx, day, list)
	n, berr := p.cotahistBackfill(ctx, day, list)
	if n > 0 {
		note += fmt.Sprintf(" (%d backfilled)", n)
	}
	if berr != nil {
		err = errors.Join(err, fmt.Errorf("history backfill: %w", berr))
	}
	return note, err
}

func (p *pipeline) cotahistDaily(ctx context.Context, day time.Time, list []db.Asset) (string, error) {
	ids := map[string]int64{}
	tickers := make([]string, 0, len(list))
	for _, a := range list {
		ids[a.Ticker] = a.ID
		tickers = append(tickers, a.Ticker)
	}
	for back := 0; back < CotahistLookback; back++ {
		d := day.AddDate(0, 0, -back)
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		n := 0
		err := p.d.Cotahist.Scan(ctx, marketdata.CotahistDaily, d, marketdata.LotFilter(tickers...), func(r marketdata.CotahistRecord) error {
			n++
			return p.upsertPrice(ctx, ids[r.Ticker], r.Date, "cotahist", r.Close.String(), "")
		})
		if marketdata.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: %d closes", d.Format(dateLayout), n), nil
	}
	return "", fmt.Errorf("no COTAHIST daily file in the last %d days", CotahistLookback)
}

func (p *pipeline) usPrices(ctx context.Context, day time.Time) (string, error) {
	if p.d.US == nil {
		return "no US price source configured (tiingo_token)", ErrSkipped
	}
	list, err := p.tracked(ctx, "US")
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "no US assets", ErrSkipped
	}
	done, err := p.backfilled(ctx, "tiingo")
	if err != nil {
		return "", err
	}
	var failed []string
	backfilled := 0
	for _, a := range list {
		// One request per ticker either way (Tiingo's hourly quota): a
		// ticker not yet backfilled just asks for the longer window.
		from, backfill := day.AddDate(0, 0, -7), !done[assetSubject(a.ID)]
		if backfill {
			from = historyStart(day)
		}
		bars, err := p.d.US.DailyPrices(ctx, a.Ticker, from, day)
		for _, b := range bars {
			if err != nil {
				break
			}
			err = p.upsertPrice(ctx, a.ID, b.Date, "tiingo", b.Close.String(), b.AdjClose.String())
		}
		if err == nil && backfill {
			err = p.markBackfilled(ctx, assetSubject(a.ID), "tiingo", from)
			backfilled++
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
		}
	}
	note := fmt.Sprintf("%d/%d tickers", len(list)-len(failed), len(list))
	if backfilled > 0 {
		note += fmt.Sprintf(" (%d backfilled)", backfilled)
	}
	if len(failed) > 0 {
		return note, errors.New(strings.Join(failed, "; "))
	}
	return note, nil
}

// indices stores recent index closes, loading two years once for an index
// whose history is short.
func (p *pipeline) indices(ctx context.Context, day time.Time) (string, error) {
	return p.syncIndices(ctx, func(code string) (time.Time, bool, error) { return p.indexFrom(ctx, day, code) })
}

// syncIndices fetches each configured index from the date plan returns.
func (p *pipeline) syncIndices(ctx context.Context, plan func(code string) (time.Time, bool, error)) (string, error) {
	var notes, failed []string
	for _, ix := range []struct {
		code string
		src  marketdata.IndexSource
	}{{"IBOV", p.d.IBOV}, {"SP500", p.d.SP500}} {
		if ix.src == nil {
			continue
		}
		from, backfill, err := plan(ix.code)
		var pts []marketdata.IndexPoint
		if err == nil {
			pts, err = ix.src.Closes(ctx, from)
		}
		for _, pt := range pts {
			if err != nil {
				break
			}
			err = p.q.UpsertIndexPoint(ctx, db.UpsertIndexPointParams{IndexCode: ix.code, RefDate: pt.Date.Format(dateLayout), Close: pt.Close.String()})
		}
		if err == nil && backfill {
			err = p.markBackfilled(ctx, "index:"+ix.code, "index", from)
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", ix.code, err))
			continue
		}
		note := fmt.Sprintf("%s %d", ix.code, len(pts))
		if backfill {
			note += " (backfilled)"
		}
		notes = append(notes, note)
	}
	if len(notes) == 0 && len(failed) == 0 {
		return "", ErrSkipped
	}
	if len(failed) > 0 {
		return strings.Join(notes, ", "), errors.New(strings.Join(failed, "; "))
	}
	return strings.Join(notes, ", "), nil
}

func (p *pipeline) evaluate(ctx context.Context, _ time.Time) (string, error) {
	if len(p.d.Evaluators) == 0 {
		return "", ErrSkipped
	}
	var failed, ok []string
	for _, e := range p.d.Evaluators {
		if err := e.Run(ctx); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", e.Name, err))
			continue
		}
		ok = append(ok, e.Name)
	}
	if len(failed) > 0 {
		return strings.Join(ok, ", "), errors.New(strings.Join(failed, "; "))
	}
	return strings.Join(ok, ", "), nil
}

func latestNote(n int, date func(int) time.Time) string {
	if n == 0 {
		return "no new points"
	}
	latest := date(0)
	for i := 1; i < n; i++ {
		if d := date(i); d.After(latest) {
			latest = d
		}
	}
	return fmt.Sprintf("%d points, latest %s", n, latest.Format(dateLayout))
}

// beta recomputes the 2-year weekly beta of every tracked asset against its
// benchmark (B3: raw COTAHIST closes vs IBOV; US: Tiingo adjusted closes vs
// S&P 500) and lets the ratings service refresh automatic volatility.
func (p *pipeline) beta(ctx context.Context, day time.Time) (string, error) {
	if p.d.Betas == nil {
		return "", ErrSkipped
	}
	from, to := day.AddDate(-2, 0, 0).Format(dateLayout), day.Format(dateLayout)
	var list []db.Asset
	for _, mkt := range []string{"B3", "US"} {
		l, err := p.tracked(ctx, mkt)
		if err != nil {
			return "", err
		}
		list = append(list, l...)
	}
	if len(list) == 0 {
		return "no assets", ErrSkipped
	}
	indexCache := map[string][]engine.PricePoint{}
	computed, short, changed := 0, 0, 0
	for _, a := range list {
		ix, ok := indexCache[a.Benchmark]
		if !ok {
			rows, err := p.q.IndexHistory(ctx, db.IndexHistoryParams{IndexCode: a.Benchmark, FromDate: from, ToDate: to})
			if err != nil {
				return "", err
			}
			for _, r := range rows {
				if pt, ok := point(r.RefDate, r.Close); ok {
					ix = append(ix, pt)
				}
			}
			indexCache[a.Benchmark] = ix
		}
		source := "cotahist"
		if a.Market == "US" {
			source = "tiingo"
		}
		rows, err := p.q.AssetPriceHistory(ctx, db.AssetPriceHistoryParams{AssetID: a.ID, Source: source, FromDate: from, ToDate: to})
		if err != nil {
			return "", err
		}
		var px []engine.PricePoint
		for _, r := range rows {
			v := r.Close
			if r.AdjClose.Valid && r.AdjClose.String != "" {
				v = r.AdjClose.String
			}
			if pt, ok := point(r.RefDate, v); ok {
				px = append(px, pt)
			}
		}
		res := engine.WeeklyBeta(px, ix)
		if !res.OK {
			short++
			continue
		}
		computed++
		ch, err := p.d.Betas.ApplyBeta(ctx, a.ID, a.Benchmark, res.Beta, res.Weeks)
		if err != nil {
			return "", err
		}
		if ch {
			changed++
		}
	}
	return fmt.Sprintf("%d computed, %d short history, %d ratings updated", computed, short, changed), nil
}

func point(date, v string) (engine.PricePoint, bool) {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return engine.PricePoint{}, false
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return engine.PricePoint{}, false
	}
	return engine.PricePoint{Date: t, Close: d}, true
}

// gatedResult turns per-ticker plan gating into a note, not a failure: when
// every ticker was gated the step is skipped; real failures still fail it.
func gatedResult(note, what string, gated, total int, failed []string) (string, error) {
	if gated > 0 {
		gnote := fmt.Sprintf("brapi plan lacks %s for %d tickers; enter dividends and corporate actions manually", what, gated)
		if gated == total {
			note = gnote
		} else {
			note += "; " + gnote
		}
	}
	if len(failed) > 0 {
		return note, errors.New(strings.Join(failed, "; "))
	}
	if gated > 0 && gated == total {
		return note, ErrSkipped
	}
	return note, nil
}
