// Package tactical stores tactical setups (spec §10): the catalyst note,
// entry, target and stop prices and expiry of a short-horizon position.
package tactical

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Errors.
var (
	ErrValidation = errors.New("tactical validation error")
	ErrNotFound   = errors.New("tactical setup not found")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const dateLayout = "2006-01-02"

// Setup is a stored tactical setup.
type Setup struct {
	AssetID     int64         `json:"asset_id"`
	NoteID      int64         `json:"note_id"`
	EntryPrice  money.Decimal `json:"entry_price"`
	TargetPrice money.Decimal `json:"target_price"`
	StopPrice   money.Decimal `json:"stop_price"`
	ExpiresOn   string        `json:"expires_on"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
	// Sizing follows from entry and stop.
	Sizing engine.TacticalSizing `json:"sizing"`
}

// Input creates or replaces a setup.
type Input struct {
	NoteID      int64
	EntryPrice  money.Decimal
	TargetPrice money.Decimal
	StopPrice   money.Decimal
	ExpiresOn   string
}

// Service manages tactical setups.
type Service struct {
	q        *db.Queries
	assets   *assets.Service
	research *research.Service
	params   engine.Params
}

// NewService builds a Service with the default rule params.
func NewService(conn db.DBTX, a *assets.Service, r *research.Service) *Service {
	return &Service{q: db.New(conn), assets: a, research: r, params: engine.DefaultParams()}
}

func (s *Service) convert(r db.TacticalSetup) (Setup, error) {
	out := Setup{AssetID: r.AssetID, NoteID: r.NoteID, ExpiresOn: r.ExpiresOn, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	for _, f := range []struct {
		dst *money.Decimal
		src string
	}{{&out.EntryPrice, r.EntryPrice}, {&out.TargetPrice, r.TargetPrice}, {&out.StopPrice, r.StopPrice}} {
		v, err := money.NewFromString(f.src)
		if err != nil {
			return Setup{}, err
		}
		*f.dst = v
	}
	sz, err := s.params.TacticalSize(out.EntryPrice.Decimal, out.StopPrice.Decimal)
	if err != nil {
		return Setup{}, err
	}
	out.Sizing = sz
	return out, nil
}

// Put creates or replaces the setup of a tactical-sleeve asset.
func (s *Service) Put(ctx context.Context, assetID int64, in Input) (Setup, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return Setup{}, err
	}
	if a.Sleeve != assets.SleeveTactical {
		return Setup{}, validationf("%s is in the core sleeve; move it to tactical first", a.Ticker)
	}
	n, err := s.research.Get(ctx, in.NoteID)
	if errors.Is(err, research.ErrNotFound) || (err == nil && n.AssetID != assetID) {
		return Setup{}, validationf("note_id must be a research note of %s describing the catalyst", a.Ticker)
	}
	if err != nil {
		return Setup{}, err
	}
	entry, target, stop := in.EntryPrice.Decimal, in.TargetPrice.Decimal, in.StopPrice.Decimal
	if !entry.IsPositive() || !target.IsPositive() || !stop.IsPositive() {
		return Setup{}, validationf("entry, target and stop prices must be positive")
	}
	if !stop.LessThan(entry) || !target.GreaterThan(entry) {
		return Setup{}, validationf("prices must satisfy stop < entry < target")
	}
	exp := strings.TrimSpace(in.ExpiresOn)
	if _, err := time.Parse(dateLayout, exp); err != nil {
		return Setup{}, validationf("expires_on must be YYYY-MM-DD")
	}
	row, err := s.q.UpsertTacticalSetup(ctx, db.UpsertTacticalSetupParams{
		AssetID: assetID, NoteID: in.NoteID, EntryPrice: entry.String(), TargetPrice: target.String(),
		StopPrice: stop.String(), ExpiresOn: exp,
	})
	if err != nil {
		return Setup{}, err
	}
	return s.convert(row)
}

// Get returns an asset's setup.
func (s *Service) Get(ctx context.Context, assetID int64) (Setup, error) {
	row, err := s.q.GetTacticalSetup(ctx, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return Setup{}, ErrNotFound
	}
	if err != nil {
		return Setup{}, err
	}
	return s.convert(row)
}

// All returns every setup by asset id.
func (s *Service) All(ctx context.Context) (map[int64]Setup, error) {
	rows, err := s.q.ListTacticalSetups(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Setup, len(rows))
	for _, r := range rows {
		st, err := s.convert(r)
		if err != nil {
			return nil, err
		}
		out[r.AssetID] = st
	}
	return out, nil
}

// Delete removes an asset's setup.
func (s *Service) Delete(ctx context.Context, assetID int64) error {
	n, err := s.q.DeleteTacticalSetup(ctx, assetID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
