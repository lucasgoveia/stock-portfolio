package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// historyYears is the history window the daily job backfills, the same one
// the beta step reads.
const historyYears = 2

// indexSlackDays is how much later than the window start an index's earliest
// stored point may be (holidays, weekends) before it counts as short.
const indexSlackDays = 10

// historyStart is where the backfill window begins for a run day.
func historyStart(day time.Time) time.Time { return day.AddDate(-historyYears, 0, 0) }

func assetSubject(id int64) string { return fmt.Sprintf("asset:%d", id) }

// backfilled returns the subjects already backfilled from a source.
func (p *pipeline) backfilled(ctx context.Context, source string) (map[string]bool, error) {
	subjects, err := p.q.ListHistoryBackfills(ctx, source)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(subjects))
	for _, s := range subjects {
		out[s] = true
	}
	return out, nil
}

// markBackfilled records a successful history fetch so it is not repeated.
func (p *pipeline) markBackfilled(ctx context.Context, subject, source string, from time.Time) error {
	return p.q.UpsertHistoryBackfill(ctx, db.UpsertHistoryBackfillParams{
		Subject: subject, Source: source, FromDate: from.Format(dateLayout), DoneAt: p.stamp(),
	})
}

// cotahistBackfill loads two years of raw closes for the B3 assets not yet
// backfilled, scanning each yearly file once for all of them.
func (p *pipeline) cotahistBackfill(ctx context.Context, day time.Time, list []db.Asset) (int, error) {
	done, err := p.backfilled(ctx, "cotahist")
	if err != nil {
		return 0, err
	}
	ids := map[string]int64{}
	var tickers []string
	var pending []db.Asset
	for _, a := range list {
		if !done[assetSubject(a.ID)] {
			ids[a.Ticker] = a.ID
			tickers = append(tickers, a.Ticker)
			pending = append(pending, a)
		}
	}
	if len(pending) == 0 {
		return 0, nil
	}
	from := historyStart(day)
	filter := marketdata.LotFilter(tickers...)
	store := func(r marketdata.CotahistRecord) error {
		if r.Date.Before(from) {
			return nil
		}
		return p.upsertPrice(ctx, ids[r.Ticker], r.Date, "cotahist", r.Close.String(), "")
	}
	for y := from.Year(); y <= day.Year(); y++ {
		err := p.d.Cotahist.Scan(ctx, marketdata.CotahistYearly, time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), filter, store)
		if marketdata.IsNotFound(err) && y == day.Year() {
			err = p.cotahistMonths(ctx, day, filter, store)
		}
		if err != nil {
			return 0, fmt.Errorf("backfill %d: %w", y, err)
		}
	}
	for _, a := range pending {
		if err := p.markBackfilled(ctx, assetSubject(a.ID), "cotahist", from); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
}

// cotahistMonths covers the current year, month by month, when its yearly file
// is not published yet. A missing month is skipped: the daily files cover it.
func (p *pipeline) cotahistMonths(ctx context.Context, day time.Time, filter marketdata.CotahistFilter, fn func(marketdata.CotahistRecord) error) error {
	for mo := time.January; mo <= day.Month(); mo++ {
		err := p.d.Cotahist.Scan(ctx, marketdata.CotahistMonthly, time.Date(day.Year(), mo, 1, 0, 0, 0, 0, time.UTC), filter, fn)
		if err != nil && !marketdata.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// indexFrom plans an index fetch: from where, and whether it is a backfill
// (the index is not marked done and its stored history is shorter than the
// window).
func (p *pipeline) indexFrom(ctx context.Context, day time.Time, code string) (time.Time, bool, error) {
	short := day.AddDate(0, 0, -14)
	done, err := p.backfilled(ctx, "index")
	if err != nil {
		return short, false, err
	}
	if done["index:"+code] {
		return short, false, nil
	}
	earliest, err := p.q.EarliestIndexDate(ctx, code)
	if err != nil {
		return short, false, err
	}
	from := historyStart(day)
	if earliest != "" && earliest <= from.AddDate(0, 0, indexSlackDays).Format(dateLayout) {
		return short, false, nil
	}
	return from, true, nil
}
