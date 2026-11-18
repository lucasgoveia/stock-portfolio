// Package ratings stores the append-only rating history and computes the
// weighted score. See docs/spec.md sections 5.1 and 5.3.
package ratings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("rating not found")
	ErrValidation = errors.New("rating validation error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const dateLayout = "2006-01-02"

// Settings keys for factor weights.
const (
	KeyWeightVolatility     = "rating.weight.volatility"
	KeyWeightUnderstanding  = "rating.weight.understanding"
	KeyWeightMoat           = "rating.weight.moat"
	KeyWeightPredictability = "rating.weight.predictability"
	KeyWeightHorizon        = "rating.weight.horizon"
)

// Rating is one appended rating row with its weighted score (computed with
// the weights in force when read).
type Rating struct {
	ID             int64           `json:"id"`
	AssetID        int64           `json:"asset_id"`
	RatedOn        string          `json:"rated_on"`
	Volatility     int             `json:"volatility"`
	Understanding  int             `json:"understanding"`
	Moat           int             `json:"moat"`
	Predictability int             `json:"predictability"`
	Horizon        int             `json:"horizon"`
	Reason         string          `json:"reason"`
	Score          decimal.Decimal `json:"score"`
	CreatedAt      string          `json:"created_at"`
}

// Factors returns the engine input for r.
func (r Rating) Factors() engine.Factors {
	return engine.Factors{Volatility: r.Volatility, Understanding: r.Understanding, Moat: r.Moat,
		Predictability: r.Predictability, Horizon: r.Horizon}
}

// AddInput is a new rating. RatedOn defaults to today.
type AddInput struct {
	RatedOn        string
	Volatility     int
	Understanding  int
	Moat           int
	Predictability int
	Horizon        int
	Reason         string
}

// ChangeHook runs after a rating is appended so targets can be recomputed.
type ChangeHook func(ctx context.Context, assetID int64) error

// Service manages ratings and factor weights.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	now    func() time.Time
	hooks  []ChangeHook
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, now: clock}
}

// OnChange registers a hook called after every appended rating (the target
// recompute trigger of spec 5.3). Hook failures are logged, not returned: the
// rating is already stored. Register hooks before serving requests.
func (s *Service) OnChange(h ChangeHook) { s.hooks = append(s.hooks, h) }

func toRating(r db.Rating, w engine.Weights) (Rating, error) {
	out := Rating{
		ID: r.ID, AssetID: r.AssetID, RatedOn: r.RatedOn, Volatility: int(r.Volatility),
		Understanding: int(r.Understanding), Moat: int(r.Moat), Predictability: int(r.Predictability),
		Horizon: int(r.Horizon), Reason: r.Reason, CreatedAt: r.CreatedAt,
	}
	score, err := engine.WeightedScore(out.Factors(), w)
	out.Score = score
	return out, err
}

// Add appends a rating (never edits or removes history).
func (s *Service) Add(ctx context.Context, assetID int64, in AddInput) (Rating, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return Rating{}, err
	}
	on := strings.TrimSpace(in.RatedOn)
	if on == "" {
		on = s.now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, on); err != nil {
		return Rating{}, validationf("rated_on must be YYYY-MM-DD")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return Rating{}, validationf("reason is required")
	}
	for name, v := range map[string]int{"volatility": in.Volatility, "understanding": in.Understanding,
		"moat": in.Moat, "predictability": in.Predictability, "horizon": in.Horizon} {
		if v < 1 || v > 5 {
			return Rating{}, validationf("%s must be between 1 and 5", name)
		}
	}
	w, err := s.Weights(ctx)
	if err != nil {
		return Rating{}, err
	}
	row, err := s.q.CreateRating(ctx, db.CreateRatingParams{
		AssetID: assetID, RatedOn: on, Volatility: int64(in.Volatility), Understanding: int64(in.Understanding),
		Moat: int64(in.Moat), Predictability: int64(in.Predictability), Horizon: int64(in.Horizon), Reason: reason,
	})
	if err != nil {
		return Rating{}, fmt.Errorf("create rating: %w", err)
	}
	for _, h := range s.hooks {
		if err := h(ctx, assetID); err != nil {
			slog.Warn("rating change hook failed", "asset_id", assetID, "err", err)
		}
	}
	return toRating(row, w)
}

// History returns an asset's ratings, newest first (by rated_on, then id).
func (s *Service) History(ctx context.Context, assetID int64) ([]Rating, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return nil, err
	}
	w, err := s.Weights(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRatingsByAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list ratings: %w", err)
	}
	out := make([]Rating, 0, len(rows))
	for _, r := range rows {
		x, err := toRating(r, w)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// Latest returns the current rating (latest wins) or ErrNotFound.
func (s *Service) Latest(ctx context.Context, assetID int64) (Rating, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return Rating{}, err
	}
	w, err := s.Weights(ctx)
	if err != nil {
		return Rating{}, err
	}
	row, err := s.q.LatestRatingByAsset(ctx, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return Rating{}, ErrNotFound
	}
	if err != nil {
		return Rating{}, fmt.Errorf("latest rating: %w", err)
	}
	return toRating(row, w)
}

// WeightsInput are the factor weights as stored.
type WeightsInput struct {
	Volatility     money.Decimal `json:"volatility"`
	Understanding  money.Decimal `json:"understanding"`
	Moat           money.Decimal `json:"moat"`
	Predictability money.Decimal `json:"predictability"`
	Horizon        money.Decimal `json:"horizon"`
}

func (s *Service) weight(ctx context.Context, key string, def decimal.Decimal) (decimal.Decimal, error) {
	v, err := s.q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("get setting %s: %w", key, err)
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		return def, fmt.Errorf("setting %s: %w", key, err)
	}
	return d, nil
}

// Weights returns the factor weights from settings, defaulting to 1/1/1.5/1.5/1.
func (s *Service) Weights(ctx context.Context) (engine.Weights, error) {
	w := engine.DefaultWeights()
	var err error
	for _, p := range []struct {
		key string
		dst *decimal.Decimal
	}{
		{KeyWeightVolatility, &w.Volatility}, {KeyWeightUnderstanding, &w.Understanding},
		{KeyWeightMoat, &w.Moat}, {KeyWeightPredictability, &w.Predictability},
		{KeyWeightHorizon, &w.Horizon},
	} {
		if *p.dst, err = s.weight(ctx, p.key, *p.dst); err != nil {
			return w, err
		}
	}
	return w, nil
}

// SetWeights validates and stores the factor weights (each >= 0, sum > 0).
func (s *Service) SetWeights(ctx context.Context, in WeightsInput) (engine.Weights, error) {
	w := engine.Weights{Volatility: in.Volatility.Decimal, Understanding: in.Understanding.Decimal,
		Moat: in.Moat.Decimal, Predictability: in.Predictability.Decimal, Horizon: in.Horizon.Decimal}
	if _, err := engine.WeightedScore(engine.Factors{Volatility: 1, Understanding: 1, Moat: 1, Predictability: 1, Horizon: 1}, w); err != nil {
		return engine.Weights{}, validationf("weights must be non-negative with a positive sum")
	}
	for key, v := range map[string]decimal.Decimal{
		KeyWeightVolatility: w.Volatility, KeyWeightUnderstanding: w.Understanding, KeyWeightMoat: w.Moat,
		KeyWeightPredictability: w.Predictability, KeyWeightHorizon: w.Horizon,
	} {
		if err := s.q.SetSetting(ctx, db.SetSettingParams{Key: key, Value: v.String()}); err != nil {
			return engine.Weights{}, fmt.Errorf("set setting %s: %w", key, err)
		}
	}
	return w, nil
}
