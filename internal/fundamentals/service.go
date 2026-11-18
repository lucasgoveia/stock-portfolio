// Package fundamentals stores company financials from CVM (B3) and SEC EDGAR
// (US) and derives the fundamentals panel of spec §12.2: multiples, yields,
// margin history, current-versus-history ranges and the valuation helpers
// (Bazin, Graham, reverse DCF) of §12.1.
package fundamentals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Errors returned by Service.
var (
	ErrValidation = errors.New("fundamentals validation error")
	// ErrUpstream means a data source failed or had nothing for the company.
	ErrUpstream = errors.New("fundamentals source error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02T15:04:05Z"
)

// RefreshInterval is how old a successful fetch may be before the daily job
// fetches again; failed fetches are retried after RetryInterval.
const (
	RefreshInterval = 7 * 24 * time.Hour
	RetryInterval   = 24 * time.Hour
)

// BRSource returns annual statements for a CNPJ (marketdata.CVM).
type BRSource interface {
	AnnualFinancials(ctx context.Context, cnpj string, years []int) (marketdata.FinancialFacts, error)
}

// USSource resolves tickers and returns company facts (marketdata.EDGAR).
type USSource interface {
	CIKForTicker(ctx context.Context, ticker string) (string, error)
	Financials(ctx context.Context, cik string) (marketdata.FinancialFacts, error)
}

// Sources are the external collaborators; a nil source disables its market.
type Sources struct {
	BR BRSource
	US USSource
	// Dividends supplies trailing B3 dividends (brapi); optional.
	Dividends marketdata.DividendSource
}

// Service manages stored fundamentals.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	src    Sources
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, src Sources, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, src: src, now: clock}
}

func (s *Service) today() string { return s.now().UTC().Format(dateLayout) }

// sourceName is the human name of a source code.
func sourceName(code string) string {
	switch code {
	case "cvm":
		return "CVM Dados Abertos (DFP)"
	case "sec":
		return "SEC EDGAR (XBRL company facts)"
	}
	return ""
}

// identifier returns the identifier kind and value of an asset.
func identifier(a db.Asset) (kind, value string) {
	if a.Market == assets.MarketUS {
		return "cik", a.Cik
	}
	return "cnpj", a.Cnpj
}

func supported(a db.Asset) bool { return a.Kind == assets.KindStock }

// Get returns the fundamentals panel of an asset. Params override helper
// defaults and are not stored.
func (s *Service) Get(ctx context.Context, assetID int64, p Params) (FundamentalsPanel, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return FundamentalsPanel{}, err
	}
	kind, id := identifier(a)
	r := FundamentalsPanel{
		AssetID: a.ID, Ticker: a.Ticker, Market: a.Market, Currency: a.Currency,
		IdentifierKind: kind, Identifier: id,
		History: []HistoryPoint{}, Notes: []string{},
	}
	r.Helpers = Helpers{
		Bazin: &HelperValue{Method: "bazin", Note: "needs fundamentals"}, Graham: &HelperValue{Method: "graham", Note: "needs fundamentals"},
		ReverseDCF: &ReverseDCF{Status: "unavailable", Note: "needs fundamentals"},
	}
	if !supported(a) {
		r.Status = StatusUnsupported
		r.Message = "Fundamentals cover stocks only (not ETFs or BDRs)."
		return r, nil
	}
	sync, serr := s.q.GetFundamentalSync(ctx, a.ID)
	hasSync := serr == nil
	if serr != nil && !errors.Is(serr, sql.ErrNoRows) {
		return FundamentalsPanel{}, fmt.Errorf("fundamentals sync: %w", serr)
	}
	if hasSync {
		r.Source, r.SourceName, r.FetchedAt, r.AsOf = sync.Source, sourceName(sync.Source), sync.FetchedAt, sync.AsOf
		_ = json.Unmarshal([]byte(sync.Notes), &r.Notes)
		if sync.Status == "error" {
			r.LastError = sync.Message
		}
	}
	periods, err := s.loadPeriods(ctx, a.ID)
	if err != nil {
		return FundamentalsPanel{}, err
	}
	if len(periods) == 0 {
		switch {
		case a.Market == assets.MarketB3 && id == "":
			r.Status = StatusNeedsIdentifier
			r.Message = "Set the company CNPJ to load statements from CVM."
		default:
			r.Status = StatusNotFetched
			r.Message = "No fundamentals fetched yet."
		}
		return r, nil
	}
	r.Status = StatusOK
	if r.Source == "" { // facts exist without a sync row (manual data)
		r.Source = map[string]string{assets.MarketB3: "cvm", assets.MarketUS: "sec"}[a.Market]
		r.SourceName = sourceName(r.Source)
	}

	in := computeInput{Market: a.Market, Periods: periods, Params: p}
	if row, err := s.q.LatestAssetPrice(ctx, a.ID); err == nil {
		if v, err := decimal.NewFromString(row.Close); err == nil {
			in.Price = &pricePoint{Date: row.RefDate, Close: v}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return FundamentalsPanel{}, fmt.Errorf("latest price: %w", err)
	}
	in.PriceOn = func(end string) (decimal.Decimal, bool) {
		row, err := s.q.AssetPriceOnOrBefore(ctx, db.AssetPriceOnOrBeforeParams{AssetID: a.ID, OnDate: end, FromDate: daysBefore(end, priceWindowDays)})
		if err != nil {
			return decimal.Zero, false
		}
		v, err := decimal.NewFromString(row.Close)
		return v, err == nil
	}
	today := s.today()
	if rows, err := s.q.TrailingDividendsPerShare(ctx, db.TrailingDividendsPerShareParams{
		AssetID: a.ID, FromDate: daysBefore(today, 365), ToDate: today,
	}); err == nil {
		for _, v := range rows {
			if d, err := decimal.NewFromString(v); err == nil {
				in.LocalTTM = in.LocalTTM.Add(d)
			}
		}
	}

	c := compute(in)
	r.FundamentalMetrics, r.History, r.Ranges, r.Helpers = c.FundamentalMetrics, c.History, c.Ranges, c.Helpers
	r.Notes = append(r.Notes, c.Notes...)
	if a.Market == assets.MarketB3 {
		r.Notes = append(r.Notes, "B3 market cap uses one share price for all share classes (ON and PN).")
	}
	if len(c.History) == 0 {
		r.Status = StatusNotFetched
	}
	return r, nil
}

// loadPeriods groups stored facts into periods, ascending.
func (s *Service) loadPeriods(ctx context.Context, assetID int64) ([]period, error) {
	rows, err := s.q.ListFundamentalFacts(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list fundamental facts: %w", err)
	}
	idx := map[string]*period{}
	var order []*period
	for _, r := range rows {
		v, err := decimal.NewFromString(r.Value)
		if err != nil {
			continue
		}
		k := r.PeriodEnd + "|" + r.PeriodType
		p, ok := idx[k]
		if !ok {
			p = &period{End: r.PeriodEnd, Type: r.PeriodType, V: map[string]decimal.Decimal{}}
			idx[k] = p
			order = append(order, p)
		}
		p.V[r.Concept] = v
	}
	out := make([]period, 0, len(order))
	for _, p := range order {
		out = append(out, *p)
	}
	sortPeriods(out)
	return out, nil
}

// Refresh fetches the asset's statements from its source and stores them. It
// records the outcome, so Get can show the last error next to older data.
func (s *Service) Refresh(ctx context.Context, assetID int64) (FundamentalsPanel, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return FundamentalsPanel{}, err
	}
	if !supported(a) {
		return FundamentalsPanel{}, validationf("fundamentals cover stocks only (not ETFs or BDRs)")
	}
	facts, err := s.fetch(ctx, &a)
	if err != nil {
		if !errors.Is(err, ErrValidation) {
			s.recordError(ctx, a.ID, err)
		}
		return FundamentalsPanel{}, err
	}
	if len(facts.Periods) == 0 {
		err := fmt.Errorf("%w: no annual statements found for %s %s", ErrUpstream, strings.ToUpper(facts.Source), s.idLabel(a))
		s.recordError(ctx, a.ID, err)
		return FundamentalsPanel{}, err
	}
	asOf := ""
	for _, p := range facts.Periods {
		end := p.End.Format(dateLayout)
		if end > asOf {
			asOf = end
		}
		for concept, v := range p.Values {
			if err := s.q.UpsertFundamentalFact(ctx, db.UpsertFundamentalFactParams{
				AssetID: a.ID, PeriodEnd: end, PeriodType: p.Type, Concept: concept, Value: v.String(),
				Source: facts.Source, Filed: p.Filed,
			}); err != nil {
				return FundamentalsPanel{}, fmt.Errorf("store fact: %w", err)
			}
		}
	}
	notes := facts.Notes
	if a.Market == assets.MarketB3 {
		notes = append(notes, s.trailingDividends(ctx, a)...)
	}
	nj, _ := json.Marshal(notesOrEmpty(notes))
	if err := s.q.UpsertFundamentalSync(ctx, db.UpsertFundamentalSyncParams{
		AssetID: a.ID, Source: facts.Source, FetchedAt: s.now().UTC().Format(tsLayout), AsOf: asOf,
		Status: "ok", Message: "", Notes: string(nj),
	}); err != nil {
		return FundamentalsPanel{}, fmt.Errorf("store sync: %w", err)
	}
	return s.Get(ctx, a.ID, Params{})
}

func notesOrEmpty(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}

func (s *Service) idLabel(a db.Asset) string {
	k, v := identifier(a)
	return strings.ToUpper(k) + " " + v
}

// recordError stores a failed attempt while keeping the previous source,
// as-of date and notes.
func (s *Service) recordError(ctx context.Context, assetID int64, cause error) {
	prev, err := s.q.GetFundamentalSync(ctx, assetID)
	if err != nil {
		prev = db.FundamentalSync{Notes: "[]"}
	}
	_ = s.q.UpsertFundamentalSync(ctx, db.UpsertFundamentalSyncParams{
		AssetID: assetID, Source: prev.Source, FetchedAt: s.now().UTC().Format(tsLayout), AsOf: prev.AsOf,
		Status: "error", Message: cause.Error(), Notes: prev.Notes,
	})
}

// fetch loads facts for the asset, resolving and saving the SEC CIK of a US
// asset that has none.
func (s *Service) fetch(ctx context.Context, a *db.Asset) (marketdata.FinancialFacts, error) {
	if a.Market == assets.MarketUS {
		if s.src.US == nil {
			return marketdata.FinancialFacts{}, validationf("US fundamentals source is not configured")
		}
		if a.Cik == "" {
			cik, err := s.src.US.CIKForTicker(ctx, a.Ticker)
			if err != nil {
				return marketdata.FinancialFacts{}, fmt.Errorf("%w: %v (set the CIK manually)", ErrUpstream, err)
			}
			upd, err := s.assets.Update(ctx, a.ID, assets.UpdateInput{CIK: &cik})
			if err != nil {
				return marketdata.FinancialFacts{}, err
			}
			*a = upd
		}
		f, err := s.src.US.Financials(ctx, a.Cik)
		if err != nil {
			return marketdata.FinancialFacts{}, fmt.Errorf("%w: %v", ErrUpstream, err)
		}
		return f, nil
	}
	if s.src.BR == nil {
		return marketdata.FinancialFacts{}, validationf("B3 fundamentals source is not configured")
	}
	if a.Cnpj == "" {
		return marketdata.FinancialFacts{}, validationf("set the company CNPJ before refreshing")
	}
	// First fetch (or a short history) pulls five years of DFP files; later
	// refreshes only the latest two, which catches the new year and restatements.
	year := s.now().UTC().Year()
	n := 2
	if cnt, err := s.q.CountAnnualFundamentalPeriods(ctx, a.ID); err != nil || cnt < MaxHistory {
		n = MaxHistory + 1
	}
	years := make([]int, 0, n)
	for i := 0; i < n; i++ {
		years = append(years, year-i)
	}
	f, err := s.src.BR.AnnualFinancials(ctx, a.Cnpj, years)
	if err != nil {
		return marketdata.FinancialFacts{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return f, nil
}

// trailingDividends stores the trailing 12-month gross dividends per share of
// a B3 asset as a 'ttm' fact and returns notes about it.
func (s *Service) trailingDividends(ctx context.Context, a db.Asset) []string {
	if s.src.Dividends == nil {
		return nil
	}
	now := s.now().UTC()
	from := now.AddDate(-1, 0, 0)
	ca, err := s.src.Dividends.CorporateActions(ctx, a.Ticker, from, now)
	if err != nil {
		return []string{"trailing dividends not refreshed: " + err.Error()}
	}
	sum := decimal.Zero
	for _, d := range ca.CashDividends {
		if d.LastDatePrior.After(from) && !d.LastDatePrior.After(now) {
			sum = sum.Add(d.Rate.Decimal)
		}
	}
	if err := s.q.DeleteFundamentalTTM(ctx, a.ID); err != nil {
		return []string{"trailing dividends not stored: " + err.Error()}
	}
	if err := s.q.UpsertFundamentalFact(ctx, db.UpsertFundamentalFactParams{
		AssetID: a.ID, PeriodEnd: s.today(), PeriodType: marketdata.PeriodTTM, Concept: marketdata.ConceptDPS,
		Value: sum.String(), Source: "brapi", Filed: s.today(),
	}); err != nil {
		return []string{"trailing dividends not stored: " + err.Error()}
	}
	return nil
}

// RefreshStale refreshes tracked stocks whose data is missing or older than
// RefreshInterval (RetryInterval after a failure). It is the daily job step.
func (s *Service) RefreshStale(ctx context.Context, now time.Time) (string, error) {
	var tracked []db.Asset
	for _, st := range []string{assets.StatusHolding, assets.StatusWatchlist} {
		l, err := s.assets.List(ctx, assets.Filter{Status: st})
		if err != nil {
			return "", err
		}
		tracked = append(tracked, l...)
	}
	var refreshed, fresh, skipped int
	var failed []string
	for _, a := range tracked {
		if !supported(a) {
			continue
		}
		if a.Market == assets.MarketB3 && a.Cnpj == "" {
			skipped++
			continue
		}
		if sync, err := s.q.GetFundamentalSync(ctx, a.ID); err == nil {
			at, perr := time.Parse(tsLayout, sync.FetchedAt)
			wait := RefreshInterval
			if sync.Status == "error" {
				wait = RetryInterval
			}
			if perr == nil && now.Sub(at) < wait {
				fresh++
				continue
			}
		}
		if _, err := s.Refresh(ctx, a.ID); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", a.Ticker, err))
			continue
		}
		refreshed++
	}
	note := fmt.Sprintf("%d refreshed, %d fresh, %d without CNPJ", refreshed, fresh, skipped)
	if refreshed+fresh+skipped == 0 && len(failed) == 0 {
		return "no stocks tracked", nil
	}
	if len(failed) > 0 {
		return note, errors.New(strings.Join(failed, "; "))
	}
	return note, nil
}

// FairValueDraft proposes a fair value version from a helper (bazin or graham). The
// caller stores it through the fair value endpoint.
func (s *Service) Draft(ctx context.Context, assetID int64, method string, p Params) (FairValueDraft, error) {
	rep, err := s.Get(ctx, assetID, p)
	if err != nil {
		return FairValueDraft{}, err
	}
	method = strings.ToLower(strings.TrimSpace(method))
	var h *HelperValue
	inputs := map[string]any{}
	switch method {
	case "bazin":
		h = rep.Helpers.Bazin
		if h != nil && h.Value != nil && rep.FundamentalMetrics.DividendsPerShare != nil {
			inputs["dividends_per_share"] = rep.FundamentalMetrics.DividendsPerShare.String()
			inputs["required_yield"] = h.RequiredYield.String()
		}
	case "graham":
		h = rep.Helpers.Graham
		if h != nil && h.Value != nil && rep.FundamentalMetrics.EPS != nil && rep.FundamentalMetrics.BVPS != nil {
			inputs["eps"] = rep.FundamentalMetrics.EPS.String()
			inputs["bvps"] = rep.FundamentalMetrics.BVPS.String()
			inputs["formula"] = "sqrt(22.5 x EPS x BVPS)"
		}
	default:
		return FairValueDraft{}, validationf("method must be bazin or graham")
	}
	if h == nil || h.Value == nil {
		msg := "helper value is undefined"
		if h != nil && h.Note != "" {
			msg = h.Note
		}
		return FairValueDraft{}, validationf("%s: %s", method, msg)
	}
	inputs["fiscal_year_end"] = rep.FundamentalMetrics.FiscalYearEnd
	inputs["fundamentals_source"] = rep.SourceName
	inputs["fetched_at"] = rep.FetchedAt
	sources := []string{rep.SourceName + " " + strings.ToUpper(rep.IdentifierKind) + " " + rep.Identifier}
	if method == "bazin" && rep.Market == assets.MarketB3 {
		sources = append(sources, "brapi.dev dividends")
	}
	return FairValueDraft{
		Method: method, ValuePerShare: money.Decimal{Decimal: h.Value.Decimal}, Currency: rep.Currency, AsOf: s.today(),
		Inputs: inputs, Sources: sources,
		Note: fmt.Sprintf("%s from fundamentals (fiscal year ended %s)", method, rep.FundamentalMetrics.FiscalYearEnd),
	}, nil
}
