// Package valuation stores versioned fair values (spec §12.1) and raises the
// fair_value_stale and missing_fair_value alerts.
package valuation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Methods.
const (
	MethodScenarioDCF = "scenario_dcf"
	MethodBazin       = "bazin"
	MethodGraham      = "graham"
	MethodManual      = "manual"
	MethodTargetPrice = "target_price"
)

// Alert types raised by Evaluate.
const (
	AlertStale   = "fair_value_stale"
	AlertMissing = "missing_fair_value"
)

// MaxAgeMonths is how old a fair value may be before it is stale.
const MaxAgeMonths = 12

// Errors returned by Service.
var (
	ErrNotFound   = errors.New("fair value not found")
	ErrValidation = errors.New("fair value validation error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02T15:04:05Z"
)

// FairValue is one dated fair value version.
type FairValue struct {
	ID            int64           `json:"id"`
	AssetID       int64           `json:"asset_id"`
	Method        string          `json:"method"`
	ValuePerShare money.Decimal   `json:"value_per_share"`
	Currency      string          `json:"currency"`
	AsOf          string          `json:"as_of"`
	Inputs        json.RawMessage `json:"inputs"`
	Sources       []string        `json:"sources"`
	Note          string          `json:"note"`
	Stale         bool            `json:"stale"`
	CreatedAt     string          `json:"created_at"`
}

// CreateInput is a new version. For scenario_dcf, DCF is required and the
// value is computed; for every other method ValuePerShare is required.
// Currency defaults to the asset's currency and AsOf to today.
type CreateInput struct {
	Method        string
	ValuePerShare *money.Decimal
	DCF           *engine.DCFInput
	Currency      string
	AsOf          string
	Sources       []string
	Note          string
	// Inputs documents how a helper value (bazin, graham) was computed; it is
	// stored as the version's inputs JSON. For scenario_dcf the keys are added
	// next to the computed dcf and result (for example an analyst run id).
	Inputs map[string]any
}

// Service manages fair values.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	alerts *alerts.Service
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now; a nil alerts
// service disables Evaluate.
func NewService(conn db.DBTX, a *assets.Service, al *alerts.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, alerts: al, now: clock}
}

func (s *Service) today() string { return s.now().Format(dateLayout) }

// IsStale reports whether a fair value dated asOf is older than 12 months on today.
func IsStale(asOf, today string) bool {
	a, err := time.Parse(dateLayout, asOf)
	if err != nil {
		return true
	}
	t, err := time.Parse(dateLayout, today)
	if err != nil {
		return false
	}
	return t.After(a.AddDate(0, MaxAgeMonths, 0))
}

func (s *Service) convert(r db.FairValue) (FairValue, error) {
	v, err := money.NewFromString(r.ValuePerShare)
	if err != nil {
		return FairValue{}, fmt.Errorf("fair value %d: %w", r.ID, err)
	}
	fv := FairValue{
		ID: r.ID, AssetID: r.AssetID, Method: r.Method, ValuePerShare: v, Currency: r.Currency,
		AsOf: r.AsOf, Inputs: json.RawMessage(r.Inputs), Note: r.Note, CreatedAt: r.CreatedAt,
		Sources: []string{}, Stale: IsStale(r.AsOf, s.today()),
	}
	_ = json.Unmarshal([]byte(r.Sources), &fv.Sources)
	return fv, nil
}

// Preview computes a scenario DCF without storing it.
func Preview(in engine.DCFInput) (engine.DCFResult, error) {
	res, err := engine.ScenarioDCF(in)
	if errors.Is(err, engine.ErrInvalidInput) {
		return res, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return res, err
}

// Create stores a new fair value version.
func (s *Service) Create(ctx context.Context, assetID int64, in CreateInput) (FairValue, error) {
	asset, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return FairValue{}, err
	}
	method := strings.ToLower(strings.TrimSpace(in.Method))
	var (
		value  money.Decimal
		inputs = []byte("{}")
	)
	switch method {
	case MethodScenarioDCF:
		if in.DCF == nil {
			return FairValue{}, validationf("scenario_dcf needs dcf inputs")
		}
		res, err := Preview(*in.DCF)
		if err != nil {
			return FairValue{}, err
		}
		value = money.New(res.ValuePerShare)
		stored := map[string]any{"dcf": in.DCF, "result": res}
		for k, v := range in.Inputs { // extra provenance, such as the analyst run id
			if _, taken := stored[k]; !taken {
				stored[k] = v
			}
		}
		if inputs, err = json.Marshal(stored); err != nil {
			return FairValue{}, err
		}
	case MethodBazin, MethodGraham, MethodManual, MethodTargetPrice:
		if in.DCF != nil {
			return FairValue{}, validationf("dcf inputs only apply to scenario_dcf")
		}
		if in.ValuePerShare == nil {
			return FairValue{}, validationf("value_per_share is required")
		}
		value = *in.ValuePerShare
		if len(in.Inputs) > 0 {
			if inputs, err = json.Marshal(in.Inputs); err != nil {
				return FairValue{}, validationf("inputs must be a JSON object")
			}
		}
	default:
		return FairValue{}, validationf("method must be one of scenario_dcf, bazin, graham, manual, target_price")
	}
	if !value.IsPositive() {
		return FairValue{}, validationf("value per share must be positive (got %s)", value.String())
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = asset.Currency
	}
	if currency != "BRL" && currency != "USD" {
		return FairValue{}, validationf("currency must be BRL or USD")
	}
	asOf := strings.TrimSpace(in.AsOf)
	if asOf == "" {
		asOf = s.today()
	}
	if _, err := time.Parse(dateLayout, asOf); err != nil {
		return FairValue{}, validationf("as_of must be YYYY-MM-DD")
	}
	if asOf > s.today() {
		return FairValue{}, validationf("as_of cannot be in the future")
	}
	srcs := make([]string, 0, len(in.Sources))
	for _, v := range in.Sources {
		if v = strings.TrimSpace(v); v != "" {
			srcs = append(srcs, v)
		}
	}
	srcJSON, err := json.Marshal(srcs)
	if err != nil {
		return FairValue{}, err
	}
	row, err := s.q.CreateFairValue(ctx, db.CreateFairValueParams{
		AssetID: assetID, Method: method, ValuePerShare: value.String(), Currency: currency, AsOf: asOf,
		Inputs: string(inputs), Sources: string(srcJSON), Note: strings.TrimSpace(in.Note),
		CreatedAt: s.now().UTC().Format(tsLayout),
	})
	if err != nil {
		return FairValue{}, fmt.Errorf("create fair value: %w", err)
	}
	fv, err := s.convert(row)
	if err != nil {
		return FairValue{}, err
	}
	return fv, s.Evaluate(ctx)
}

// Get returns one version.
func (s *Service) Get(ctx context.Context, id int64) (FairValue, error) {
	row, err := s.q.GetFairValue(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return FairValue{}, ErrNotFound
	}
	if err != nil {
		return FairValue{}, fmt.Errorf("get fair value: %w", err)
	}
	return s.convert(row)
}

// List returns an asset's versions, newest first.
func (s *Service) List(ctx context.Context, assetID int64) ([]FairValue, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListFairValuesByAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list fair values: %w", err)
	}
	out := make([]FairValue, 0, len(rows))
	for _, r := range rows {
		fv, err := s.convert(r)
		if err != nil {
			return nil, err
		}
		out = append(out, fv)
	}
	return out, nil
}

// Latest returns the newest version of an asset (by as_of, then id).
func (s *Service) Latest(ctx context.Context, assetID int64) (FairValue, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return FairValue{}, err
	}
	row, err := s.q.LatestFairValue(ctx, assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return FairValue{}, ErrNotFound
	}
	if err != nil {
		return FairValue{}, fmt.Errorf("latest fair value: %w", err)
	}
	return s.convert(row)
}

// LatestAll returns the newest version per asset, keyed by asset id.
func (s *Service) LatestAll(ctx context.Context) (map[int64]FairValue, error) {
	rows, err := s.q.ListLatestFairValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest fair values: %w", err)
	}
	out := make(map[int64]FairValue, len(rows))
	for _, r := range rows {
		fv, err := s.convert(r)
		if err != nil {
			return nil, err
		}
		out[r.AssetID] = fv
	}
	return out, nil
}

// Delete removes a version.
func (s *Service) Delete(ctx context.Context, id int64) error {
	n, err := s.q.DeleteFairValue(ctx, id)
	if err != nil {
		return fmt.Errorf("delete fair value: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.Evaluate(ctx)
}

// Evaluate raises fair_value_stale for holdings and watchlist assets whose
// latest version is older than 12 months, missing_fair_value for holdings
// without any version, and resolves alerts whose condition cleared.
func (s *Service) Evaluate(ctx context.Context) error {
	if s.alerts == nil {
		return nil
	}
	latest, err := s.LatestAll(ctx)
	if err != nil {
		return err
	}
	var stale, missing []string
	for _, status := range []string{assets.StatusHolding, assets.StatusWatchlist} {
		list, err := s.assets.List(ctx, assets.Filter{Status: status})
		if err != nil {
			return err
		}
		for _, a := range list {
			id := a.ID
			fv, ok := latest[id]
			switch {
			case !ok && status == assets.StatusHolding:
				if _, err := s.alerts.Upsert(ctx, AlertMissing, &id, "", map[string]any{
					"asset_id": id, "ticker": a.Ticker,
				}); err != nil {
					return err
				}
				missing = append(missing, alerts.Key(AlertMissing, &id, ""))
			case ok && fv.Stale:
				if _, err := s.alerts.Upsert(ctx, AlertStale, &id, "", map[string]any{
					"asset_id": id, "ticker": a.Ticker, "as_of": fv.AsOf, "fair_value_id": fv.ID,
				}); err != nil {
					return err
				}
				stale = append(stale, alerts.Key(AlertStale, &id, ""))
			}
		}
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertStale, stale); err != nil {
		return err
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertMissing, missing); err != nil {
		return err
	}
	return nil
}
