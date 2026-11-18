// Package research stores research notes and the opportunity catalyst flag.
// See docs/spec.md sections 12.3 and 8.2.
package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Authors.
const (
	AuthorUser       = "user"
	AuthorAIApproved = "ai_approved"
)

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("research note not found")
	ErrValidation = errors.New("research validation error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02T15:04:05Z"
)

// Note is a research note. Opportunity is the raw flag; Active says whether it
// still counts at the time the note was read (flag set and not expired).
type Note struct {
	ID      int64    `json:"id"`
	AssetID int64    `json:"asset_id"`
	Body    string   `json:"text"`
	Sources []string `json:"sources"`
	Author  string   `json:"author"`
	// Opportunity is the catalyst flag; ExpiresOn is its last valid day (inclusive).
	Opportunity bool    `json:"opportunity"`
	ExpiresOn   *string `json:"opportunity_expires_on"`
	// OpportunityActive is true when the flag is set and ExpiresOn is not before today.
	OpportunityActive bool   `json:"opportunity_active"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// CreateInput is a new note. Author defaults to user. When Opportunity is true,
// OpportunityExpiresOn is required.
type CreateInput struct {
	Body                 string
	Sources              []string
	Author               string
	Opportunity          bool
	OpportunityExpiresOn string
}

// UpdateInput holds optional changes. Setting Opportunity false clears the
// expiry; setting it true needs an expiry (new or existing).
type UpdateInput struct {
	Body                 *string
	Sources              *[]string
	Opportunity          *bool
	OpportunityExpiresOn *string
}

// Service manages research notes.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, now: clock}
}

func (s *Service) today() string { return s.now().Format(dateLayout) }

func (s *Service) convert(r db.ResearchNote) Note {
	n := Note{
		ID: r.ID, AssetID: r.AssetID, Body: r.Body, Author: r.Author, Opportunity: r.Opportunity == 1,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Sources: []string{},
	}
	_ = json.Unmarshal([]byte(r.Sources), &n.Sources)
	if r.OpportunityExpiresOn.Valid {
		v := r.OpportunityExpiresOn.String
		n.ExpiresOn = &v
	}
	n.OpportunityActive = n.Opportunity && n.ExpiresOn != nil && *n.ExpiresOn >= s.today()
	return n
}

func cleanSources(in []string) ([]string, string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	b, err := json.Marshal(out)
	return out, string(b), err
}

func checkDate(v string) error {
	if _, err := time.Parse(dateLayout, v); err != nil {
		return validationf("opportunity_expires_on must be YYYY-MM-DD")
	}
	return nil
}

// Create adds a note to an asset.
func (s *Service) Create(ctx context.Context, assetID int64, in CreateInput) (Note, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return Note{}, err
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return Note{}, validationf("text is required")
	}
	author := strings.ToLower(strings.TrimSpace(in.Author))
	if author == "" {
		author = AuthorUser
	}
	if author != AuthorUser && author != AuthorAIApproved {
		return Note{}, validationf("author must be user or ai_approved")
	}
	_, srcJSON, err := cleanSources(in.Sources)
	if err != nil {
		return Note{}, validationf("sources: %v", err)
	}
	var exp sql.NullString
	opp := int64(0)
	if in.Opportunity {
		if err := checkDate(in.OpportunityExpiresOn); err != nil {
			return Note{}, validationf("opportunity notes need an expiry date (opportunity_expires_on, YYYY-MM-DD)")
		}
		opp = 1
		exp = sql.NullString{String: in.OpportunityExpiresOn, Valid: true}
	} else if in.OpportunityExpiresOn != "" {
		return Note{}, validationf("opportunity_expires_on requires opportunity=true")
	}
	now := s.now().UTC().Format(tsLayout)
	row, err := s.q.CreateResearchNote(ctx, db.CreateResearchNoteParams{
		AssetID: assetID, Body: body, Sources: srcJSON, Author: author, Opportunity: opp,
		OpportunityExpiresOn: exp, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Note{}, fmt.Errorf("create note: %w", err)
	}
	return s.convert(row), nil
}

// Get returns one note.
func (s *Service) Get(ctx context.Context, id int64) (Note, error) {
	row, err := s.q.GetResearchNote(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("get note: %w", err)
	}
	return s.convert(row), nil
}

// List returns an asset's notes, newest first.
func (s *Service) List(ctx context.Context, assetID int64) ([]Note, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListResearchNotesByAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	out := make([]Note, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.convert(r))
	}
	return out, nil
}

// Update applies the non-nil fields.
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (Note, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return Note{}, err
	}
	body, sources, opp, exp := cur.Body, cur.Sources, cur.Opportunity, cur.ExpiresOn
	if in.Body != nil {
		body = strings.TrimSpace(*in.Body)
		if body == "" {
			return Note{}, validationf("text is required")
		}
	}
	if in.Sources != nil {
		sources = *in.Sources
	}
	if in.Opportunity != nil {
		opp = *in.Opportunity
		if !opp {
			exp = nil
		}
	}
	if in.OpportunityExpiresOn != nil {
		if !opp {
			return Note{}, validationf("opportunity_expires_on requires opportunity=true")
		}
		v := *in.OpportunityExpiresOn
		exp = &v
	}
	oppInt := int64(0)
	var expSQL sql.NullString
	if opp {
		if exp == nil {
			return Note{}, validationf("opportunity notes need an expiry date (opportunity_expires_on, YYYY-MM-DD)")
		}
		if err := checkDate(*exp); err != nil {
			return Note{}, err
		}
		oppInt = 1
		expSQL = sql.NullString{String: *exp, Valid: true}
	}
	_, srcJSON, err := cleanSources(sources)
	if err != nil {
		return Note{}, validationf("sources: %v", err)
	}
	row, err := s.q.UpdateResearchNote(ctx, db.UpdateResearchNoteParams{
		Body: body, Sources: srcJSON, Opportunity: oppInt, OpportunityExpiresOn: expSQL,
		UpdatedAt: s.now().UTC().Format(tsLayout), ID: id,
	})
	if err != nil {
		return Note{}, fmt.Errorf("update note: %w", err)
	}
	return s.convert(row), nil
}

// Delete removes a note.
func (s *Service) Delete(ctx context.Context, id int64) error {
	n, err := s.q.DeleteResearchNote(ctx, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveOpportunities returns notes flagged opportunity whose expiry is not
// before asof (default today); expired flags no longer count. assetID, when
// non-nil, restricts to one asset.
func (s *Service) ActiveOpportunities(ctx context.Context, asof string, assetID *int64) ([]Note, error) {
	if asof == "" {
		asof = s.today()
	}
	if _, err := time.Parse(dateLayout, asof); err != nil {
		return nil, validationf("as_of must be YYYY-MM-DD")
	}
	var asset any
	if assetID != nil {
		asset = *assetID
	}
	rows, err := s.q.ListActiveOpportunities(ctx, db.ListActiveOpportunitiesParams{Asof: sql.NullString{String: asof, Valid: true}, AssetID: asset})
	if err != nil {
		return nil, fmt.Errorf("list opportunities: %w", err)
	}
	out := make([]Note, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.convert(r))
	}
	return out, nil
}

// HasActiveOpportunity reports whether the asset has a non-expired opportunity
// note at asof (default today). Used by the buy-gate/opportunity logic (spec 8.2).
func (s *Service) HasActiveOpportunity(ctx context.Context, assetID int64, asof string) (bool, error) {
	list, err := s.ActiveOpportunities(ctx, asof, &assetID)
	return len(list) > 0, err
}
