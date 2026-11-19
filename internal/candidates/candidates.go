// Package candidates holds new-ticker proposals submitted by the user's Claude
// client over MCP (spec §13.6). A candidate is only a proposal: nothing exists
// for it until the user accepts it, and accepting creates a watchlist asset
// with the proposal's note, scenarios and fair value as drafts to approve.
package candidates

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Statuses.
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
)

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("candidate not found")
	ErrValidation = errors.New("candidate validation error")
	ErrConflict   = errors.New("candidate conflict")
)

func validationf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, a...))
}

// Candidate is one proposal.
type Candidate struct {
	ID     int64  `json:"id"`
	Ticker string `json:"ticker"`
	Market string `json:"market" enum:"B3,US"`
	Name   string `json:"name"`
	CNPJ   string `json:"cnpj"`
	CIK    string `json:"cik"`
	// Note is the research note written by the model (markdown).
	Note string `json:"note"`
	// Output is the proposed draft (scenarios, fair value range, initiatives,
	// catalysts, sources); empty fields when the proposal has none.
	Output     analyst.Output     `json:"output"`
	Provenance analyst.Provenance `json:"provenance"`
	Status     string             `json:"status" enum:"pending,accepted,rejected"`
	// AssetID and RunID are set when accepted: the watchlist asset and the
	// analyst draft created for it.
	AssetID   *int64 `json:"asset_id,omitempty"`
	RunID     *int64 `json:"run_id,omitempty"`
	CreatedAt string `json:"created_at"`
	DecidedAt string `json:"decided_at,omitempty"`
}

// SubmitInput is a new proposal. Output and Provenance come from the MCP
// session (sources already marked external where the app did not serve them).
type SubmitInput struct {
	Ticker     string
	Market     string
	Name       string
	CNPJ       string
	CIK        string
	Note       string
	Output     analyst.Output
	Provenance analyst.Provenance
}

// Service stores and decides candidates.
type Service struct {
	q       *db.Queries
	assets  *assets.Service
	analyst *analyst.Service
	now     func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, an *analyst.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, analyst: an, now: clock}
}

func (s *Service) ts() string { return s.now().UTC().Format("2006-01-02T15:04:05Z") }

func convert(r db.Candidate) Candidate {
	c := Candidate{
		ID: r.ID, Ticker: r.Ticker, Market: r.Market, Name: r.Name, CNPJ: r.Cnpj, CIK: r.Cik, Note: r.Note,
		Status: r.Status, CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
	}
	_ = json.Unmarshal([]byte(r.Output), &c.Output)
	_ = json.Unmarshal([]byte(r.Provenance), &c.Provenance)
	if r.AssetID.Valid {
		c.AssetID = &r.AssetID.Int64
	}
	if r.RunID.Valid {
		c.RunID = &r.RunID.Int64
	}
	if c.Output.Catalysts == nil {
		c.Output.Catalysts = []analyst.Catalyst{}
	}
	if c.Output.ThesisCheck == nil {
		c.Output.ThesisCheck = []analyst.ThesisCheck{}
	}
	if c.Output.Sources == nil {
		c.Output.Sources = []analyst.Source{}
	}
	if c.Provenance.Documents == nil {
		c.Provenance.Documents = []analyst.Document{}
	}
	if c.Provenance.MCP.ExternalURLs == nil {
		c.Provenance.MCP.ExternalURLs = []string{}
	}
	return c
}

// Submit stores a pending proposal. The ticker must not already be an asset
// or a pending candidate, B3 proposals need a CNPJ with valid check digits and
// whatever draft is given is validated like an analyst answer.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (Candidate, error) {
	in.Ticker = strings.ToUpper(strings.TrimSpace(in.Ticker))
	in.Market = strings.ToUpper(strings.TrimSpace(in.Market))
	in.Name = strings.TrimSpace(in.Name)
	if in.Ticker == "" {
		return Candidate{}, validationf("ticker is required")
	}
	switch in.Market {
	case assets.MarketB3:
		if !marketdata.ValidCNPJ(in.CNPJ) {
			return Candidate{}, validationf("a B3 candidate needs a CNPJ with valid check digits")
		}
		in.CNPJ, in.CIK = marketdata.FormatCNPJ(in.CNPJ), ""
	case assets.MarketUS:
		if strings.TrimSpace(in.CIK) != "" {
			if in.CIK = marketdata.FormatCIK(in.CIK); in.CIK == "" {
				return Candidate{}, validationf("cik must be 1 to 10 digits")
			}
		}
		in.CNPJ = ""
	default:
		return Candidate{}, validationf("market must be B3 or US")
	}
	if strings.TrimSpace(in.Note) == "" && !hasDraft(in.Output) {
		return Candidate{}, validationf("a candidate needs a research note or a draft (scenarios or a fair value range)")
	}
	if err := in.Output.Validate(true); err != nil {
		return Candidate{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	existing, err := s.assets.List(ctx, assets.Filter{Market: in.Market})
	if err != nil {
		return Candidate{}, err
	}
	for _, a := range existing {
		if a.Ticker == in.Ticker {
			return Candidate{}, fmt.Errorf("%w: %s (%s) is already in the app as asset %d: use submit_analysis instead", ErrConflict, in.Ticker, in.Market, a.ID)
		}
	}
	rows, err := s.q.ListCandidates(ctx)
	if err != nil {
		return Candidate{}, fmt.Errorf("list candidates: %w", err)
	}
	for _, r := range rows {
		if r.Status == StatusPending && r.Ticker == in.Ticker && r.Market == in.Market {
			return Candidate{}, fmt.Errorf("%w: a pending candidate for %s (%s) already exists (#%d); the user must accept or reject it first", ErrConflict, in.Ticker, in.Market, r.ID)
		}
	}
	if in.Provenance.MCP.SubmittedAt == "" {
		in.Provenance.MCP.SubmittedAt = s.ts()
	}
	out, err := json.Marshal(in.Output)
	if err != nil {
		return Candidate{}, err
	}
	prov, err := json.Marshal(in.Provenance)
	if err != nil {
		return Candidate{}, err
	}
	row, err := s.q.CreateCandidate(ctx, db.CreateCandidateParams{
		Ticker: in.Ticker, Market: in.Market, Name: in.Name, Cnpj: in.CNPJ, Cik: in.CIK, Note: strings.TrimSpace(in.Note),
		Output: string(out), Provenance: string(prov), CreatedAt: s.ts(),
	})
	if err != nil {
		return Candidate{}, fmt.Errorf("store candidate: %w", err)
	}
	return convert(row), nil
}

func hasDraft(o analyst.Output) bool {
	return len(o.Scenarios.Base.RevenueGrowth) > 0 || !o.FairValueRange.High.IsZero() || len(o.Catalysts) > 0
}

// Get returns one candidate.
func (s *Service) Get(ctx context.Context, id int64) (Candidate, error) {
	r, err := s.q.GetCandidate(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("get candidate: %w", err)
	}
	return convert(r), nil
}

// List returns every candidate, pending first, newest first.
func (s *Service) List(ctx context.Context) ([]Candidate, error) {
	rows, err := s.q.ListCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	out := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}
	return out, nil
}

// CountPending is the number of proposals awaiting a decision.
func (s *Service) CountPending(ctx context.Context) (int, error) {
	n, err := s.q.CountPendingCandidates(ctx)
	if err != nil {
		return 0, fmt.Errorf("count candidates: %w", err)
	}
	return int(n), nil
}

// Accept creates the watchlist asset (with its CNPJ or CIK) and, from the
// proposal's note and draft, an MCP analyst draft on it that the user approves
// part by part in the asset's AI analyst tab. Nothing is applied here.
func (s *Service) Accept(ctx context.Context, id int64) (Candidate, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.Status != StatusPending {
		return Candidate{}, fmt.Errorf("%w: this candidate is already %s", ErrConflict, c.Status)
	}
	out := c.Output
	if strings.TrimSpace(c.Note) != "" {
		out.Summary = c.Note
	}
	if err := out.Validate(true); err != nil {
		return Candidate{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	name := c.Name
	asset, err := s.assets.Create(ctx, assets.CreateInput{
		Ticker: c.Ticker, Name: name, Market: c.Market, Kind: assets.KindStock, CNPJ: c.CNPJ, CIK: c.CIK,
	})
	if errors.Is(err, assets.ErrDuplicate) {
		return Candidate{}, fmt.Errorf("%w: %s is already in the app; reject this candidate", ErrConflict, c.Ticker)
	}
	if err != nil {
		return Candidate{}, err
	}
	run, err := s.analyst.Submit(ctx, asset.ID, out, c.Provenance, true)
	if err != nil {
		return Candidate{}, fmt.Errorf("asset %d was created but its draft could not be: %w", asset.ID, err)
	}
	row, err := s.q.DecideCandidate(ctx, db.DecideCandidateParams{
		Status: StatusAccepted, AssetID: sql.NullInt64{Int64: asset.ID, Valid: true},
		RunID: sql.NullInt64{Int64: run.ID, Valid: true}, DecidedAt: s.ts(), ID: id,
	})
	if err != nil {
		return Candidate{}, fmt.Errorf("decide candidate: %w", err)
	}
	return convert(row), nil
}

// Reject marks the proposal rejected; the row is kept.
func (s *Service) Reject(ctx context.Context, id int64) (Candidate, error) {
	c, err := s.Get(ctx, id)
	if err != nil {
		return Candidate{}, err
	}
	if c.Status != StatusPending {
		return Candidate{}, fmt.Errorf("%w: this candidate is already %s", ErrConflict, c.Status)
	}
	row, err := s.q.DecideCandidate(ctx, db.DecideCandidateParams{Status: StatusRejected, DecidedAt: s.ts(), ID: id})
	if err != nil {
		return Candidate{}, fmt.Errorf("decide candidate: %w", err)
	}
	return convert(row), nil
}
