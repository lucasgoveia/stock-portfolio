// Package cash tracks cash instruments (Tesouro Selic, CDI, USD cash), their
// movements and their valuation. See docs/spec.md sections 2 and 7.
package cash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Instrument kinds.
const (
	KindTesouroSelic = "tesouro_selic"
	KindCDI          = "cdi"
	KindUSDCash      = "usd_cash"
)

// Errors returned by Service.
var (
	ErrNotFound            = errors.New("cash: not found")
	ErrValidation          = errors.New("cash: validation error")
	ErrInsufficientBalance = errors.New("cash: insufficient balance")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const dateLayout = "2006-01-02"

// Instrument is a cash holding.
type Instrument struct {
	ID       int64           `json:"id"`
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	Currency string          `json:"currency"`
	PctCDI   decimal.Decimal `json:"pct_cdi"`
}

// Valuation is one instrument valued at a date.
type Valuation struct {
	Instrument
	// Quantity is title units (Tesouro Selic only).
	Quantity *decimal.Decimal `json:"quantity,omitempty"`
	// Balance is the value in the instrument's own currency.
	Balance  decimal.Decimal `json:"balance"`
	ValueBRL decimal.Decimal `json:"value_brl"`
	// RateDate is the reference date of the price/FX used, when one applies.
	RateDate *string `json:"rate_date,omitempty"`
}

// Summary is total cash at a date with the BRL/USD split.
type Summary struct {
	AsOf     string          `json:"as_of"`
	TotalBRL decimal.Decimal `json:"total_brl"`
	// BRL is Tesouro Selic + CDI instruments; USD is USD cash converted to BRL.
	BRL decimal.Decimal `json:"brl"`
	USD decimal.Decimal `json:"usd_in_brl"`
	// USDBalance is the raw USD amount.
	USDBalance decimal.Decimal `json:"usd_balance"`
	// USDShare is USD / total (0 when total is 0), 4 decimals.
	USDShare    decimal.Decimal `json:"usd_share"`
	Instruments []Valuation     `json:"instruments"`
}

// Service manages cash instruments.
type Service struct {
	conn  *sql.DB
	q     *db.Queries
	rates Rates
	now   func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn *sql.DB, rates Rates, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{conn: conn, q: db.New(conn), rates: rates, now: clock}
}

func toInstrument(r db.CashInstrument) (Instrument, error) {
	p, err := decimal.NewFromString(r.PctCdi)
	return Instrument{ID: r.ID, Kind: r.Kind, Name: r.Name, Currency: r.Currency, PctCDI: p}, err
}

func toMovement(r db.CashMovement) (Movement, error) {
	a, err := decimal.NewFromString(r.Amount)
	if err != nil {
		return Movement{}, err
	}
	q, err := decimal.NewFromString(r.Quantity)
	return Movement{ID: r.ID, Instrument: r.InstrumentID, Date: r.MoveDate, Type: r.Type, Amount: a, Quantity: q}, err
}

// CreateInput creates an instrument. PctCDI applies to kind cdi (default 100).
type CreateInput struct {
	Kind   string
	Name   string
	PctCDI *money.Decimal
}

// Create adds an instrument. Currency follows the kind.
func (s *Service) Create(ctx context.Context, in CreateInput) (Instrument, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	name := strings.TrimSpace(in.Name)
	cur := "BRL"
	switch kind {
	case KindTesouroSelic, KindCDI:
	case KindUSDCash:
		cur = "USD"
	default:
		return Instrument{}, validationf("kind must be tesouro_selic, cdi or usd_cash")
	}
	if name == "" {
		return Instrument{}, validationf("name is required")
	}
	pct := hundred
	if in.PctCDI != nil {
		if kind != KindCDI {
			return Instrument{}, validationf("pct_cdi only applies to cdi instruments")
		}
		pct = in.PctCDI.Decimal
	}
	if !pct.IsPositive() {
		return Instrument{}, validationf("pct_cdi must be positive")
	}
	r, err := s.q.CreateCashInstrument(ctx, db.CreateCashInstrumentParams{Kind: kind, Name: name, Currency: cur, PctCdi: pct.String()})
	if err != nil {
		return Instrument{}, fmt.Errorf("create instrument: %w", err)
	}
	return toInstrument(r)
}

// List returns all instruments.
func (s *Service) List(ctx context.Context) ([]Instrument, error) {
	rows, err := s.q.ListCashInstruments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instruments: %w", err)
	}
	out := make([]Instrument, 0, len(rows))
	for _, r := range rows {
		i, err := toInstrument(r)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

func (s *Service) get(ctx context.Context, id int64) (Instrument, error) {
	r, err := s.q.GetCashInstrument(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Instrument{}, ErrNotFound
	}
	if err != nil {
		return Instrument{}, fmt.Errorf("get instrument: %w", err)
	}
	return toInstrument(r)
}

// MovementInput is a ledger entry to add.
type MovementInput struct {
	Date     string
	Type     string
	Amount   money.Decimal
	Quantity *money.Decimal
}

// Movements lists an instrument's ledger.
func (s *Service) Movements(ctx context.Context, instrumentID int64) ([]Movement, error) {
	if _, err := s.get(ctx, instrumentID); err != nil {
		return nil, err
	}
	return listMovements(ctx, s.q, instrumentID)
}

func listMovements(ctx context.Context, q *db.Queries, instrumentID int64) ([]Movement, error) {
	rows, err := q.ListCashMovements(ctx, instrumentID)
	if err != nil {
		return nil, fmt.Errorf("list movements: %w", err)
	}
	out := make([]Movement, 0, len(rows))
	for _, r := range rows {
		m, err := toMovement(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// AddMovement validates and records a movement. USD cash and Tesouro Selic
// holdings may not go negative; CDI balances are not checked because they
// depend on accrual.
func (s *Service) AddMovement(ctx context.Context, instrumentID int64, in MovementInput) (Movement, error) {
	ins, err := s.get(ctx, instrumentID)
	if err != nil {
		return Movement{}, err
	}
	date := strings.TrimSpace(in.Date)
	if _, err := time.Parse(dateLayout, date); err != nil {
		return Movement{}, validationf("date must be YYYY-MM-DD")
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	qty := decimal.Zero
	switch typ {
	case MoveDeposit, MoveWithdraw:
		if !in.Amount.IsPositive() {
			return Movement{}, validationf("amount must be positive")
		}
	case MoveYieldAdjust:
		if in.Amount.IsZero() {
			return Movement{}, validationf("amount must not be zero")
		}
	case MoveBalance:
		if in.Amount.IsNegative() {
			return Movement{}, validationf("balance must not be negative")
		}
	default:
		return Movement{}, validationf("type must be deposit, withdraw, yield_adjust or balance")
	}
	if ins.Kind == KindTesouroSelic {
		if typ != MoveDeposit && typ != MoveWithdraw {
			return Movement{}, validationf("tesouro_selic only supports deposit and withdraw (with quantity)")
		}
		if in.Quantity == nil || !in.Quantity.IsPositive() {
			return Movement{}, validationf("quantity is required for tesouro_selic movements")
		}
		qty = in.Quantity.Decimal
	} else if in.Quantity != nil {
		return Movement{}, validationf("quantity only applies to tesouro_selic")
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return Movement{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := db.New(tx)
	row, err := q.CreateCashMovement(ctx, db.CreateCashMovementParams{
		InstrumentID: instrumentID, MoveDate: date, Type: typ, Amount: in.Amount.String(), Quantity: qty.String(),
	})
	if err != nil {
		return Movement{}, fmt.Errorf("create movement: %w", err)
	}
	if err := checkLedger(ctx, q, ins); err != nil {
		return Movement{}, err
	}
	if err := tx.Commit(); err != nil {
		return Movement{}, fmt.Errorf("commit: %w", err)
	}
	return toMovement(row)
}

// DeleteMovement removes a movement if the ledger stays valid.
func (s *Service) DeleteMovement(ctx context.Context, id int64) error {
	row, err := s.q.GetCashMovement(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get movement: %w", err)
	}
	ins, err := s.get(ctx, row.InstrumentID)
	if err != nil {
		return err
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := db.New(tx)
	if err := q.DeleteCashMovement(ctx, id); err != nil {
		return fmt.Errorf("delete movement: %w", err)
	}
	if err := checkLedger(ctx, q, ins); err != nil {
		return err
	}
	return tx.Commit()
}

func checkLedger(ctx context.Context, q *db.Queries, ins Instrument) error {
	ms, err := listMovements(ctx, q, ins.ID)
	if err != nil {
		return err
	}
	const far = "9999-12-31"
	switch ins.Kind {
	case KindUSDCash:
		_, err = SimpleBalance(ms, far)
	case KindTesouroSelic:
		_, err = TesouroQuantity(ms, far)
	}
	return err
}

// Summary values all instruments at asof (default today) and splits BRL/USD.
func (s *Service) Summary(ctx context.Context, asof string) (Summary, error) {
	if asof == "" {
		asof = s.now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, asof); err != nil {
		return Summary{}, validationf("as_of must be YYYY-MM-DD")
	}
	list, err := s.List(ctx)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{AsOf: asof, Instruments: []Valuation{}}
	for _, ins := range list {
		v, err := s.value(ctx, ins, asof)
		if err != nil {
			return Summary{}, fmt.Errorf("%s: %w", ins.Name, err)
		}
		sum.Instruments = append(sum.Instruments, v)
		if ins.Currency == "USD" {
			sum.USD = sum.USD.Add(v.ValueBRL)
			sum.USDBalance = sum.USDBalance.Add(v.Balance)
		} else {
			sum.BRL = sum.BRL.Add(v.ValueBRL)
		}
	}
	sum.TotalBRL = sum.BRL.Add(sum.USD)
	if sum.TotalBRL.IsPositive() {
		sum.USDShare = sum.USD.DivRound(sum.TotalBRL, 4)
	}
	return sum, nil
}

// Value values one instrument at asof.
func (s *Service) Value(ctx context.Context, id int64, asof string) (Valuation, error) {
	ins, err := s.get(ctx, id)
	if err != nil {
		return Valuation{}, err
	}
	if asof == "" {
		asof = s.now().Format(dateLayout)
	}
	if _, err := time.Parse(dateLayout, asof); err != nil {
		return Valuation{}, validationf("as_of must be YYYY-MM-DD")
	}
	return s.value(ctx, ins, asof)
}

func (s *Service) value(ctx context.Context, ins Instrument, asof string) (Valuation, error) {
	ms, err := s.Movements(ctx, ins.ID)
	if err != nil {
		return Valuation{}, err
	}
	v := Valuation{Instrument: ins}
	switch ins.Kind {
	case KindTesouroSelic:
		qty, err := TesouroQuantity(ms, asof)
		if err != nil {
			return v, err
		}
		v.Quantity = &qty
		if qty.IsZero() {
			return v, nil
		}
		q, err := s.rates.TesouroSelicPrice(ctx, asof)
		if err != nil {
			return v, err
		}
		v.Balance = qty.Mul(q.Value)
		v.ValueBRL = v.Balance
		v.RateDate = &q.Date
	case KindCDI:
		if len(ms) == 0 || ms[0].Date > asof {
			return v, nil
		}
		rates, err := s.rates.CDIRates(ctx, ms[0].Date, asof)
		if err != nil {
			return v, err
		}
		v.Balance = CDIBalance(ms, rates, ins.PctCDI, asof)
		v.ValueBRL = v.Balance
		if n := len(rates); n > 0 {
			v.RateDate = &rates[n-1].Date
		}
	case KindUSDCash:
		bal, err := SimpleBalance(ms, asof)
		if err != nil {
			return v, err
		}
		v.Balance = bal
		if bal.IsZero() {
			return v, nil
		}
		q, err := s.rates.USDBRL(ctx, asof)
		if err != nil {
			return v, err
		}
		v.ValueBRL = bal.Mul(q.Value)
		v.RateDate = &q.Date
	}
	return v, nil
}
