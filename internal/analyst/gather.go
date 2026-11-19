package analyst

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// Text budgets, in characters (about four per token): the whole prompt stays
// near 40k tokens so a run costs cents.
const (
	budgetAnnualHead = 12000
	budgetMDA        = 30000
	budgetExhibit    = 25000
	budgetCover      = 8000
	filings8K        = 3
	announcementsBR  = 10
)

var mdaMarker = []string{
	"management's discussion and analysis of financial condition",
	"management’s discussion and analysis of financial condition",
}

// excerpt keeps the start of a periodic report plus its MD&A section. The
// first mention of MD&A is the table of contents, so the second mention (in
// document order) is where the section starts.
func excerpt(text string) string {
	low := strings.ToLower(text)
	if len(low) != len(text) {
		return truncate(text, budgetAnnualHead+budgetMDA)
	}
	var starts []int
	for _, m := range mdaMarker {
		for off := 0; ; {
			i := strings.Index(low[off:], m)
			if i < 0 {
				break
			}
			starts = append(starts, off+i)
			off += i + len(m)
		}
	}
	if len(starts) < 2 {
		return truncate(text, budgetAnnualHead+budgetMDA)
	}
	sort.Ints(starts)
	return truncate(text, budgetAnnualHead) + "\n[...]\n" + truncate(text[starts[1]:], budgetMDA)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n[truncated]"
}

// gather collects everything the model is given. Missing pieces are skipped,
// never fatal: a run with only the thesis is still useful.
func (s *Service) gather(ctx context.Context, a db.Asset) (Inputs, error) {
	in, err := s.baseInputs(ctx, a)
	if err != nil {
		return in, err
	}
	switch a.Market {
	case assets.MarketUS:
		s.gatherUS(ctx, a, &in)
	default:
		s.gatherBR(ctx, a, &in)
	}
	return in, nil
}

// baseInputs is the part of the inputs that needs no filings: the thesis, the
// current fair value and the fundamentals status with the DCF base figures.
func (s *Service) baseInputs(ctx context.Context, a db.Asset) (Inputs, error) {
	in := Inputs{
		Ticker: a.Ticker, Market: a.Market, Currency: a.Currency,
		Documents: []Document{}, Skipped: []string{},
	}
	switch t, err := s.d.Thesis.Get(ctx, a.ID); {
	case err == nil:
		ti := &ThesisInput{Text: t.Text, Status: t.Status, BreakConditions: []string{}}
		for _, c := range t.BreakConditions {
			ti.BreakConditions = append(ti.BreakConditions, c.Text)
		}
		in.Thesis = ti
	case !errors.Is(err, thesis.ErrNotFound):
		return in, err
	}
	switch fv, err := s.d.Valuation.Latest(ctx, a.ID); {
	case err == nil:
		in.FairValue = &FairValueInput{
			Method: fv.Method, ValuePerShare: fv.ValuePerShare, Currency: fv.Currency, AsOf: fv.AsOf,
		}
		if fv.Method == valuation.MethodScenarioDCF {
			in.FairValue.Scenarios = json.RawMessage(fv.Inputs)
		}
	case !errors.Is(err, valuation.ErrNotFound):
		return in, err
	}
	panel, err := s.d.Fundamentals.Get(ctx, a.ID, fundamentals.Params{})
	if err != nil {
		return in, err
	}
	in.Fundamentals = panel.Status
	in.DCFBase = dcfBase(panel)
	in.fundamentalsJSON = fundamentalsSummary(panel)
	return in, nil
}

// DCFBaseOf returns the base figures of a fundamentals panel (nil when the
// fundamentals are not available).
func DCFBaseOf(panel fundamentals.FundamentalsPanel) *DCFBase { return dcfBase(panel) }

// dcfBase returns the base revenue, net debt and shares of a panel, or nil
// when the fundamentals are not available.
func dcfBase(panel fundamentals.FundamentalsPanel) *DCFBase {
	m := panel.FundamentalMetrics
	if panel.Status != fundamentals.StatusOK || m.Revenue == nil || m.Shares == nil || !m.Revenue.IsPositive() || !m.Shares.IsPositive() {
		return nil
	}
	b := DCFBase{BaseRevenue: m.Revenue.Decimal, Shares: m.Shares.Decimal, NetDebt: decimal.Zero}
	if m.NetDebt != nil {
		b.NetDebt = m.NetDebt.Decimal
	}
	return &b
}

func fundamentalsSummary(p fundamentals.FundamentalsPanel) string {
	if p.Status != fundamentals.StatusOK {
		return "Fundamentals are not available (" + p.Status + "): " + p.Message
	}
	b, err := json.Marshal(map[string]any{
		"as_of": p.AsOf, "source": p.SourceName, "currency": p.Currency, "metrics": p.FundamentalMetrics, "history": p.History,
	})
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *Service) gatherUS(ctx context.Context, a db.Asset, in *Inputs) {
	if s.d.US == nil || a.Cik == "" {
		in.Skipped = append(in.Skipped, "SEC filings: no CIK on the asset (refresh its fundamentals first)")
		return
	}
	list, err := s.d.US.RecentFilings(ctx, a.Cik, []string{"10-K", "10-Q", "8-K"}, filings8K)
	if err != nil {
		in.Skipped = append(in.Skipped, "SEC filings list: "+err.Error())
		return
	}
	seen := map[string]int{}
	for _, f := range list {
		seen[f.Form]++
		if f.Form != "8-K" && seen[f.Form] > 1 {
			continue
		}
		url, kind, budget := f.URL, f.Form, budgetCover
		switch f.Form {
		case "10-K", "10-Q":
			budget = 0
		case "8-K":
			// Earnings releases are the 99.1 exhibit of an 8-K (item 2.02).
			if strings.Contains(f.Items, "2.02") {
				ex, err := s.d.US.Exhibit99(ctx, f)
				if err != nil {
					in.Skipped = append(in.Skipped, f.Title+" exhibit: "+err.Error())
				} else if ex != "" {
					url, kind, budget = ex, "8-K exhibit 99.1", budgetExhibit
				}
			}
		}
		text, err := s.d.US.Document(ctx, url, budget)
		if err != nil {
			in.Skipped = append(in.Skipped, fmt.Sprintf("%s: %v", f.Title, err))
			continue
		}
		if budget == 0 {
			text = excerpt(text)
		}
		in.Documents = append(in.Documents, Document{
			Kind: kind, Title: a.Ticker + " " + f.Title, URL: url, Date: f.FilingDate, Chars: len([]rune(text)), Text: text,
		})
	}
}

func (s *Service) gatherBR(ctx context.Context, a db.Asset, in *Inputs) {
	if s.d.BR == nil || a.Cnpj == "" {
		in.Skipped = append(in.Skipped, "CVM announcements: no CNPJ on the asset")
		return
	}
	year := s.now().In(s.loc).Year()
	var all []marketdata.Announcement
	for y := year; y >= year-1 && len(all) < announcementsBR; y-- {
		list, err := s.d.BR.Announcements(ctx, a.Cnpj, y, []string{"Fato Relevante", "Comunicado ao Mercado"}, announcementsBR-len(all))
		switch {
		case marketdata.IsNotFound(err):
			continue
		case err != nil:
			in.Skipped = append(in.Skipped, fmt.Sprintf("CVM announcements %d: %v", y, err))
			return
		}
		all = append(all, list...)
	}
	for _, an := range all {
		in.Documents = append(in.Documents, Document{
			Kind: strings.ToLower(an.Category), Title: an.Subject, URL: an.URL, Date: an.Date,
		})
	}
}
