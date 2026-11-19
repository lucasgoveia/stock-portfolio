// Package performance writes daily value snapshots and computes
// time-weighted returns against CDI, IBOV and the S&P 500 in BRL (spec §15).
package performance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// ErrValidation is returned for bad input.
var ErrValidation = errors.New("performance validation error")

const dateLayout = "2006-01-02"

// Segments.
const (
	SegTotal    = "total"
	SegCore     = "core"
	SegTactical = "tactical"
	SegBR       = "br"
	SegUS       = "us"
	assetPrefix = "asset:"
)

// Service writes and reads snapshots.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	trades *trades.Service
	cash   *cash.Service
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, t *trades.Service, c *cash.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, trades: t, cash: c, now: clock}
}

type seg struct{ value, flow decimal.Decimal }

func (s *Service) fx(ctx context.Context, date string) (decimal.Decimal, error) {
	r, err := s.q.LatestFXRate(ctx, db.LatestFXRateParams{Pair: "USDBRL", RefDate: date})
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(r.Rate)
}

// Snapshot computes and stores every segment for one day. Assets without a
// price on or before the date are valued at their cost (first days only).
func (s *Service) Snapshot(ctx context.Context, date string) error {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return fmt.Errorf("%w: date must be YYYY-MM-DD", ErrValidation)
	}
	list, err := s.assets.List(ctx, assets.Filter{})
	if err != nil {
		return err
	}
	segs := map[string]*seg{}
	add := func(name string, value, flow decimal.Decimal) {
		if segs[name] == nil {
			segs[name] = &seg{}
		}
		segs[name].value = segs[name].value.Add(value)
		segs[name].flow = segs[name].flow.Add(flow)
	}
	usd := decimal.Zero
	usdLoaded := false
	usdRate := func() (decimal.Decimal, error) {
		if !usdLoaded {
			v, err := s.fx(ctx, date)
			if err != nil {
				return decimal.Zero, fmt.Errorf("no USDBRL on or before %s: %w", date, err)
			}
			usd, usdLoaded = v, true
		}
		return usd, nil
	}
	for _, a := range list {
		pos, err := s.trades.PositionAt(ctx, a.ID, date)
		if err != nil {
			return err
		}
		ts, err := s.trades.List(ctx, a.ID)
		if err != nil {
			return err
		}
		// Price on or before the day, in the asset's currency (nil: none yet).
		var px *decimal.Decimal
		if price, err := s.q.AssetPriceAt(ctx, db.AssetPriceAtParams{AssetID: a.ID, RefDate: date}); err == nil {
			v, err := decimal.NewFromString(price.Close)
			if err != nil {
				return err
			}
			px = &v
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		fxToBRL := decimal.NewFromInt(1)
		if a.Currency == "USD" && px != nil {
			r, err := usdRate()
			if err != nil {
				return err
			}
			fxToBRL = r
		}
		// Trade flows (spec §15): a buy or sell is valued at the day's close,
		// the same basis as the end-of-day value, so the move between the
		// execution price and the close is neither return nor flow. Without
		// a price the cash amount is used.
		flow := decimal.Zero
		for _, t := range ts {
			if t.Date != date {
				continue
			}
			var amt decimal.Decimal
			switch {
			case px != nil:
				amt = t.Quantity.Mul(*px).Mul(fxToBRL)
			case t.Side == trades.SideBuy:
				amt = t.Quantity.Mul(t.Price).Add(t.Fees).Mul(t.FXRate)
			default:
				amt = t.Quantity.Mul(t.Price).Sub(t.Fees).Mul(t.FXRate)
			}
			if t.Side == trades.SideBuy {
				flow = flow.Add(amt)
			} else {
				flow = flow.Sub(amt)
			}
		}
		if pos.Quantity.IsZero() && flow.IsZero() {
			continue
		}
		value := decimal.Zero
		if pos.Quantity.IsPositive() {
			if px == nil {
				value = pos.TotalCostBRL
			} else {
				value = pos.Quantity.Mul(*px).Mul(fxToBRL)
			}
		}
		add(assetPrefix+strconv.FormatInt(a.ID, 10), value, flow)
		add(a.Sleeve, value, flow)
		if a.Market == assets.MarketUS {
			add(SegUS, value, flow)
		} else {
			add(SegBR, value, flow)
		}
		add(SegTotal, value, flow)
	}
	sum, err := s.cash.Summary(ctx, date)
	if err != nil && !errors.Is(err, cash.ErrNoRate) {
		return err
	}
	cashFlow := decimal.Zero
	moves, err := s.q.CashFlowsOn(ctx, date)
	if err != nil {
		return err
	}
	for _, m := range moves {
		amt, err := decimal.NewFromString(m.Amount)
		if err != nil {
			return err
		}
		if m.Currency == "USD" {
			r, err := usdRate()
			if err != nil {
				return err
			}
			amt = amt.Mul(r)
		}
		if m.Type == cash.MoveWithdraw {
			amt = amt.Neg()
		}
		cashFlow = cashFlow.Add(amt)
	}
	add(SegTotal, sum.TotalBRL, cashFlow)

	if err := s.q.DeletePerformanceDay(ctx, date); err != nil {
		return err
	}
	for name, v := range segs {
		if err := s.q.UpsertPerformanceSnapshot(ctx, db.UpsertPerformanceSnapshotParams{
			RefDate: date, Segment: name, Value: v.value.Round(8).String(), Flow: v.flow.Round(8).String(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Backfill snapshots every weekday from the first trade or cash movement (or
// from, if later) up to to. It returns the number of days written.
func (s *Service) Backfill(ctx context.Context, from, to time.Time) (int, error) {
	first, err := s.q.FirstActivityDate(ctx)
	if err != nil {
		return 0, err
	}
	if first == "" {
		return 0, nil
	}
	start, err := time.Parse(dateLayout, first)
	if err != nil {
		return 0, err
	}
	if from.After(start) {
		start = from
	}
	n := 0
	for d := start; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		if err := s.Snapshot(ctx, d.Format(dateLayout)); err != nil {
			return n, fmt.Errorf("%s: %w", d.Format(dateLayout), err)
		}
		n++
	}
	return n, nil
}

// SnapshotToday writes today's snapshot (daily job step).
func (s *Service) SnapshotToday(ctx context.Context, day time.Time) (string, error) {
	date := day.Format(dateLayout)
	if err := s.Snapshot(ctx, date); err != nil {
		return "", err
	}
	return "snapshot " + date, nil
}

// Segment is one series' result over the period.
type Segment struct {
	Segment    string          `json:"segment"`
	Label      string          `json:"label"`
	StartValue decimal.Decimal `json:"start_value"`
	EndValue   decimal.Decimal `json:"end_value"`
	NetFlows   decimal.Decimal `json:"net_flows"`
	// TWR is the time-weighted return over the period (0.1 = +10%).
	TWR decimal.Decimal `json:"twr"`
}

// SeriesPoint is one day of cumulative indexes (1 = start of the period).
type SeriesPoint struct {
	Date     string           `json:"date"`
	Total    decimal.Decimal  `json:"total"`
	Value    decimal.Decimal  `json:"value"`
	CDI      *decimal.Decimal `json:"cdi"`
	IBOV     *decimal.Decimal `json:"ibov"`
	SP500BRL *decimal.Decimal `json:"sp500_brl"`
}

// Report is the performance page.
type Report struct {
	From       string        `json:"from"`
	To         string        `json:"to"`
	Series     []SeriesPoint `json:"series"`
	Segments   []Segment     `json:"segments"`
	Benchmarks struct {
		CDI      *decimal.Decimal `json:"cdi"`
		IBOV     *decimal.Decimal `json:"ibov"`
		SP500BRL *decimal.Decimal `json:"sp500_brl"`
	} `json:"benchmarks"`
}

func (s *Service) series(ctx context.Context, segment, from, to string) ([]engine.FlowPoint, error) {
	rows, err := s.q.PerformanceSeries(ctx, db.PerformanceSeriesParams{Segment: segment, FromDate: from, ToDate: to})
	if err != nil {
		return nil, err
	}
	out := make([]engine.FlowPoint, 0, len(rows))
	for _, r := range rows {
		v, err := decimal.NewFromString(r.Value)
		if err != nil {
			return nil, err
		}
		f, err := decimal.NewFromString(r.Flow)
		if err != nil {
			return nil, err
		}
		out = append(out, engine.FlowPoint{Date: r.RefDate, Value: v, Flow: f})
	}
	return out, nil
}

// periodTWR chains the period's TWR. The first day's flow is the starting
// capital: it is reported as a flow (start value 0), so start + flows + gain
// reconcile to the end value.
func periodTWR(pts []engine.FlowPoint) (engine.FlowPoint, engine.FlowPoint, decimal.Decimal, decimal.Decimal) {
	if len(pts) == 0 {
		z := engine.FlowPoint{}
		return z, z, decimal.Zero, decimal.Zero
	}
	idx := engine.TWR(pts)
	flows := decimal.Zero
	first := pts[0]
	if first.Flow.IsPositive() {
		flows = first.Flow
		first.Value = decimal.Zero
	}
	for _, p := range pts[1:] {
		flows = flows.Add(p.Flow)
	}
	return first, pts[len(pts)-1], flows, idx[len(idx)-1].Index.Sub(decimal.NewFromInt(1))
}

// Report computes every segment's TWR and the benchmark indexes between
// from and to (YYYY-MM-DD; defaults: one year back to today).
func (s *Service) Report(ctx context.Context, from, to string) (Report, error) {
	if to == "" {
		to = s.now().Format(dateLayout)
	}
	if from == "" {
		t, _ := time.Parse(dateLayout, to)
		from = t.AddDate(-1, 0, 0).Format(dateLayout)
	}
	for _, d := range []string{from, to} {
		if _, err := time.Parse(dateLayout, d); err != nil {
			return Report{}, fmt.Errorf("%w: from and to must be YYYY-MM-DD", ErrValidation)
		}
	}
	r := Report{From: from, To: to, Series: []SeriesPoint{}, Segments: []Segment{}}
	names, err := s.q.PerformanceSegments(ctx)
	if err != nil {
		return r, err
	}
	tickers := map[string]string{}
	if list, err := s.assets.List(ctx, assets.Filter{}); err == nil {
		for _, a := range list {
			tickers[assetPrefix+strconv.FormatInt(a.ID, 10)] = a.Ticker
		}
	}
	labels := map[string]string{SegTotal: "Total", SegCore: "Core", SegTactical: "Tactical", SegBR: "Brazil", SegUS: "United States"}
	order := map[string]int{SegTotal: 0, SegCore: 1, SegTactical: 2, SegBR: 3, SegUS: 4}
	var total []engine.FlowPoint
	for _, name := range names {
		pts, err := s.series(ctx, name, from, to)
		if err != nil {
			return r, err
		}
		if len(pts) == 0 {
			continue
		}
		first, last, flows, twr := periodTWR(pts)
		label := labels[name]
		if label == "" {
			label = tickers[name]
		}
		r.Segments = append(r.Segments, Segment{Segment: name, Label: label, StartValue: first.Value,
			EndValue: last.Value, NetFlows: flows, TWR: twr.Round(6)})
		if name == SegTotal {
			total = pts
		}
	}
	sort.SliceStable(r.Segments, func(i, j int) bool {
		oi, iok := order[r.Segments[i].Segment]
		oj, jok := order[r.Segments[j].Segment]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		}
		return r.Segments[i].Label < r.Segments[j].Label
	})
	if len(total) == 0 {
		return r, nil
	}
	idx := engine.TWR(total)
	cdi, ibov, sp, err := s.benchmarks(ctx, total[0].Date, to)
	if err != nil {
		return r, err
	}
	for i, p := range total {
		pt := SeriesPoint{Date: p.Date, Total: idx[i].Index, Value: p.Value}
		pt.CDI, pt.IBOV, pt.SP500BRL = at(cdi, p.Date), at(ibov, p.Date), at(sp, p.Date)
		r.Series = append(r.Series, pt)
	}
	lastDate := total[len(total)-1].Date
	minus1 := func(v *decimal.Decimal) *decimal.Decimal {
		if v == nil {
			return nil
		}
		x := v.Sub(decimal.NewFromInt(1)).Round(6)
		return &x
	}
	r.Benchmarks.CDI, r.Benchmarks.IBOV, r.Benchmarks.SP500BRL = minus1(at(cdi, lastDate)), minus1(at(ibov, lastDate)), minus1(at(sp, lastDate))
	return r, nil
}

// at returns the index on or before date (nil when the series starts later).
func at(series []engine.IndexPoint, date string) *decimal.Decimal {
	var v *decimal.Decimal
	for i := range series {
		if series[i].Date > date {
			break
		}
		x := series[i].Index
		v = &x
	}
	return v
}

func (s *Service) benchmarks(ctx context.Context, from, to string) (cdi, ibov, sp []engine.IndexPoint, err error) {
	rates, err := s.q.ListCDIRates(ctx, db.ListCDIRatesParams{RefDate: from, RefDate_2: to})
	if err != nil {
		return nil, nil, nil, err
	}
	var dates []string
	var pct []decimal.Decimal
	for _, r := range rates {
		v, err := decimal.NewFromString(r.RatePct)
		if err != nil {
			return nil, nil, nil, err
		}
		dates, pct = append(dates, r.RefDate), append(pct, v)
	}
	cdi = engine.CDIIndex(dates, pct)
	load := func(code string, brl bool) ([]engine.IndexPoint, error) {
		rows, err := s.q.IndexHistory(ctx, db.IndexHistoryParams{IndexCode: code, FromDate: from, ToDate: to})
		if err != nil {
			return nil, err
		}
		var ds []string
		var cs []decimal.Decimal
		for _, r := range rows {
			v, err := decimal.NewFromString(r.Close)
			if err != nil {
				return nil, err
			}
			if brl {
				fx, err := s.fx(ctx, r.RefDate)
				if err != nil {
					continue // no FX yet for that day
				}
				v = v.Mul(fx)
			}
			ds, cs = append(ds, r.RefDate), append(cs, v)
		}
		return engine.PriceIndex(ds, cs), nil
	}
	if ibov, err = load("IBOV", false); err != nil {
		return nil, nil, nil, err
	}
	if sp, err = load("SP500", true); err != nil {
		return nil, nil, nil, err
	}
	return cdi, ibov, sp, nil
}
