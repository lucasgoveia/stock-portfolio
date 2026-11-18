// Package assets holds the asset domain: validation, lifecycle transitions
// and persistence.
package assets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Enum values.
const (
	MarketB3 = "B3"
	MarketUS = "US"

	CurrencyBRL = "BRL"
	CurrencyUSD = "USD"

	KindStock = "stock"
	KindBDR   = "bdr"
	KindETF   = "etf"

	SleeveCore     = "core"
	SleeveTactical = "tactical"

	StatusWatchlist = "watchlist"
	StatusHolding   = "holding"
	StatusExited    = "exited"

	BenchmarkIBOV  = "IBOV"
	BenchmarkSP500 = "SP500"
)

// Sentinel errors.
var (
	ErrNotFound          = errors.New("asset not found")
	ErrDuplicate         = errors.New("asset already exists for this market and ticker")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrValidation        = errors.New("validation error")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

var transitions = map[string][]string{
	StatusWatchlist: {StatusHolding},
	StatusHolding:   {StatusExited},
	StatusExited:    {StatusWatchlist},
}

// CanTransition reports whether from -> to is an allowed lifecycle move.
func CanTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// DefaultBenchmark returns the default benchmark index for a market.
func DefaultBenchmark(market string) string {
	if market == MarketUS {
		return BenchmarkSP500
	}
	return BenchmarkIBOV
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// CreateInput is the data needed to create an asset. Currency and Benchmark
// are optional and derived from Market when empty.
type CreateInput struct {
	Ticker    string
	Name      string
	Market    string
	Currency  string
	Kind      string
	Sleeve    string
	Benchmark string
}

// UpdateInput holds optional field changes.
type UpdateInput struct {
	Name      *string
	Sleeve    *string
	Benchmark *string
}

// Filter restricts List results; empty fields are ignored.
type Filter struct {
	Status string
	Sleeve string
	Market string
}

// Service manages assets.
type Service struct {
	q *db.Queries
}

// NewService builds a Service on top of a database handle.
func NewService(conn db.DBTX) *Service {
	return &Service{q: db.New(conn)}
}

// Create validates and inserts an asset with status watchlist.
func (s *Service) Create(ctx context.Context, in CreateInput) (db.Asset, error) {
	in.Ticker = strings.ToUpper(strings.TrimSpace(in.Ticker))
	in.Name = strings.TrimSpace(in.Name)
	in.Market = strings.ToUpper(strings.TrimSpace(in.Market))
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.Sleeve = strings.ToLower(strings.TrimSpace(in.Sleeve))
	in.Benchmark = strings.ToUpper(strings.TrimSpace(in.Benchmark))

	if in.Ticker == "" {
		return db.Asset{}, validationf("ticker is required")
	}
	if !oneOf(in.Market, MarketB3, MarketUS) {
		return db.Asset{}, validationf("market must be B3 or US")
	}
	wantCur := CurrencyBRL
	if in.Market == MarketUS {
		wantCur = CurrencyUSD
	}
	if in.Currency == "" {
		in.Currency = wantCur
	}
	if in.Currency != wantCur {
		return db.Asset{}, validationf("currency for market %s must be %s", in.Market, wantCur)
	}
	if !oneOf(in.Kind, KindStock, KindBDR, KindETF) {
		return db.Asset{}, validationf("kind must be stock, bdr or etf")
	}
	if in.Kind == KindBDR && in.Market != MarketB3 {
		return db.Asset{}, validationf("bdr is only valid on market B3")
	}
	if in.Sleeve == "" {
		in.Sleeve = SleeveCore
	}
	if !oneOf(in.Sleeve, SleeveCore, SleeveTactical) {
		return db.Asset{}, validationf("sleeve must be core or tactical")
	}
	if in.Benchmark == "" {
		in.Benchmark = DefaultBenchmark(in.Market)
	}
	if !oneOf(in.Benchmark, BenchmarkIBOV, BenchmarkSP500) {
		return db.Asset{}, validationf("benchmark must be IBOV or SP500")
	}

	a, err := s.q.CreateAsset(ctx, db.CreateAssetParams{
		Ticker: in.Ticker, Name: in.Name, Market: in.Market, Currency: in.Currency,
		Kind: in.Kind, Sleeve: in.Sleeve, Status: StatusWatchlist, Benchmark: in.Benchmark,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return db.Asset{}, ErrDuplicate
		}
		return db.Asset{}, fmt.Errorf("create asset: %w", err)
	}
	return a, nil
}

// Get returns one asset.
func (s *Service) Get(ctx context.Context, id int64) (db.Asset, error) {
	a, err := s.q.GetAsset(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Asset{}, ErrNotFound
	}
	if err != nil {
		return db.Asset{}, fmt.Errorf("get asset: %w", err)
	}
	return a, nil
}

// List returns assets matching the filter.
func (s *Service) List(ctx context.Context, f Filter) ([]db.Asset, error) {
	if f.Status != "" && !oneOf(f.Status, StatusWatchlist, StatusHolding, StatusExited) {
		return nil, validationf("invalid status filter")
	}
	if f.Sleeve != "" && !oneOf(f.Sleeve, SleeveCore, SleeveTactical) {
		return nil, validationf("invalid sleeve filter")
	}
	f.Market = strings.ToUpper(f.Market)
	if f.Market != "" && !oneOf(f.Market, MarketB3, MarketUS) {
		return nil, validationf("invalid market filter")
	}
	p := db.ListAssetsParams{}
	if f.Status != "" {
		p.Status = f.Status
	}
	if f.Sleeve != "" {
		p.Sleeve = f.Sleeve
	}
	if f.Market != "" {
		p.Market = f.Market
	}
	out, err := s.q.ListAssets(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	if out == nil {
		out = []db.Asset{}
	}
	return out, nil
}

func nullStr(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: strings.TrimSpace(*p), Valid: true}
}

// Update applies the non-nil fields.
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (db.Asset, error) {
	if in.Sleeve != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Sleeve))
		if !oneOf(v, SleeveCore, SleeveTactical) {
			return db.Asset{}, validationf("sleeve must be core or tactical")
		}
		in.Sleeve = &v
	}
	if in.Benchmark != nil {
		v := strings.ToUpper(strings.TrimSpace(*in.Benchmark))
		if !oneOf(v, BenchmarkIBOV, BenchmarkSP500) {
			return db.Asset{}, validationf("benchmark must be IBOV or SP500")
		}
		in.Benchmark = &v
	}
	a, err := s.q.UpdateAsset(ctx, db.UpdateAssetParams{
		Name: nullStr(in.Name), Sleeve: nullStr(in.Sleeve), Benchmark: nullStr(in.Benchmark), ID: id,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return db.Asset{}, ErrNotFound
	}
	if err != nil {
		return db.Asset{}, fmt.Errorf("update asset: %w", err)
	}
	return a, nil
}

// Transition moves an asset to a new status if the lifecycle allows it.
// Trades (issue #5) call this to drive holding/exited automatically.
func (s *Service) Transition(ctx context.Context, id int64, to string) (db.Asset, error) {
	to = strings.ToLower(strings.TrimSpace(to))
	if !oneOf(to, StatusWatchlist, StatusHolding, StatusExited) {
		return db.Asset{}, validationf("status must be watchlist, holding or exited")
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return db.Asset{}, err
	}
	if !CanTransition(cur.Status, to) {
		return db.Asset{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, cur.Status, to)
	}
	a, err := s.q.UpdateAssetStatus(ctx, db.UpdateAssetStatusParams{Status: to, ID: id})
	if err != nil {
		return db.Asset{}, fmt.Errorf("update status: %w", err)
	}
	return a, nil
}
