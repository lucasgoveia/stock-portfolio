// Package trades records buy/sell trades and derives positions (quantity,
// average cost, realized gain). See docs/spec.md sections 4.2 and 11.1.
package trades

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Trade sides.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

// Errors returned by Service.
var (
	ErrNotFound             = errors.New("trade not found")
	ErrValidation           = errors.New("trade validation error")
	ErrInsufficientQuantity = errors.New("insufficient quantity")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Trade is one recorded buy or sell.
type Trade struct {
	ID       int64           `json:"id"`
	AssetID  int64           `json:"asset_id"`
	Date     string          `json:"date"`
	Side     string          `json:"side"`
	Quantity decimal.Decimal `json:"quantity"`
	Price    decimal.Decimal `json:"price"`
	Fees     decimal.Decimal `json:"fees"`
	Currency string          `json:"currency"`
	// FXRate is BRL per unit of Currency on the trade date (1 for BRL).
	FXRate    decimal.Decimal `json:"fx_rate"`
	CreatedAt string          `json:"created_at"`
}

// CreateInput is a trade to record. Currency defaults to the asset's currency;
// Fees default to zero; FXRate defaults to 1 for BRL assets and is required for USD.
type CreateInput struct {
	Date     string
	Side     string
	Quantity money.Decimal
	Price    money.Decimal
	Fees     *money.Decimal
	Currency string
	FXRate   *money.Decimal
}

// Service manages trades and positions.
type Service struct {
	conn   *sql.DB
	assets *assets.Service
}

// NewService builds a Service. The assets service drives lifecycle transitions.
func NewService(conn *sql.DB, a *assets.Service) *Service {
	return &Service{conn: conn, assets: a}
}

func convert(r db.Trade) (Trade, error) {
	t := Trade{ID: r.ID, AssetID: r.AssetID, Date: r.TradeDate, Side: r.Side, Currency: r.Currency, CreatedAt: r.CreatedAt}
	var err error
	if t.Quantity, err = decimal.NewFromString(r.Quantity); err != nil {
		return t, fmt.Errorf("trade %d quantity: %w", r.ID, err)
	}
	if t.Price, err = decimal.NewFromString(r.Price); err != nil {
		return t, fmt.Errorf("trade %d price: %w", r.ID, err)
	}
	if t.Fees, err = decimal.NewFromString(r.Fees); err != nil {
		return t, fmt.Errorf("trade %d fees: %w", r.ID, err)
	}
	if t.FXRate, err = decimal.NewFromString(r.FxRate); err != nil {
		return t, fmt.Errorf("trade %d fx: %w", r.ID, err)
	}
	return t, nil
}

func convertAll(rows []db.Trade) ([]Trade, error) {
	out := make([]Trade, 0, len(rows))
	for _, r := range rows {
		t, err := convert(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Create records a trade. The whole history is replayed with the new trade in
// place (so back-dated trades are checked too); selling more than held is
// rejected. The asset status then follows the position: first buy moves
// watchlist to holding, selling to zero moves holding to exited.
func (s *Service) Create(ctx context.Context, assetID int64, in CreateInput) (Trade, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return Trade{}, err
	}
	date := strings.TrimSpace(in.Date)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return Trade{}, validationf("date must be YYYY-MM-DD")
	}
	side := strings.ToLower(strings.TrimSpace(in.Side))
	if side != SideBuy && side != SideSell {
		return Trade{}, validationf("side must be buy or sell")
	}
	if !in.Quantity.IsPositive() {
		return Trade{}, validationf("quantity must be positive")
	}
	if !in.Price.IsPositive() {
		return Trade{}, validationf("price must be positive")
	}
	fees := decimal.Zero
	if in.Fees != nil {
		fees = in.Fees.Decimal
	}
	if fees.IsNegative() {
		return Trade{}, validationf("fees must not be negative")
	}
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	if cur == "" {
		cur = a.Currency
	}
	if cur != a.Currency {
		return Trade{}, validationf("currency must be %s for this asset", a.Currency)
	}
	fx := decimal.NewFromInt(1)
	if in.FXRate != nil {
		fx = in.FXRate.Decimal
	} else if cur == assets.CurrencyUSD {
		return Trade{}, validationf("fx_rate is required for USD trades")
	}
	if !fx.IsPositive() {
		return Trade{}, validationf("fx_rate must be positive")
	}
	if cur == assets.CurrencyBRL && !fx.Equal(decimal.NewFromInt(1)) {
		return Trade{}, validationf("fx_rate must be 1 for BRL trades")
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Trade{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := db.New(tx)
	row, err := q.CreateTrade(ctx, db.CreateTradeParams{
		AssetID: assetID, TradeDate: date, Side: side, Quantity: in.Quantity.String(),
		Price: in.Price.String(), Fees: fees.String(), Currency: cur, FxRate: fx.String(),
	})
	if err != nil {
		return Trade{}, fmt.Errorf("create trade: %w", err)
	}
	rows, err := q.ListTradesByAsset(ctx, assetID)
	if err != nil {
		return Trade{}, fmt.Errorf("list trades: %w", err)
	}
	all, err := convertAll(rows)
	if err != nil {
		return Trade{}, err
	}
	pos, err := replay(ctx, q, assetID, all)
	if err != nil {
		return Trade{}, err // tx rolled back
	}
	if err := tx.Commit(); err != nil {
		return Trade{}, fmt.Errorf("commit: %w", err)
	}
	if err := s.syncStatus(ctx, a, pos); err != nil {
		return Trade{}, err
	}
	return convert(row)
}

// Delete removes a trade, provided the remaining history stays valid.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := db.New(tx)
	row, err := q.GetTrade(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get trade: %w", err)
	}
	if err := q.DeleteTrade(ctx, id); err != nil {
		return fmt.Errorf("delete trade: %w", err)
	}
	rows, err := q.ListTradesByAsset(ctx, row.AssetID)
	if err != nil {
		return fmt.Errorf("list trades: %w", err)
	}
	all, err := convertAll(rows)
	if err != nil {
		return err
	}
	pos, err := replay(ctx, q, row.AssetID, all)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	a, err := s.assets.Get(ctx, row.AssetID)
	if err != nil {
		return err
	}
	return s.syncStatus(ctx, a, pos)
}

// syncStatus moves the asset along the lifecycle to match the position.
func (s *Service) syncStatus(ctx context.Context, a db.Asset, pos Position) error {
	switch {
	case pos.Quantity.IsPositive() && a.Status != assets.StatusHolding:
		if a.Status == assets.StatusExited {
			if _, err := s.assets.Transition(ctx, a.ID, assets.StatusWatchlist); err != nil {
				return err
			}
		}
		_, err := s.assets.Transition(ctx, a.ID, assets.StatusHolding)
		return err
	case pos.Quantity.IsZero() && a.Status == assets.StatusHolding:
		_, err := s.assets.Transition(ctx, a.ID, assets.StatusExited)
		return err
	}
	return nil
}

// events loads an asset's confirmed corporate actions.
func events(ctx context.Context, q *db.Queries, assetID int64) ([]Event, error) {
	rows, err := q.ConfirmedCorporateActions(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("corporate actions: %w", err)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		f, err := decimal.NewFromString(r.Factor)
		if err != nil {
			return nil, err
		}
		c, err := decimal.NewFromString(r.CostPerShare)
		if err != nil {
			return nil, err
		}
		out = append(out, Event{Date: r.ExDate, Kind: r.Kind, Factor: f, CostPerShare: c})
	}
	return out, nil
}

// replay replays an asset's trades with its confirmed corporate actions.
// Bonus shares of USD assets are converted at the FX of the last trade
// before the event (no rate store here).
func replay(ctx context.Context, q *db.Queries, assetID int64, ts []Trade) (Position, error) {
	ev, err := events(ctx, q, assetID)
	if err != nil {
		return Position{}, err
	}
	for i := range ev {
		for _, t := range ts {
			if t.Date < ev[i].Date {
				ev[i].FXRate = t.FXRate
			}
		}
	}
	return ReplayWithEvents(assetID, ts, ev)
}

// PositionAt replays an asset's trades and events up to and including date
// (empty: all), for dividends on a record date and year-end reports.
func (s *Service) PositionAt(ctx context.Context, assetID int64, date string) (Position, error) {
	ts, err := s.List(ctx, assetID)
	if err != nil {
		return Position{}, err
	}
	q := db.New(s.conn)
	if date == "" {
		return replay(ctx, q, assetID, ts)
	}
	var upTo []Trade
	for _, t := range ts {
		if t.Date <= date {
			upTo = append(upTo, t)
		}
	}
	ev, err := events(ctx, q, assetID)
	if err != nil {
		return Position{}, err
	}
	var evUpTo []Event
	for _, e := range ev {
		if e.Date <= date {
			for _, t := range upTo {
				if t.Date < e.Date {
					e.FXRate = t.FXRate
				}
			}
			evUpTo = append(evUpTo, e)
		}
	}
	return ReplayWithEvents(assetID, upTo, evUpTo)
}

// Revalidate replays an asset's history after a corporate action changes and
// syncs the lifecycle status. It fails when the history becomes invalid.
func (s *Service) Revalidate(ctx context.Context, assetID int64) error {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return err
	}
	pos, err := s.PositionAt(ctx, assetID, "")
	if err != nil {
		return err
	}
	return s.syncStatus(ctx, a, pos)
}

// List returns an asset's trades ordered by date.
func (s *Service) List(ctx context.Context, assetID int64) ([]Trade, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return nil, err
	}
	rows, err := db.New(s.conn).ListTradesByAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("list trades: %w", err)
	}
	return convertAll(rows)
}

// Position derives one asset's position.
func (s *Service) Position(ctx context.Context, assetID int64) (Position, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return Position{}, err
	}
	ts, err := s.List(ctx, assetID)
	if err != nil {
		return Position{}, err
	}
	p, err := replay(ctx, db.New(s.conn), assetID, ts)
	if err != nil {
		return Position{}, err
	}
	if p.Currency == "" {
		p.Currency = a.Currency
	}
	return p, nil
}

// Positions derives positions for every asset that has trades.
func (s *Service) Positions(ctx context.Context) ([]Position, error) {
	rows, err := db.New(s.conn).ListAllTrades(ctx)
	if err != nil {
		return nil, fmt.Errorf("list trades: %w", err)
	}
	all, err := convertAll(rows)
	if err != nil {
		return nil, err
	}
	out := []Position{}
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].AssetID == all[i].AssetID {
			j++
		}
		p, err := replay(ctx, db.New(s.conn), all[i].AssetID, all[i:j])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
		i = j
	}
	return out, nil
}
