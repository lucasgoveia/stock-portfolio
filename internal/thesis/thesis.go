// Package thesis holds per-asset investment theses, their break conditions and
// reviews, and emits thesis_broken / thesis_review_due alerts.
// See docs/spec.md sections 4.2 and 9.2.
package thesis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Thesis statuses.
const (
	StatusActive      = "active"
	StatusUnderReview = "under_review"
	StatusBroken      = "broken"
)

// Alert type names owned by this package.
const (
	AlertBroken    = "thesis_broken"
	AlertReviewDue = "thesis_review_due"
)

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("thesis not found")
	ErrValidation = errors.New("thesis validation error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02T15:04:05Z"
)

func validStatus(s string) bool {
	return s == StatusActive || s == StatusUnderReview || s == StatusBroken
}

// BreakCondition is one condition that, if met, breaks the thesis.
type BreakCondition struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Thesis is an asset's thesis with its break conditions.
type Thesis struct {
	ID              int64            `json:"id"`
	AssetID         int64            `json:"asset_id"`
	Text            string           `json:"text"`
	Status          string           `json:"status"`
	NextReviewOn    *string          `json:"next_review_on"`
	BreakConditions []BreakCondition `json:"break_conditions"`
	CreatedAt       string           `json:"created_at"`
	UpdatedAt       string           `json:"updated_at"`
}

// Review is a recorded thesis review.
type Review struct {
	ID           int64   `json:"id"`
	ThesisID     int64   `json:"thesis_id"`
	ReviewedOn   string  `json:"reviewed_on"`
	Note         string  `json:"note"`
	StatusAfter  string  `json:"status_after"`
	NextReviewOn *string `json:"next_review_on"`
	CreatedAt    string  `json:"created_at"`
}

// Service manages theses and emits alerts.
type Service struct {
	conn   *sql.DB
	q      *db.Queries
	assets *assets.Service
	alerts *alerts.Service
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now. The clock should be
// the same one given to the alerts service.
func NewService(conn *sql.DB, a *assets.Service, al *alerts.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{conn: conn, q: db.New(conn), assets: a, alerts: al, now: clock}
}

func (s *Service) today() string { return s.now().Format(dateLayout) }
func (s *Service) ts() string    { return s.now().UTC().Format(tsLayout) }

func nullDate(p *string) (sql.NullString, error) {
	if p == nil || strings.TrimSpace(*p) == "" {
		return sql.NullString{}, nil
	}
	v := strings.TrimSpace(*p)
	if _, err := time.Parse(dateLayout, v); err != nil {
		return sql.NullString{}, validationf("next_review_on must be YYYY-MM-DD")
	}
	return sql.NullString{String: v, Valid: true}, nil
}

func strPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func (s *Service) load(ctx context.Context, t db.Thesis) (Thesis, error) {
	cs, err := s.q.ListBreakConditions(ctx, t.ID)
	if err != nil {
		return Thesis{}, fmt.Errorf("list break conditions: %w", err)
	}
	out := Thesis{
		ID: t.ID, AssetID: t.AssetID, Text: t.Body, Status: t.Status, NextReviewOn: strPtr(t.NextReviewOn),
		BreakConditions: make([]BreakCondition, 0, len(cs)), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	for _, c := range cs {
		out.BreakConditions = append(out.BreakConditions, BreakCondition{ID: c.ID, Text: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
	}
	return out, nil
}

func (s *Service) row(ctx context.Context, assetID int64) (db.Thesis, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return db.Thesis{}, err
	}
	t, err := s.q.GetThesisByAsset(ctx, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Thesis{}, ErrNotFound
	}
	if err != nil {
		return db.Thesis{}, fmt.Errorf("get thesis: %w", err)
	}
	return t, nil
}

// Get returns the asset's thesis.
func (s *Service) Get(ctx context.Context, assetID int64) (Thesis, error) {
	t, err := s.row(ctx, assetID)
	if err != nil {
		return Thesis{}, err
	}
	return s.load(ctx, t)
}

// HasBreakCondition reports whether the asset has a thesis with at least one
// break condition (part of the buy gate, spec 8.1).
func (s *Service) HasBreakCondition(ctx context.Context, assetID int64) (bool, error) {
	t, err := s.q.GetThesisByAsset(ctx, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cs, err := s.q.ListBreakConditions(ctx, t.ID)
	return len(cs) > 0, err
}

// PutInput creates or replaces the thesis text and next review date.
type PutInput struct {
	Text         string
	NextReviewOn *string
}

// Put creates the thesis (status active) or replaces its text and review
// date. Status and break conditions are untouched on replace.
func (s *Service) Put(ctx context.Context, assetID int64, in PutInput) (Thesis, bool, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return Thesis{}, false, err
	}
	body := strings.TrimSpace(in.Text)
	if body == "" {
		return Thesis{}, false, validationf("text is required")
	}
	review, err := nullDate(in.NextReviewOn)
	if err != nil {
		return Thesis{}, false, err
	}
	created := false
	cur, err := s.q.GetThesisByAsset(ctx, assetID)
	var row db.Thesis
	switch {
	case errors.Is(err, sql.ErrNoRows):
		row, err = s.q.CreateThesis(ctx, db.CreateThesisParams{AssetID: assetID, Body: body, NextReviewOn: review, CreatedAt: s.ts(), UpdatedAt: s.ts()})
		created = true
	case err != nil:
		return Thesis{}, false, fmt.Errorf("get thesis: %w", err)
	default:
		row, err = s.q.UpdateThesis(ctx, db.UpdateThesisParams{Body: body, NextReviewOn: review, UpdatedAt: s.ts(), ID: cur.ID})
	}
	if err != nil {
		return Thesis{}, false, fmt.Errorf("save thesis: %w", err)
	}
	if err := s.Evaluate(ctx); err != nil {
		return Thesis{}, false, err
	}
	t, err := s.load(ctx, row)
	return t, created, err
}

// Delete removes the thesis (and its conditions and reviews).
func (s *Service) Delete(ctx context.Context, assetID int64) error {
	t, err := s.row(ctx, assetID)
	if err != nil {
		return err
	}
	if err := s.q.DeleteThesis(ctx, t.ID); err != nil {
		return fmt.Errorf("delete thesis: %w", err)
	}
	return s.Evaluate(ctx)
}

// SetStatus changes the thesis status. Marking it broken emits thesis_broken
// with a full-exit suggestion; leaving broken resolves it.
func (s *Service) SetStatus(ctx context.Context, assetID int64, status string) (Thesis, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if !validStatus(status) {
		return Thesis{}, validationf("status must be active, under_review or broken")
	}
	t, err := s.row(ctx, assetID)
	if err != nil {
		return Thesis{}, err
	}
	row, err := s.q.SetThesisStatus(ctx, db.SetThesisStatusParams{Status: status, UpdatedAt: s.ts(), ID: t.ID})
	if err != nil {
		return Thesis{}, fmt.Errorf("set status: %w", err)
	}
	if err := s.Evaluate(ctx); err != nil {
		return Thesis{}, err
	}
	return s.load(ctx, row)
}

// ReviewInput records a review. ReviewedOn defaults to today. Status is
// optional: when empty, an under_review thesis returns to active and any
// other status is kept. NextReviewOn schedules the next review.
type ReviewInput struct {
	ReviewedOn   string
	Note         string
	Status       string
	NextReviewOn *string
}

// Review appends a review, applies its outcome (status, next review date) and
// re-evaluates alerts (a future next_review_on clears thesis_review_due).
func (s *Service) Review(ctx context.Context, assetID int64, in ReviewInput) (Review, error) {
	t, err := s.row(ctx, assetID)
	if err != nil {
		return Review{}, err
	}
	on := strings.TrimSpace(in.ReviewedOn)
	if on == "" {
		on = s.today()
	}
	if _, err := time.Parse(dateLayout, on); err != nil {
		return Review{}, validationf("reviewed_on must be YYYY-MM-DD")
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		return Review{}, validationf("note is required")
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	switch {
	case status == "" && t.Status == StatusUnderReview:
		status = StatusActive
	case status == "":
		status = t.Status
	case !validStatus(status):
		return Review{}, validationf("status must be active, under_review or broken")
	}
	next, err := nullDate(in.NextReviewOn)
	if err != nil {
		return Review{}, err
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Review{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	tq := db.New(tx)
	rv, err := tq.CreateThesisReview(ctx, db.CreateThesisReviewParams{
		ThesisID: t.ID, ReviewedOn: on, Note: note, StatusAfter: status, NextReviewOn: next, CreatedAt: s.ts(),
	})
	if err != nil {
		return Review{}, fmt.Errorf("create review: %w", err)
	}
	if _, err := tq.SetThesisReview(ctx, db.SetThesisReviewParams{Status: status, NextReviewOn: next, UpdatedAt: s.ts(), ID: t.ID}); err != nil {
		return Review{}, fmt.Errorf("apply review: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Review{}, fmt.Errorf("commit: %w", err)
	}
	if err := s.Evaluate(ctx); err != nil {
		return Review{}, err
	}
	return toReview(rv), nil
}

func toReview(r db.ThesisReview) Review {
	return Review{ID: r.ID, ThesisID: r.ThesisID, ReviewedOn: r.ReviewedOn, Note: r.Note,
		StatusAfter: r.StatusAfter, NextReviewOn: strPtr(r.NextReviewOn), CreatedAt: r.CreatedAt}
}

// Reviews lists the asset's reviews, newest first.
func (s *Service) Reviews(ctx context.Context, assetID int64) ([]Review, error) {
	t, err := s.row(ctx, assetID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThesisReviews(ctx, t.ID)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	out := make([]Review, 0, len(rows))
	for _, r := range rows {
		out = append(out, toReview(r))
	}
	return out, nil
}

// AddCondition appends a break condition.
func (s *Service) AddCondition(ctx context.Context, assetID int64, text string) (BreakCondition, error) {
	t, err := s.row(ctx, assetID)
	if err != nil {
		return BreakCondition{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return BreakCondition{}, validationf("text is required")
	}
	c, err := s.q.CreateBreakCondition(ctx, db.CreateBreakConditionParams{ThesisID: t.ID, Body: text, CreatedAt: s.ts(), UpdatedAt: s.ts()})
	if err != nil {
		return BreakCondition{}, fmt.Errorf("create condition: %w", err)
	}
	return BreakCondition{ID: c.ID, Text: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}, nil
}

// UpdateCondition edits one break condition.
func (s *Service) UpdateCondition(ctx context.Context, id int64, text string) (BreakCondition, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return BreakCondition{}, validationf("text is required")
	}
	c, err := s.q.UpdateBreakCondition(ctx, db.UpdateBreakConditionParams{Body: text, UpdatedAt: s.ts(), ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return BreakCondition{}, ErrNotFound
	}
	if err != nil {
		return BreakCondition{}, fmt.Errorf("update condition: %w", err)
	}
	return BreakCondition{ID: c.ID, Text: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}, nil
}

// DeleteCondition removes one break condition.
func (s *Service) DeleteCondition(ctx context.Context, id int64) error {
	n, err := s.q.DeleteBreakCondition(ctx, id)
	if err != nil {
		return fmt.Errorf("delete condition: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Evaluate upserts thesis_broken (status broken) and thesis_review_due
// (next_review_on reached, thesis not broken) alerts for every thesis and
// resolves those whose condition no longer holds. Mutations call it, and the
// daily job should call it so a review date that passes raises its alert.
func (s *Service) Evaluate(ctx context.Context) error {
	rows, err := s.q.ListTheses(ctx)
	if err != nil {
		return fmt.Errorf("list theses: %w", err)
	}
	today := s.today()
	var broken, due []string
	for _, t := range rows {
		asset, err := s.assets.Get(ctx, t.AssetID)
		if err != nil {
			return err
		}
		id := t.AssetID
		switch {
		case t.Status == StatusBroken:
			if _, err := s.alerts.Upsert(ctx, AlertBroken, &id, "", map[string]any{
				"asset_id": id, "ticker": asset.Ticker, "suggestion": "full_exit",
				"message": "Thesis is broken: consider a full exit (or a staged exit).",
			}); err != nil {
				return err
			}
			broken = append(broken, alerts.Key(AlertBroken, &id, ""))
		case t.NextReviewOn.Valid && t.NextReviewOn.String <= today:
			if _, err := s.alerts.Upsert(ctx, AlertReviewDue, &id, "", map[string]any{
				"asset_id": id, "ticker": asset.Ticker, "next_review_on": t.NextReviewOn.String,
			}); err != nil {
				return err
			}
			due = append(due, alerts.Key(AlertReviewDue, &id, ""))
		}
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertBroken, broken); err != nil {
		return err
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertReviewDue, due); err != nil {
		return err
	}
	return nil
}
