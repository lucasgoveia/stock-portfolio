// Package analyst runs the AI analyst (spec §13): on demand, one asset at a
// time, it gathers the thesis, fair value, fundamentals and recent filings,
// asks a language model for a structured draft, and stores it as a draft.
// Nothing is applied until the user approves parts of it, and a monthly
// spend cap is checked before every run.
package analyst

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"

	_ "time/tzdata" // America/Sao_Paulo without system tzdata
)

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("analyst run not found")
	ErrValidation = errors.New("analyst validation error")
	// ErrNoKey means no Anthropic API key is configured.
	ErrNoKey = errors.New("the Anthropic API key is not configured (set STOCKPORTFOLIO_ANTHROPIC_API_KEY)")
	// ErrCapReached means this month's spend reached the cap.
	ErrCapReached = errors.New("monthly AI cap reached")
	// ErrConflict means the run is not in a state that allows the action.
	ErrConflict = errors.New("analyst run conflict")
	// ErrUpstream means the model call or its output failed; the run is stored as failed.
	ErrUpstream = errors.New("analyst model error")
)

func validationf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, a...))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02T15:04:05Z"
	// maxOutputTokens bounds one answer (including thinking). Kept under the
	// size the SDK allows for a non-streaming request.
	maxOutputTokens = 20000
)

// Fundamentals is the fundamentals service (fundamentals.Service).
type Fundamentals interface {
	Get(ctx context.Context, assetID int64, p fundamentals.Params) (fundamentals.FundamentalsPanel, error)
}

// USFilings lists and downloads SEC filings (marketdata.EDGAR).
type USFilings interface {
	RecentFilings(ctx context.Context, cik string, forms []string, perForm int) ([]marketdata.Filing, error)
	Exhibit99(ctx context.Context, f marketdata.Filing) (string, error)
	Document(ctx context.Context, url string, maxChars int) (string, error)
}

// BRFilings lists CVM IPE announcements (marketdata.CVM).
type BRFilings interface {
	Announcements(ctx context.Context, cnpj string, year int, categories []string, limit int) ([]marketdata.Announcement, error)
}

// Deps are the collaborators of Service.
type Deps struct {
	Conn         db.DBTX
	Assets       *assets.Service
	Settings     *settings.Service
	Thesis       *thesis.Service
	Valuation    *valuation.Service
	Research     *research.Service
	Fundamentals Fundamentals
	US           USFilings
	BR           BRFilings
	// Client is the model; nil means no API key is configured.
	Client Client
	Clock  func() time.Time
}

// Service runs and stores analyst runs.
type Service struct {
	q *db.Queries
	d Deps
	// mu serializes runs so two concurrent requests cannot both pass the cap.
	mu  sync.Mutex
	now func() time.Time
	loc *time.Location
}

// NewService builds a Service.
func NewService(d Deps) *Service {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.FixedZone("BRT", -3*3600)
	}
	return &Service{q: db.New(d.Conn), d: d, now: clock, loc: loc}
}

func (s *Service) ts() string { return s.now().UTC().Format(tsLayout) }

// monthBounds returns the UTC timestamps bounding the current calendar month
// in America/Sao_Paulo.
func (s *Service) monthBounds() (label, from, to string) {
	n := s.now().In(s.loc)
	start := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, s.loc)
	return start.Format("2006-01"), start.UTC().Format(tsLayout), start.AddDate(0, 1, 0).UTC().Format(tsLayout)
}

// monthCosts returns the sum and count of run costs this month
// (America/Sao_Paulo), in USD.
func (s *Service) monthCosts(ctx context.Context) (decimal.Decimal, int, error) {
	_, from, to := s.monthBounds()
	rows, err := s.q.ListAnalystCostsBetween(ctx, db.ListAnalystCostsBetweenParams{CreatedAt: from, CreatedAt_2: to})
	if err != nil {
		return decimal.Zero, 0, fmt.Errorf("month spend: %w", err)
	}
	sum := decimal.Zero
	for _, r := range rows {
		v, err := decimal.NewFromString(r)
		if err != nil {
			return decimal.Zero, 0, fmt.Errorf("month spend: bad cost %q: %w", r, err)
		}
		sum = sum.Add(v)
	}
	return sum, len(rows), nil
}

// MonthSpend is the AI spend this month (America/Sao_Paulo), in USD.
func (s *Service) MonthSpend(ctx context.Context) (decimal.Decimal, error) {
	sum, _, err := s.monthCosts(ctx)
	return sum, err
}

// Usage reports this month's spend against the cap and whether a run may start.
func (s *Service) Usage(ctx context.Context) (Usage, error) {
	ai, err := s.d.Settings.AI(ctx)
	if err != nil {
		return Usage{}, err
	}
	spent, n, err := s.monthCosts(ctx)
	if err != nil {
		return Usage{}, err
	}
	label, _, _ := s.monthBounds()
	capUSD := ai.MonthlyCapUSD.Decimal
	remaining := capUSD.Sub(spent)
	if remaining.IsNegative() {
		remaining = decimal.Zero
	}
	u := Usage{
		Month: label, SpentUSD: money.New(spent), CapUSD: money.New(capUSD), RemainingUSD: money.New(remaining),
		Runs: n, Model: ai.Model, KeyConfigured: s.d.Client != nil, CanRun: true,
	}
	if err := s.check(ai, spent); err != nil {
		u.CanRun, u.BlockedReason = false, err.Error()
	}
	return u, nil
}

// check refuses a run when the key, the model price or the cap blocks it.
func (s *Service) check(ai settings.AI, spent decimal.Decimal) error {
	if s.d.Client == nil {
		return ErrNoKey
	}
	if _, ok := prices[ai.Model]; !ok {
		return validationf("no price is configured for model %q, so the cap cannot be applied (known models: %s)",
			ai.Model, strings.Join(KnownModels(), ", "))
	}
	if spent.GreaterThanOrEqual(ai.MonthlyCapUSD.Decimal) {
		return fmt.Errorf("%w: $%s of $%s", ErrCapReached, spent.StringFixed(2), ai.MonthlyCapUSD.StringFixed(2))
	}
	return nil
}

func (s *Service) convert(r db.AnalystRun) (Run, error) {
	cost, err := money.NewFromString(r.CostUsd)
	if err != nil {
		return Run{}, fmt.Errorf("analyst run %d: %w", r.ID, err)
	}
	run := Run{
		ID: r.ID, AssetID: r.AssetID, Model: r.Model, Status: r.Status, InputTokens: r.InputTokens,
		OutputTokens: r.OutputTokens, CostUSD: cost, Error: r.Error, CreatedAt: r.CreatedAt,
	}
	_ = json.Unmarshal([]byte(r.Inputs), &run.Inputs)
	if run.Inputs.Documents == nil {
		run.Inputs.Documents = []Document{}
	}
	if run.Inputs.Skipped == nil {
		run.Inputs.Skipped = []string{}
	}
	if r.Status != StatusFailed {
		var o Output
		if json.Unmarshal([]byte(r.Output), &o) == nil && len(o.Scenarios.Base.RevenueGrowth) > 0 {
			run.Output = &o
		}
	}
	_ = json.Unmarshal([]byte(r.Approvals), &run.Approvals)
	if run.Approvals.Catalysts == nil {
		run.Approvals.Catalysts = []int{}
	}
	if run.Approvals.NoteIDs == nil {
		run.Approvals.NoteIDs = []int64{}
	}
	return run, nil
}

// Get returns one run.
func (s *Service) Get(ctx context.Context, id int64) (Run, error) {
	r, err := s.q.GetAnalystRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get analyst run: %w", err)
	}
	return s.convert(r)
}

// List returns an asset's runs, newest first.
func (s *Service) List(ctx context.Context, assetID int64) ([]Run, error) {
	if _, err := s.d.Assets.Get(ctx, assetID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListAnalystRunsByAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list analyst runs: %w", err)
	}
	out := make([]Run, 0, len(rows))
	for _, r := range rows {
		run, err := s.convert(r)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

// Run analyses an asset. The cap and key are checked first; the model call
// runs detached from the request so a closed browser tab does not lose a run
// that is already being paid for.
func (s *Service) Run(ctx context.Context, assetID int64) (Run, error) {
	asset, err := s.d.Assets.Get(ctx, assetID)
	if err != nil {
		return Run{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ai, err := s.d.Settings.AI(ctx)
	if err != nil {
		return Run{}, err
	}
	spent, err := s.MonthSpend(ctx)
	if err != nil {
		return Run{}, err
	}
	if err := s.check(ai, spent); err != nil {
		return Run{}, err
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout+3*time.Minute)
	defer cancel()
	in, err := s.gather(ctx, asset)
	if err != nil {
		return Run{}, err
	}
	resp, callErr := s.d.Client.Complete(ctx, Request{
		Model: ai.Model, System: systemPrompt, User: userPrompt(in),
		Schema: outputSchema(), MaxTokens: maxOutputTokens,
	})
	cost, priceErr := Cost(ai.Model, resp.InputTokens, resp.OutputTokens)
	if priceErr != nil {
		cost = decimal.Zero
	}
	if callErr != nil {
		return s.fail(ctx, asset.ID, ai.Model, in, resp, cost, callErr)
	}
	allowed := map[string]bool{}
	for _, d := range in.Documents {
		allowed[d.URL] = true
	}
	out, dropped, err := parseOutput(resp.Text, allowed)
	if err != nil {
		return s.fail(ctx, asset.ID, ai.Model, in, resp, cost, err)
	}
	in.DroppedSources = dropped
	inJSON, err := json.Marshal(in)
	if err != nil {
		return Run{}, err
	}
	outJSON, err := json.Marshal(out)
	if err != nil {
		return Run{}, err
	}
	row, err := s.q.CreateAnalystRun(ctx, db.CreateAnalystRunParams{
		AssetID: asset.ID, Model: ai.Model, Status: StatusDraft, Inputs: string(inJSON), Output: string(outJSON),
		InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, CostUsd: cost.String(), CreatedAt: s.ts(),
	})
	if err != nil {
		return Run{}, fmt.Errorf("store analyst run: %w", err)
	}
	return s.convert(row)
}

// fail stores a failed run (its tokens still count against the cap) and
// returns the error.
func (s *Service) fail(ctx context.Context, assetID int64, model string, in Inputs, resp Response, cost decimal.Decimal, cause error) (Run, error) {
	inJSON, _ := json.Marshal(in)
	if _, err := s.q.CreateAnalystRun(ctx, db.CreateAnalystRunParams{
		AssetID: assetID, Model: model, Status: StatusFailed, Inputs: string(inJSON), Output: "{}",
		InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, CostUsd: cost.String(),
		Error: cause.Error(), CreatedAt: s.ts(),
	}); err != nil {
		return Run{}, fmt.Errorf("store failed analyst run: %w (original: %v)", err, cause)
	}
	return Run{}, fmt.Errorf("%w: %v", ErrUpstream, cause)
}

// Discard marks a draft as discarded.
func (s *Service) Discard(ctx context.Context, id int64) (Run, error) {
	run, err := s.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != StatusDraft {
		return Run{}, fmt.Errorf("%w: only a draft can be discarded (this run is %s)", ErrConflict, run.Status)
	}
	return s.setStatus(ctx, id, StatusDiscarded, run.Approvals)
}

func (s *Service) setStatus(ctx context.Context, id int64, status string, a Approvals) (Run, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return Run{}, err
	}
	row, err := s.q.UpdateAnalystRunStatus(ctx, db.UpdateAnalystRunStatusParams{Status: status, Approvals: string(b), ID: id})
	if err != nil {
		return Run{}, fmt.Errorf("update analyst run: %w", err)
	}
	return s.convert(row)
}
