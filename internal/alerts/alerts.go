// Package alerts stores alerts raised by jobs and manages their lifecycle
// (open, snoozed, acknowledged, resolved). See docs/spec.md section 16.
package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Alert states.
const (
	StateOpen         = "open"
	StateSnoozed      = "snoozed"
	StateAcknowledged = "acknowledged"
	StateResolved     = "resolved"
)

// Errors returned by Service.
var (
	ErrInvalidType = errors.New("alerts: unknown alert type")
	ErrInvalid     = errors.New("alerts: invalid argument")
	ErrNotFound    = errors.New("alerts: not found")
	ErrResolved    = errors.New("alerts: alert already resolved")
)

// Types is the full alert type enum from spec section 16.2.
var Types = []string{
	"overweight", "overvalued", "thesis_broken", "thesis_review_due", "fair_value_stale",
	"missing_fair_value", "raise_cash", "deploy_cash", "better_opportunity", "below_floor",
	"undeployed_budget", "too_many_positions", "tactical_target_hit", "tactical_stop_hit",
	"tactical_expired", "tactical_count", "opportunity", "expected_vs_actual_miss",
	"corporate_action_pending", "price_jump_unexplained", "tax_pool_conflict",
	"bootstrap_exit", "data_fetch_failed",
}

// ValidType reports whether t is a known alert type.
func ValidType(t string) bool {
	for _, k := range Types {
		if k == t {
			return true
		}
	}
	return false
}

// timeFormat is fixed-width UTC so stored timestamps compare correctly as text.
const timeFormat = "2006-01-02T15:04:05Z"

// Alert is a stored alert. State is the effective state at read time: a snoozed
// alert whose SnoozedUntil has passed is reported as open.
type Alert struct {
	ID           int64           `json:"id"`
	Type         string          `json:"type"`
	AssetID      *int64          `json:"asset_id"`
	DedupeKey    string          `json:"dedupe_key"`
	Payload      json.RawMessage `json:"payload"`
	State        string          `json:"state"`
	SnoozedUntil *time.Time      `json:"snoozed_until"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	ResolvedAt   *time.Time      `json:"resolved_at"`
}

// Service manages alerts. Clock is injectable for tests.
type Service struct {
	q   *db.Queries
	now func() time.Time
}

// New returns a Service. A nil clock uses time.Now.
func New(conn *sql.DB, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), now: func() time.Time { return clock().UTC().Truncate(time.Second) }}
}

// Key builds the dedupe key: type + asset + variant.
func Key(alertType string, assetID *int64, variant string) string {
	asset := ""
	if assetID != nil {
		asset = fmt.Sprint(*assetID)
	}
	return alertType + "|" + asset + "|" + variant
}

// Upsert creates an open alert unless a non-resolved alert with the same dedupe
// key exists, in which case it only refreshes payload and updated_at (state is
// kept, so acknowledged and snoozed alerts stay that way).
func (s *Service) Upsert(ctx context.Context, alertType string, assetID *int64, variant string, payload any) (Alert, error) {
	if !ValidType(alertType) {
		return Alert{}, fmt.Errorf("%w: %q", ErrInvalidType, alertType)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Alert{}, fmt.Errorf("%w: payload: %v", ErrInvalid, err)
	}
	if payload == nil {
		raw = []byte("{}")
	}
	now := s.now().Format(timeFormat)
	row, err := s.q.UpsertAlert(ctx, db.UpsertAlertParams{
		Type:      alertType,
		AssetID:   nullInt(assetID),
		DedupeKey: Key(alertType, assetID, variant),
		Payload:   string(raw),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return Alert{}, fmt.Errorf("upsert alert: %w", err)
	}
	return s.convert(row), nil
}

// ResolveMissing resolves every non-resolved alert of alertType whose dedupe key
// (see Key) is not in activeKeys. Jobs call it after evaluating a trigger to
// clear alerts whose condition no longer holds. It returns the number resolved.
func (s *Service) ResolveMissing(ctx context.Context, alertType string, activeKeys []string) (int, error) {
	if !ValidType(alertType) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidType, alertType)
	}
	active := make(map[string]struct{}, len(activeKeys))
	for _, k := range activeKeys {
		active[k] = struct{}{}
	}
	rows, err := s.q.ListActiveAlertsByType(ctx, alertType)
	if err != nil {
		return 0, fmt.Errorf("list active alerts: %w", err)
	}
	n := 0
	for _, r := range rows {
		if _, ok := active[r.DedupeKey]; ok {
			continue
		}
		if _, err := s.Resolve(ctx, r.ID); err != nil && !errors.Is(err, ErrResolved) {
			return n, err
		}
		n++
	}
	return n, nil
}

// Acknowledge marks an alert acknowledged.
func (s *Service) Acknowledge(ctx context.Context, id int64) (Alert, error) {
	return s.transition(ctx, id, StateAcknowledged, nil)
}

// Snooze hides an alert until the given time, which must be in the future.
func (s *Service) Snooze(ctx context.Context, id int64, until time.Time) (Alert, error) {
	if !until.After(s.now()) {
		return Alert{}, fmt.Errorf("%w: until must be in the future", ErrInvalid)
	}
	u := until.UTC()
	return s.transition(ctx, id, StateSnoozed, &u)
}

// Resolve marks an alert resolved.
func (s *Service) Resolve(ctx context.Context, id int64) (Alert, error) {
	return s.transition(ctx, id, StateResolved, nil)
}

func (s *Service) transition(ctx context.Context, id int64, state string, until *time.Time) (Alert, error) {
	now := s.now().Format(timeFormat)
	p := db.SetAlertStateParams{State: state, UpdatedAt: now, ID: id}
	if until != nil {
		p.SnoozedUntil = sql.NullString{String: until.Format(timeFormat), Valid: true}
	}
	if state == StateResolved {
		p.ResolvedAt = sql.NullString{String: now, Valid: true}
	}
	row, err := s.q.SetAlertState(ctx, p)
	if errors.Is(err, sql.ErrNoRows) {
		// Either missing or already resolved.
		_, gerr := s.q.GetAlert(ctx, id)
		if errors.Is(gerr, sql.ErrNoRows) {
			return Alert{}, ErrNotFound
		}
		if gerr != nil {
			return Alert{}, fmt.Errorf("get alert: %w", gerr)
		}
		return Alert{}, ErrResolved
	}
	if err != nil {
		return Alert{}, fmt.Errorf("set alert state: %w", err)
	}
	return s.convert(row), nil
}

// Get returns one alert.
func (s *Service) Get(ctx context.Context, id int64) (Alert, error) {
	row, err := s.q.GetAlert(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, ErrNotFound
	}
	if err != nil {
		return Alert{}, fmt.Errorf("get alert: %w", err)
	}
	return s.convert(row), nil
}

// Filter selects alerts for List. Zero values mean "no filter" except State.
type Filter struct {
	// State is the effective state to match. Empty means open (which includes
	// snoozed alerts that are due). "all" disables the state filter.
	State   string
	Type    string
	AssetID *int64
}

// StateAll matches every state in Filter.State.
const StateAll = "all"

// List returns alerts newest first.
//
// Snooze expiry is computed at read time rather than by a sweep: the effective
// state is "open" whenever state = snoozed and snoozed_until <= now. This needs
// no background job, cannot drift if the process is down at the deadline, and
// the injected clock makes it trivially testable. The stored row keeps state
// "snoozed" until something else transitions it.
func (s *Service) List(ctx context.Context, f Filter) ([]Alert, error) {
	if f.Type != "" && !ValidType(f.Type) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidType, f.Type)
	}
	var typ, asset any
	if f.Type != "" {
		typ = f.Type
	}
	if f.AssetID != nil {
		asset = *f.AssetID
	}
	var rows []db.Alert
	var err error
	switch f.State {
	case StateAll:
		rows, err = s.q.ListAllAlerts(ctx, db.ListAllAlertsParams{Type: typ, AssetID: asset})
	case "", StateOpen, StateSnoozed, StateAcknowledged, StateResolved:
		state := f.State
		if state == "" {
			state = StateOpen
		}
		rows, err = s.q.ListAlerts(ctx, db.ListAlertsParams{
			Now:     sql.NullString{String: s.now().Format(timeFormat), Valid: true},
			State:   state,
			Type:    typ,
			AssetID: asset,
		})
	default:
		return nil, fmt.Errorf("%w: state %q", ErrInvalid, f.State)
	}
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	out := make([]Alert, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.convert(r))
	}
	return out, nil
}

func (s *Service) convert(r db.Alert) Alert {
	a := Alert{
		ID:           r.ID,
		Type:         r.Type,
		DedupeKey:    r.DedupeKey,
		Payload:      json.RawMessage(r.Payload),
		State:        r.State,
		SnoozedUntil: parseNull(r.SnoozedUntil),
		CreatedAt:    parse(r.CreatedAt),
		UpdatedAt:    parse(r.UpdatedAt),
		ResolvedAt:   parseNull(r.ResolvedAt),
	}
	if r.AssetID.Valid {
		v := r.AssetID.Int64
		a.AssetID = &v
	}
	if a.State == StateSnoozed && a.SnoozedUntil != nil && !a.SnoozedUntil.After(s.now()) {
		a.State = StateOpen
	}
	return a
}

func nullInt(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func parse(v string) time.Time {
	t, _ := time.Parse(timeFormat, v)
	return t
}

func parseNull(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t := parse(v.String)
	return &t
}
