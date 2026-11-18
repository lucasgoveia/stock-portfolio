package cash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// PairUSDBRL is the fx_rates pair used for USD cash.
const PairUSDBRL = "USDBRL"

// ErrNoRate means a valuation input (price, CDI series, FX) is not available.
var ErrNoRate = errors.New("cash: missing valuation input")

// Quote is a rate with its reference date (CDI and Tesouro are published D-1,
// so the date can be earlier than the valuation date).
type Quote struct {
	Value decimal.Decimal `json:"value"`
	Date  string          `json:"date"`
}

// Rates supplies valuation inputs. The market data job fills the backing
// tables; tests seed them directly.
type Rates interface {
	// CDIRates returns daily CDI rates (percent per day) with from <= date <= to, ascending.
	CDIRates(ctx context.Context, from, to string) ([]CDIRate, error)
	// TesouroSelicPrice returns the latest price dated on or before asof.
	TesouroSelicPrice(ctx context.Context, asof string) (Quote, error)
	// USDBRL returns the latest BRL per USD dated on or before asof.
	USDBRL(ctx context.Context, asof string) (Quote, error)
}

// RateStore is the SQLite-backed Rates implementation, also used by the
// market data job to write inputs.
type RateStore struct{ q *db.Queries }

// NewRateStore builds a RateStore.
func NewRateStore(conn db.DBTX) *RateStore { return &RateStore{q: db.New(conn)} }

// UpsertCDI stores the daily CDI rate (percent per day) for refDate.
func (s *RateStore) UpsertCDI(ctx context.Context, refDate string, ratePct decimal.Decimal) error {
	return s.q.UpsertCDIRate(ctx, db.UpsertCDIRateParams{RefDate: refDate, RatePct: ratePct.String()})
}

// UpsertTesouroSelicPrice stores the Tesouro Selic unit price for refDate.
func (s *RateStore) UpsertTesouroSelicPrice(ctx context.Context, refDate string, price decimal.Decimal) error {
	return s.q.UpsertTesouroSelicPrice(ctx, db.UpsertTesouroSelicPriceParams{RefDate: refDate, Price: price.String()})
}

// UpsertUSDBRL stores BRL per USD for refDate.
func (s *RateStore) UpsertUSDBRL(ctx context.Context, refDate string, rate decimal.Decimal) error {
	return s.q.UpsertFXRate(ctx, db.UpsertFXRateParams{Pair: PairUSDBRL, RefDate: refDate, Rate: rate.String()})
}

// CDIRates implements Rates.
func (s *RateStore) CDIRates(ctx context.Context, from, to string) ([]CDIRate, error) {
	rows, err := s.q.ListCDIRates(ctx, db.ListCDIRatesParams{RefDate: from, RefDate_2: to})
	if err != nil {
		return nil, fmt.Errorf("list cdi rates: %w", err)
	}
	out := make([]CDIRate, 0, len(rows))
	for _, r := range rows {
		p, err := decimal.NewFromString(r.RatePct)
		if err != nil {
			return nil, fmt.Errorf("cdi rate %s: %w", r.RefDate, err)
		}
		out = append(out, CDIRate{Date: r.RefDate, Pct: p})
	}
	return out, nil
}

// TesouroSelicPrice implements Rates.
func (s *RateStore) TesouroSelicPrice(ctx context.Context, asof string) (Quote, error) {
	r, err := s.q.LatestTesouroSelicPrice(ctx, asof)
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, fmt.Errorf("%w: tesouro selic price on or before %s", ErrNoRate, asof)
	}
	if err != nil {
		return Quote{}, fmt.Errorf("tesouro price: %w", err)
	}
	v, err := decimal.NewFromString(r.Price)
	return Quote{Value: v, Date: r.RefDate}, err
}

// USDBRL implements Rates.
func (s *RateStore) USDBRL(ctx context.Context, asof string) (Quote, error) {
	r, err := s.q.LatestFXRate(ctx, db.LatestFXRateParams{Pair: PairUSDBRL, RefDate: asof})
	if errors.Is(err, sql.ErrNoRows) {
		return Quote{}, fmt.Errorf("%w: USDBRL on or before %s", ErrNoRate, asof)
	}
	if err != nil {
		return Quote{}, fmt.Errorf("fx rate: %w", err)
	}
	v, err := decimal.NewFromString(r.Rate)
	return Quote{Value: v, Date: r.RefDate}, err
}
