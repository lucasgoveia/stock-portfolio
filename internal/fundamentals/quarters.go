package fundamentals

import (
	"context"
	"fmt"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// quarterYears is how many fiscal years of ITR files a B3 refresh reads (each
// zip is about 30 MB), counted back from the current year.
const quarterYears = 3

// storeQuarters fetches (B3) or takes from the SEC facts (US) the quarterly
// income statement figures, derives each Q4 from the annual figures and stores
// them. A failure only produces a note: the annual refresh still succeeds.
func (s *Service) storeQuarters(ctx context.Context, a db.Asset, facts marketdata.FinancialFacts) []string {
	var notes []string
	quarters := facts.Quarters
	if a.Market == assets.MarketB3 {
		qs, ok := s.src.BR.(BRQuarterlySource)
		if !ok {
			return nil
		}
		year := s.now().UTC().Year()
		years := make([]int, 0, quarterYears)
		for i := 0; i < quarterYears; i++ {
			years = append(years, year-i)
		}
		var qnotes []string
		var err error
		quarters, qnotes, err = qs.QuarterlyFinancials(ctx, a.Cnpj, years)
		if err != nil {
			return []string{"quarterly figures not refreshed: " + err.Error()}
		}
		notes = append(notes, qnotes...)
	}
	quarters = marketdata.DeriveFourthQuarters(facts.Periods, quarters)
	for _, q := range quarters {
		rev, ok := q.Values[marketdata.ConceptRevenue]
		if !ok {
			continue
		}
		shares := figure(q.Values, marketdata.ConceptSharesDilAvg)
		if shares == "" {
			shares = figure(q.Values, marketdata.ConceptShares)
		}
		derived := int64(0)
		if q.Derived {
			derived = 1
		}
		if err := s.q.UpsertFundamentalQuarter(ctx, db.UpsertFundamentalQuarterParams{
			AssetID: a.ID, PeriodEnd: q.End.Format(dateLayout), FiscalQuarter: int64(q.Quarter),
			Revenue: rev.String(), NetIncome: figure(q.Values, marketdata.ConceptNetIncome),
			Eps: figure(q.Values, marketdata.ConceptEPSDiluted), Shares: shares,
			Derived: derived, Source: facts.Source, Filed: q.Filed,
		}); err != nil {
			return append(notes, fmt.Sprintf("quarterly figures not stored: %v", err))
		}
	}
	return notes
}

// figure is the stored form of an optional figure: empty when absent.
func figure(m map[string]money.Decimal, concept string) string {
	if v, ok := m[concept]; ok {
		return v.String()
	}
	return ""
}
