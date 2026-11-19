package fundamentals

import (
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// FundamentalsPanel statuses.
const (
	StatusOK              = "ok"
	StatusNeedsIdentifier = "needs_identifier" // B3 asset without CNPJ
	StatusNotFetched      = "not_fetched"      // identifier known, nothing stored yet
	StatusUnsupported     = "unsupported"      // ETFs and BDRs
)

// FundamentalMetrics are the headline figures. Fields are omitted when they cannot be
// computed; FundamentalsPanel.Notes says why. Multiples are plain numbers (12.5 means
// 12.5x); yields and margins are fractions (0.04 means 4%); amounts are in
// the asset's currency.
type FundamentalMetrics struct {
	FiscalYearEnd     string         `json:"fiscal_year_end,omitempty" doc:"Period end of the annual figures used"`
	Price             *money.Decimal `json:"price,omitempty"`
	PriceDate         string         `json:"price_date,omitempty"`
	Shares            *money.Decimal `json:"shares,omitempty"`
	MarketCap         *money.Decimal `json:"market_cap,omitempty"`
	EnterpriseValue   *money.Decimal `json:"enterprise_value,omitempty"`
	Revenue           *money.Decimal `json:"revenue,omitempty"`
	EBIT              *money.Decimal `json:"ebit,omitempty"`
	EBITDA            *money.Decimal `json:"ebitda,omitempty"`
	NetIncome         *money.Decimal `json:"net_income,omitempty"`
	FCF               *money.Decimal `json:"fcf,omitempty"`
	NetDebt           *money.Decimal `json:"net_debt,omitempty"`
	EPS               *money.Decimal `json:"eps,omitempty"`
	BVPS              *money.Decimal `json:"bvps,omitempty"`
	DividendsPerShare *money.Decimal `json:"dividends_per_share,omitempty"`
	PE                *money.Decimal `json:"pe,omitempty"`
	EVEBIT            *money.Decimal `json:"ev_ebit,omitempty"`
	FCFYield          *money.Decimal `json:"fcf_yield,omitempty" doc:"Fraction of market cap"`
	DividendYield     *money.Decimal `json:"dividend_yield,omitempty" doc:"Fraction of price"`
	NetDebtEBITDA     *money.Decimal `json:"net_debt_ebitda,omitempty"`
}

// HistoryPoint is one fiscal year of the revenue and margin history.
type HistoryPoint struct {
	PeriodEnd  string         `json:"period_end"`
	Revenue    *money.Decimal `json:"revenue,omitempty"`
	EBIT       *money.Decimal `json:"ebit,omitempty"`
	NetIncome  *money.Decimal `json:"net_income,omitempty"`
	EBITMargin *money.Decimal `json:"ebit_margin,omitempty" doc:"Fraction of revenue"`
	NetMargin  *money.Decimal `json:"net_margin,omitempty" doc:"Fraction of revenue"`
}

// RangePoint is a historical multiple at a fiscal year-end.
type RangePoint struct {
	Date  string        `json:"date"`
	Value money.Decimal `json:"value"`
}

// Range places the current multiple within its history. Min and Max are set
// only when at least two year-ends have a stored price; they include the
// current value so Position (0 to 1) is always within the bar.
type Range struct {
	Current  *money.Decimal `json:"current,omitempty"`
	Min      *money.Decimal `json:"min,omitempty"`
	Max      *money.Decimal `json:"max,omitempty"`
	Position *money.Decimal `json:"position,omitempty"`
	Points   []RangePoint   `json:"points"`
}

// Ranges are the current-versus-history bars.
type Ranges struct {
	PE     *Range `json:"pe,omitempty"`
	EVEBIT *Range `json:"ev_ebit,omitempty"`
}

// HelperValue is a Bazin or Graham value per share.
type HelperValue struct {
	Method        string         `json:"method" enum:"bazin,graham"`
	Value         *money.Decimal `json:"value,omitempty" doc:"Omitted when undefined"`
	RequiredYield *money.Decimal `json:"required_yield,omitempty" doc:"Bazin only"`
	Note          string         `json:"note"`
}

// ReverseDCF is the market-implied growth at the current price (informational).
type ReverseDCF struct {
	Status         string         `json:"status" enum:"ok,below_range,above_range,unavailable"`
	ImpliedGrowth  *money.Decimal `json:"implied_growth,omitempty" doc:"Constant yearly revenue growth over 5 years, as a fraction"`
	Basis          string         `json:"basis" enum:"ebit_ev,net_pe" doc:"ebit_ev when the margin is the EBIT margin (net debt subtracted), net_pe when it is the net margin"`
	TerminalMargin money.Decimal  `json:"terminal_margin"`
	ExitMultiple   money.Decimal  `json:"exit_multiple"`
	DiscountRate   money.Decimal  `json:"discount_rate"`
	Note           string         `json:"note"`
}

// Helpers groups the valuation helpers.
type Helpers struct {
	Bazin      *HelperValue `json:"bazin"`
	Graham     *HelperValue `json:"graham"`
	ReverseDCF *ReverseDCF  `json:"reverse_dcf"`
}

// FundamentalsPanel is the fundamentals panel of one asset.
type FundamentalsPanel struct {
	AssetID  int64  `json:"asset_id"`
	Ticker   string `json:"ticker"`
	Market   string `json:"market" enum:"B3,US"`
	Currency string `json:"currency" enum:"BRL,USD"`
	Status   string `json:"status" enum:"ok,needs_identifier,not_fetched,unsupported"`
	// IdentifierKind is "cnpj" for B3 and "cik" for US; Identifier is its value.
	IdentifierKind     string             `json:"identifier_kind" enum:"cnpj,cik"`
	Identifier         string             `json:"identifier"`
	Source             string             `json:"source" doc:"cvm or sec; empty before the first fetch"`
	SourceName         string             `json:"source_name"`
	Entity             string             `json:"entity,omitempty"`
	AsOf               string             `json:"as_of" doc:"Latest period end stored"`
	FetchedAt          string             `json:"fetched_at"`
	LastError          string             `json:"last_error,omitempty" doc:"Outcome of the last failed refresh, if it failed"`
	Message            string             `json:"message"`
	FundamentalMetrics FundamentalMetrics `json:"metrics"`
	History            []HistoryPoint     `json:"history"`
	Ranges             Ranges             `json:"ranges"`
	Helpers            Helpers            `json:"helpers"`
	Notes              []string           `json:"notes"`
}

// FairValueDraft is a fair value version proposed from a helper; the client creates it
// through POST /api/assets/{id}/fair-values.
type FairValueDraft struct {
	Method        string         `json:"method" enum:"bazin,graham"`
	ValuePerShare money.Decimal  `json:"value_per_share"`
	Currency      string         `json:"currency"`
	AsOf          string         `json:"as_of"`
	Inputs        map[string]any `json:"inputs"`
	Sources       []string       `json:"sources"`
	Note          string         `json:"note"`
}
