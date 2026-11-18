package marketdata

import (
	"sort"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// Canonical financial concepts shared by the CVM and SEC clients. Amounts are
// in the reporting currency (full units, not thousands), shares are counts,
// per-share values are in currency per share.
const (
	ConceptRevenue      = "revenue"
	ConceptEBIT         = "ebit"
	ConceptNetIncome    = "net_income" // attributable to the controlling shareholders
	ConceptEPSDiluted   = "eps_diluted"
	ConceptEquity       = "equity" // attributable to the controlling shareholders
	ConceptShares       = "shares" // outstanding at the period end
	ConceptSharesDilAvg = "shares_diluted_avg"
	ConceptSharesCover  = "shares_cover" // latest count on the filing cover (SEC dei)
	ConceptCash         = "cash"         // cash, equivalents and short-term investments
	ConceptDebt         = "debt"         // financial debt (loans, debentures, commercial paper)
	ConceptOCF          = "operating_cash_flow"
	ConceptCapex        = "capex" // positive outflow for fixed and intangible assets
	ConceptDA           = "depreciation_amortization"
	ConceptDPS          = "dividends_per_share"
)

// Period types.
const (
	PeriodAnnual    = "annual"
	PeriodQuarterly = "quarterly"
	PeriodTTM       = "ttm" // trailing 12 months, from a dividend feed
)

// FinancialPeriod holds the concepts found for one fiscal period.
type FinancialPeriod struct {
	End    time.Time // civil date, UTC midnight
	Type   string    // PeriodAnnual or PeriodQuarterly
	Filed  string    // filing date (SEC) or document version (CVM)
	Values map[string]money.Decimal
}

// FinancialFacts is what a fundamentals source returns for one company.
type FinancialFacts struct {
	Source   string // "cvm" or "sec"
	Entity   string
	Currency string
	Periods  []FinancialPeriod // ascending by End
	// Quarters are the Q1 to Q3 income statement figures (SEC: from 10-Q
	// filings, filled by EDGAR.Financials; CVM: from QuarterlyFinancials).
	Quarters []QuarterFact
	// Notes are caveats for the user (skipped years, approximations).
	Notes []string
}

func sortPeriods(p []FinancialPeriod) {
	sort.Slice(p, func(i, j int) bool {
		if !p[i].End.Equal(p[j].End) {
			return p[i].End.Before(p[j].End)
		}
		return p[i].Type < p[j].Type
	})
}
