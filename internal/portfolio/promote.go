package portfolio

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/tactical"
)

// Promote moves a tactical asset into the core sleeve (spec §10.2). It must
// pass the core buy gate: the full score (horizon included), a thesis with a
// break condition and a fresh fair value at or above the price. The tactical
// setup is removed; the asset then joins core target math.
func (s *Service) Promote(ctx context.Context, assetID int64) (db.Asset, error) {
	a, err := s.d.Assets.Get(ctx, assetID)
	if err != nil {
		return db.Asset{}, err
	}
	if a.Sleeve != assets.SleeveTactical {
		return db.Asset{}, fmt.Errorf("%w: %s is already core", ErrValidation, a.Ticker)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return db.Asset{}, err
	}
	var x *Position
	for _, p := range append(append([]Position(nil), snap.Holdings...), snap.Watchlist...) {
		if p.AssetID == assetID {
			x = &p
			break
		}
	}
	if x == nil || x.factors == nil || x.Price == nil {
		return db.Asset{}, fmt.Errorf("%w: %s needs a rating and a price before promotion", ErrValidation, a.Ticker)
	}
	p, err := s.params(ctx)
	if err != nil {
		return db.Asset{}, err
	}
	g, err := p.EvaluateGate(engine.GateInput{
		Ratings: *x.factors, BreakConditions: x.breaks, FairValue: x.FairValue, FairValueDate: x.fvDate,
		Price: *x.Price, Now: s.d.Now(),
	})
	if err != nil {
		return db.Asset{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if !g.Passed {
		var failed []string
		for _, c := range []struct {
			ok   bool
			name string
		}{
			{g.ScoreOK, "core score below " + p.MinScore.String()},
			{g.ThesisOK, "no break condition"},
			{g.FairValueFresh, "no fresh fair value"},
			{g.PriceOK, "price above fair value"},
		} {
			if !c.ok {
				failed = append(failed, c.name)
			}
		}
		return db.Asset{}, fmt.Errorf("%w: core buy gate fails: %s", ErrValidation, strings.Join(failed, ", "))
	}
	core := assets.SleeveCore
	out, err := s.d.Assets.Update(ctx, assetID, assets.UpdateInput{Sleeve: &core})
	if err != nil {
		return db.Asset{}, err
	}
	if s.d.Tactical != nil {
		if err := s.d.Tactical.Delete(ctx, assetID); err != nil && !errors.Is(err, tactical.ErrNotFound) {
			return db.Asset{}, err
		}
	}
	return out, nil
}
