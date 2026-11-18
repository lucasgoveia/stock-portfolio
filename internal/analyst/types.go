package analyst

import (
	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// Run statuses. A run is a draft until the user approves parts of it.
const (
	StatusDraft     = "draft"
	StatusApproved  = "approved"
	StatusDiscarded = "discarded"
	// StatusFailed marks a run that spent tokens (or tried to) without
	// producing a usable draft; Error says why.
	StatusFailed = "failed"
)

// Thesis check statuses.
const (
	CheckHolds   = "holds"
	CheckAtRisk  = "at_risk"
	CheckBroken  = "broken"
	maxCatalysts = 12
)

// Document is a source document the model was given. Its text is sent to the
// model but not stored; only the reference is kept with the run.
type Document struct {
	Kind  string `json:"kind" doc:"10-K, 10-Q, 8-K, 8-K exhibit 99.1, fato relevante, comunicado ao mercado"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Date  string `json:"date" doc:"YYYY-MM-DD"`
	// Chars is how much text of the document was sent.
	Chars int `json:"chars"`
	// Text is the extracted text sent to the model (never stored or served).
	Text string `json:"-"`
}

// ThesisInput is the thesis as given to the model.
type ThesisInput struct {
	Text            string   `json:"text"`
	Status          string   `json:"status"`
	BreakConditions []string `json:"break_conditions"`
}

// FairValueInput is the current fair value as given to the model.
type FairValueInput struct {
	Method        string        `json:"method"`
	ValuePerShare money.Decimal `json:"value_per_share"`
	Currency      string        `json:"currency"`
	AsOf          string        `json:"as_of"`
	Scenarios     any           `json:"scenarios,omitempty"`
}

// DCFBase is the base figures a scenario DCF needs besides the scenarios.
type DCFBase struct {
	BaseRevenue decimal.Decimal `json:"base_revenue"`
	NetDebt     decimal.Decimal `json:"net_debt"`
	Shares      decimal.Decimal `json:"shares"`
}

// Inputs records what a run was given.
type Inputs struct {
	Ticker       string          `json:"ticker"`
	Market       string          `json:"market"`
	Currency     string          `json:"currency"`
	Thesis       *ThesisInput    `json:"thesis,omitempty"`
	FairValue    *FairValueInput `json:"fair_value,omitempty"`
	Fundamentals string          `json:"fundamentals" doc:"Status of the fundamentals summary sent to the model"`
	DCFBase      *DCFBase        `json:"dcf_base,omitempty" doc:"Base revenue, net debt and shares used when scenarios are approved into a DCF version"`
	Documents    []Document      `json:"documents"`
	// Skipped lists documents that could not be fetched, with the reason.
	Skipped []string `json:"skipped"`
	// DroppedSources counts sources in the output that were not among the
	// provided documents and were removed.
	DroppedSources int `json:"dropped_sources"`

	// fundamentalsJSON is the fundamentals summary sent to the model (not stored).
	fundamentalsJSON string
}

// Catalyst is an event that could move the price.
type Catalyst struct {
	Title        string   `json:"title"`
	Detail       string   `json:"detail"`
	ExpectedDate string   `json:"expected_date,omitempty" doc:"YYYY-MM-DD when known"`
	SourceURLs   []string `json:"source_urls"`
}

// ScenarioDraft is one proposed DCF scenario, the same fields as the scenario
// editor. Rates are fractions (0.10 = 10%).
type ScenarioDraft struct {
	RevenueGrowth  []decimal.Decimal `json:"revenue_growth" doc:"Yearly revenue growth for the 5 forecast years"`
	TerminalMargin decimal.Decimal   `json:"terminal_margin"`
	ExitMultiple   decimal.Decimal   `json:"exit_multiple"`
	DiscountRate   decimal.Decimal   `json:"discount_rate"`
	Probability    decimal.Decimal   `json:"probability"`
	Rationale      string            `json:"rationale"`
}

// Scenarios are the three proposed scenarios.
type Scenarios struct {
	Bear ScenarioDraft `json:"bear"`
	Base ScenarioDraft `json:"base"`
	Bull ScenarioDraft `json:"bull"`
}

// FairValueRange is the proposed per-share range.
type FairValueRange struct {
	Low      decimal.Decimal `json:"low"`
	High     decimal.Decimal `json:"high"`
	Currency string          `json:"currency"`
}

// ThesisCheck is the verdict on one break condition.
type ThesisCheck struct {
	Condition string `json:"condition"`
	Status    string `json:"status" enum:"holds,at_risk,broken"`
	Evidence  string `json:"evidence"`
}

// Source is a document the output cites.
type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Output is the validated structured answer of the model.
type Output struct {
	Catalysts       []Catalyst     `json:"catalysts"`
	MarketSizeNotes string         `json:"market_size_notes"`
	Scenarios       Scenarios      `json:"scenarios"`
	FairValueRange  FairValueRange `json:"fair_value_range"`
	ThesisCheck     []ThesisCheck  `json:"thesis_check"`
	Sources         []Source       `json:"sources"`
}

// Approvals records what has been applied from a run.
type Approvals struct {
	Catalysts      []int   `json:"catalysts" doc:"Indexes of catalysts turned into notes"`
	MarketSize     bool    `json:"market_size"`
	ThesisCheck    bool    `json:"thesis_check"`
	NoteIDs        []int64 `json:"note_ids"`
	DCFFairValueID *int64  `json:"dcf_fair_value_id,omitempty"`
	RangeFairValue *int64  `json:"range_fair_value_id,omitempty"`
}

// Run is one AI analyst run.
type Run struct {
	ID           int64         `json:"id"`
	AssetID      int64         `json:"asset_id"`
	Model        string        `json:"model"`
	Status       string        `json:"status" enum:"draft,approved,discarded,failed"`
	Inputs       Inputs        `json:"inputs"`
	Output       *Output       `json:"output,omitempty"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	CostUSD      money.Decimal `json:"cost_usd"`
	Approvals    Approvals     `json:"approvals"`
	Error        string        `json:"error,omitempty"`
	CreatedAt    string        `json:"created_at"`
}

// Usage is the month-to-date spend against the cap and whether a run may start.
type Usage struct {
	Month         string        `json:"month" doc:"YYYY-MM, America/Sao_Paulo"`
	SpentUSD      money.Decimal `json:"spent_usd"`
	CapUSD        money.Decimal `json:"cap_usd"`
	RemainingUSD  money.Decimal `json:"remaining_usd"`
	Runs          int           `json:"runs"`
	Model         string        `json:"model"`
	KeyConfigured bool          `json:"key_configured"`
	CanRun        bool          `json:"can_run"`
	// BlockedReason says why CanRun is false.
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// ApproveInput selects the parts of a run to apply.
type ApproveInput struct {
	Catalysts      []int `json:"catalysts,omitempty" doc:"Indexes of catalysts to save as notes"`
	MarketSize     bool  `json:"market_size,omitempty" doc:"Save the market size notes as a note"`
	ThesisCheck    bool  `json:"thesis_check,omitempty" doc:"Save the thesis check as a note"`
	Scenarios      bool  `json:"scenarios,omitempty" doc:"Create a scenario DCF fair value version"`
	FairValueRange bool  `json:"fair_value_range,omitempty" doc:"Create a manual fair value version at the range midpoint"`
}
