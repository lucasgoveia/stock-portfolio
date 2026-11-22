package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Backfill loads history from `from` up to today for every configured source:
// COTAHIST yearly files (raw B3 closes), US daily prices, PTAX, CDI, Tesouro
// Selic and index closes. It is idempotent and reports per-source results.
func Backfill(ctx context.Context, d Deps, from time.Time) ([]StepResult, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	p := &pipeline{d: d, q: db.New(d.Conn)}
	today := civilDay(d.Now().In(SaoPaulo()))
	from = civilDay(from)
	steps := []Step{
		{"fx", func(ctx context.Context, _ time.Time) (string, error) { return p.fxRange(ctx, from, today) }},
		{"cdi", func(ctx context.Context, _ time.Time) (string, error) { return p.cdiRange(ctx, from, today) }},
		{"tesouro_selic", func(ctx context.Context, _ time.Time) (string, error) {
			return p.tesouro(ctx, from.AddDate(0, 0, 10))
		}},
		{"cotahist", func(ctx context.Context, _ time.Time) (string, error) { return p.cotahistYears(ctx, from, today) }},
		{"us_prices", func(ctx context.Context, _ time.Time) (string, error) { return p.usRange(ctx, from, today) }},
		{"indices", func(ctx context.Context, _ time.Time) (string, error) {
			return p.syncIndices(ctx, func(string) (time.Time, bool, error) { return from, false, nil })
		}},
		{"snapshots", func(ctx context.Context, _ time.Time) (string, error) {
			if d.Snapshots == nil {
				return "", ErrSkipped
			}
			n, err := d.Snapshots.Backfill(ctx, from, today)
			return fmt.Sprintf("%d days", n), err
		}},
	}
	var out []StepResult
	for _, st := range steps {
		t0 := d.Now()
		note, err := st.Run(ctx, today)
		r := StepResult{Name: st.Name, Status: StepOK, Note: note, DurationMS: d.Now().Sub(t0).Milliseconds()}
		switch {
		case err == nil:
		case errors.Is(err, ErrSkipped):
			r.Status = StepSkipped
		default:
			r.Status, r.Error = StepFailed, summarize(err)
		}
		out = append(out, r)
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
	return out, nil
}

func (p *pipeline) fxRange(ctx context.Context, from, to time.Time) (string, error) {
	if p.d.FX == nil {
		return "", ErrSkipped
	}
	rates, err := p.d.FX.USDBRL(ctx, from, to)
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

func (p *pipeline) cdiRange(ctx context.Context, from, to time.Time) (string, error) {
	if p.d.CDI == nil {
		return "", ErrSkipped
	}
	pts, err := p.d.CDI.CDI(ctx, from, to)
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

func (p *pipeline) cotahistYears(ctx context.Context, from, to time.Time) (string, error) {
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
	ids := map[string]int64{}
	tickers := make([]string, 0, len(list))
	for _, a := range list {
		ids[a.Ticker] = a.ID
		tickers = append(tickers, a.Ticker)
	}
	n := 0
	for y := from.Year(); y <= to.Year(); y++ {
		err := p.d.Cotahist.Scan(ctx, marketdata.CotahistYearly, time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), marketdata.LotFilter(tickers...), func(r marketdata.CotahistRecord) error {
			if r.Date.Before(from) {
				return nil
			}
			n++
			return p.upsertPrice(ctx, ids[r.Ticker], r.Date, "cotahist", r.Close.String(), "")
		})
		if err != nil {
			return "", fmt.Errorf("year %d: %w", y, err)
		}
	}
	return fmt.Sprintf("%d closes, %d tickers", n, len(tickers)), nil
}

func (p *pipeline) usRange(ctx context.Context, from, to time.Time) (string, error) {
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
	n := 0
	for _, a := range list {
		bars, err := p.d.US.DailyPrices(ctx, a.Ticker, from, to)
		if err != nil {
			return "", fmt.Errorf("%s: %w", a.Ticker, err)
		}
		for _, b := range bars {
			if err := p.upsertPrice(ctx, a.ID, b.Date, "tiingo", b.Close.String(), b.AdjClose.String()); err != nil {
				return "", err
			}
			n++
		}
	}
	return fmt.Sprintf("%d bars, %d tickers", n, len(list)), nil
}
