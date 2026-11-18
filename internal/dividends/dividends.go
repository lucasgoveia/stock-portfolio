// Package dividends records dividends and JCP (spec §8.5): detected payments
// wait for confirmation; confirmed ones credit net to a cash instrument and
// count as new money for the next contribution plan.
package dividends

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// Errors.
var (
	ErrValidation = errors.New("dividend validation error")
	ErrNotFound   = errors.New("dividend not found")
	ErrConflict   = errors.New("dividend conflict")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

const dateLayout = "2006-01-02"

// Statuses.
const (
	StatusDetected  = "detected"
	StatusConfirmed = "confirmed"
	StatusRejected  = "rejected"
)

// Dividend is a stored payment. Amounts are in the asset's currency.
type Dividend struct {
	ID             int64         `json:"id"`
	AssetID        int64         `json:"asset_id"`
	Kind           string        `json:"kind" enum:"dividend,jcp"`
	RecordDate     string        `json:"record_date"`
	PayDate        *string       `json:"pay_date"`
	Quantity       money.Decimal `json:"quantity"`
	PerShare       money.Decimal `json:"per_share"`
	Gross          money.Decimal `json:"gross"`
	Withholding    money.Decimal `json:"withholding"`
	Net            money.Decimal `json:"net"`
	NetBRL         money.Decimal `json:"net_brl"`
	Currency       string        `json:"currency"`
	FXRate         money.Decimal `json:"fx_rate"`
	Status         string        `json:"status" enum:"detected,confirmed,rejected"`
	Source         string        `json:"source"`
	MovementID     *int64        `json:"movement_id"`
	ContributionID *int64        `json:"contribution_id"`
	CreatedAt      string        `json:"created_at"`
}

// Service manages dividends.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	trades *trades.Service
	cash   *cash.Service
	params engine.DividendParams
	now    func() time.Time
}

// NewService builds a Service. A nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, t *trades.Service, c *cash.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, trades: t, cash: c, params: engine.DefaultDividendParams(), now: clock}
}

func dec(s string) money.Decimal {
	v, err := money.NewFromString(s)
	if err != nil {
		return money.New(decimal.Zero)
	}
	return v
}

func convert(r db.Dividend) Dividend {
	d := Dividend{
		ID: r.ID, AssetID: r.AssetID, Kind: r.Kind, RecordDate: r.RecordDate, Quantity: dec(r.Quantity),
		PerShare: dec(r.PerShare), Gross: dec(r.Gross), Withholding: dec(r.Withholding), Net: dec(r.Net),
		Currency: r.Currency, FXRate: dec(r.FxRate), Status: r.Status, Source: r.Source, CreatedAt: r.CreatedAt,
	}
	d.NetBRL = money.New(d.Net.Mul(d.FXRate.Decimal))
	if r.PayDate.Valid {
		v := r.PayDate.String
		d.PayDate = &v
	}
	if r.MovementID.Valid {
		v := r.MovementID.Int64
		d.MovementID = &v
	}
	if r.ContributionID.Valid {
		v := r.ContributionID.Int64
		d.ContributionID = &v
	}
	return d
}

func validDate(s string) bool {
	_, err := time.Parse(dateLayout, s)
	return err == nil
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// HeldAt is the quantity of an asset held at the end of date.
func (s *Service) HeldAt(ctx context.Context, assetID int64, date string) (decimal.Decimal, error) {
	ts, err := s.trades.List(ctx, assetID)
	if err != nil {
		return decimal.Zero, err
	}
	q := decimal.Zero
	for _, t := range ts {
		if t.Date > date {
			break
		}
		if t.Side == trades.SideBuy {
			q = q.Add(t.Quantity)
		} else {
			q = q.Sub(t.Quantity)
		}
	}
	return q, nil
}

// payerMonthBefore is the gross BRL dividends already confirmed from the same
// payer with a payment date in the same month.
func (s *Service) payerMonthBefore(ctx context.Context, assetID int64, payDate string) (decimal.Decimal, error) {
	rows, err := s.q.ListDividends(ctx, db.ListDividendsParams{Status: StatusConfirmed, AssetID: assetID})
	if err != nil {
		return decimal.Zero, err
	}
	sum := decimal.Zero
	for _, r := range rows {
		if r.Kind == engine.DividendCash && r.PayDate.Valid && len(payDate) >= 7 && strings.HasPrefix(r.PayDate.String, payDate[:7]) {
			sum = sum.Add(dec(r.Gross).Mul(dec(r.FxRate).Decimal))
		}
	}
	return sum, nil
}

// RecordInput records a received payment.
type RecordInput struct {
	Kind         string
	RecordDate   string
	PayDate      string
	Quantity     *money.Decimal // default: held at the record date
	PerShare     money.Decimal
	Withholding  *money.Decimal // default: the rule for the kind and currency
	FXRate       *money.Decimal // USD: BRL per USD on the payment date (required)
	InstrumentID int64          // cash instrument credited with the net amount
}

// Record stores a confirmed payment and credits net to the cash instrument.
func (s *Service) Record(ctx context.Context, assetID int64, in RecordInput) (Dividend, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return Dividend{}, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind != engine.DividendCash && kind != engine.DividendJCP {
		return Dividend{}, validationf("kind must be dividend or jcp")
	}
	if kind == engine.DividendJCP && a.Currency != "BRL" {
		return Dividend{}, validationf("jcp only exists for B3 assets")
	}
	if !validDate(in.RecordDate) || !validDate(in.PayDate) {
		return Dividend{}, validationf("record_date and pay_date must be YYYY-MM-DD")
	}
	qty := decimal.Zero
	if in.Quantity != nil {
		qty = in.Quantity.Decimal
	} else if qty, err = s.HeldAt(ctx, assetID, in.RecordDate); err != nil {
		return Dividend{}, err
	}
	if !qty.IsPositive() {
		return Dividend{}, validationf("no shares held on %s; enter the quantity", in.RecordDate)
	}
	if !in.PerShare.IsPositive() {
		return Dividend{}, validationf("per_share must be positive")
	}
	row, err := s.insert(ctx, a, db.CreateDividendParams{
		AssetID: assetID, Kind: kind, RecordDate: in.RecordDate, Quantity: qty.String(), PerShare: in.PerShare.String(),
		Currency: a.Currency, Status: StatusConfirmed, Source: "manual",
	}, in.PayDate, in.Withholding, in.FXRate, in.InstrumentID)
	if err != nil {
		return Dividend{}, err
	}
	return convert(row), nil
}

// insert computes amounts, credits cash and stores a confirmed payment.
func (s *Service) insert(ctx context.Context, a db.Asset, p db.CreateDividendParams, payDate string,
	withholding, fx *money.Decimal, instrumentID int64) (db.Dividend, error) {
	gross := dec(p.Quantity).Mul(dec(p.PerShare).Decimal).Round(2)
	fxRate, wh, err := s.amounts(ctx, a, p.Kind, payDate, gross, withholding, fx)
	if err != nil {
		return db.Dividend{}, err
	}
	net := gross.Sub(wh)
	if net.IsNegative() {
		return db.Dividend{}, validationf("withholding is larger than the gross amount")
	}
	mv, err := s.credit(ctx, a, instrumentID, payDate, net)
	if err != nil {
		return db.Dividend{}, err
	}
	p.PayDate, p.Gross, p.Withholding, p.Net, p.FxRate = nullStr(payDate), gross.String(), wh.String(), net.String(), fxRate.String()
	p.MovementID = sql.NullInt64{Int64: mv.ID, Valid: true}
	row, err := s.q.CreateDividend(ctx, p)
	if err != nil {
		_ = s.cash.DeleteMovement(ctx, mv.ID)
		return db.Dividend{}, err
	}
	return row, nil
}

func (s *Service) amounts(ctx context.Context, a db.Asset, kind, payDate string, gross decimal.Decimal,
	withholding, fx *money.Decimal) (decimal.Decimal, decimal.Decimal, error) {
	fxRate := decimal.NewFromInt(1)
	if a.Currency == "USD" {
		if fx == nil || !fx.IsPositive() {
			return fxRate, fxRate, validationf("fx_rate is required for USD dividends")
		}
		fxRate = fx.Decimal
	}
	if withholding != nil {
		if withholding.IsNegative() {
			return fxRate, fxRate, validationf("withholding must not be negative")
		}
		return fxRate, withholding.Decimal, nil
	}
	before, err := s.payerMonthBefore(ctx, a.ID, payDate)
	if err != nil {
		return fxRate, fxRate, err
	}
	return fxRate, s.params.Withholding(kind, a.Currency, gross, before), nil
}

// credit deposits net into a cash instrument of the asset's currency.
func (s *Service) credit(ctx context.Context, a db.Asset, instrumentID int64, date string, net decimal.Decimal) (cash.Movement, error) {
	list, err := s.cash.List(ctx)
	if err != nil {
		return cash.Movement{}, err
	}
	var ins *cash.Instrument
	for i := range list {
		if list[i].ID == instrumentID {
			ins = &list[i]
		}
	}
	if ins == nil {
		return cash.Movement{}, validationf("instrument_id must be a cash instrument")
	}
	if ins.Kind == cash.KindTesouroSelic || ins.Currency != a.Currency {
		return cash.Movement{}, validationf("credit %s dividends to a %s CDI or USD cash instrument, not %s", a.Currency, a.Currency, ins.Name)
	}
	if !net.IsPositive() {
		return cash.Movement{}, validationf("net amount must be positive")
	}
	return s.cash.AddMovement(ctx, instrumentID, cash.MovementInput{Date: date, Type: cash.MoveDeposit, Amount: money.New(net)})
}

// Detect proposes a payment announced by a data source, sized by the shares
// held at the record date. It returns false when nothing was held or the
// source already proposed it.
func (s *Service) Detect(ctx context.Context, assetID int64, kind, recordDate, payDate string, perShare decimal.Decimal, source string) (bool, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return false, err
	}
	qty, err := s.HeldAt(ctx, assetID, recordDate)
	if err != nil || !qty.IsPositive() {
		return false, err
	}
	gross := qty.Mul(perShare).Round(2)
	wh := s.params.Withholding(kind, a.Currency, gross, decimal.Zero)
	n, err := s.q.InsertDetectedDividend(ctx, db.InsertDetectedDividendParams{
		AssetID: assetID, Kind: kind, RecordDate: recordDate, PayDate: nullStr(payDate), Quantity: qty.String(),
		PerShare: perShare.String(), Gross: gross.String(), Withholding: wh.String(), Net: gross.Sub(wh).String(),
		Currency: a.Currency, FxRate: "1", Source: source,
	})
	return n > 0, err
}

// ConfirmInput confirms a detected payment.
type ConfirmInput struct {
	InstrumentID int64
	PayDate      string         // default: the detected payment date
	Withholding  *money.Decimal // default: the rule
	FXRate       *money.Decimal
}

// Confirm credits a detected payment to cash.
func (s *Service) Confirm(ctx context.Context, id int64, in ConfirmInput) (Dividend, error) {
	row, err := s.q.GetDividend(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Dividend{}, ErrNotFound
	}
	if err != nil {
		return Dividend{}, err
	}
	if row.Status != StatusDetected {
		return Dividend{}, fmt.Errorf("%w: dividend %d is %s", ErrConflict, id, row.Status)
	}
	a, err := s.assets.Get(ctx, row.AssetID)
	if err != nil {
		return Dividend{}, err
	}
	pay := in.PayDate
	if pay == "" && row.PayDate.Valid {
		pay = row.PayDate.String
	}
	if !validDate(pay) {
		return Dividend{}, validationf("pay_date must be YYYY-MM-DD")
	}
	gross := dec(row.Gross).Decimal
	fx, wh, err := s.amounts(ctx, a, row.Kind, pay, gross, in.Withholding, in.FXRate)
	if err != nil {
		return Dividend{}, err
	}
	net := gross.Sub(wh)
	mv, err := s.credit(ctx, a, in.InstrumentID, pay, net)
	if err != nil {
		return Dividend{}, err
	}
	if err := s.q.ConfirmDividend(ctx, db.ConfirmDividendParams{
		ID: id, PayDate: nullStr(pay), Withholding: wh.String(), Net: net.String(), FxRate: fx.String(),
		MovementID: sql.NullInt64{Int64: mv.ID, Valid: true},
	}); err != nil {
		_ = s.cash.DeleteMovement(ctx, mv.ID)
		return Dividend{}, err
	}
	return s.Get(ctx, id)
}

// Reject dismisses a detected payment.
func (s *Service) Reject(ctx context.Context, id int64) (Dividend, error) {
	n, err := s.q.RejectDividend(ctx, id)
	if err != nil {
		return Dividend{}, err
	}
	if n == 0 {
		if _, err := s.Get(ctx, id); err != nil {
			return Dividend{}, err
		}
		return Dividend{}, fmt.Errorf("%w: only detected dividends can be rejected", ErrConflict)
	}
	return s.Get(ctx, id)
}

// Get returns one payment.
func (s *Service) Get(ctx context.Context, id int64) (Dividend, error) {
	row, err := s.q.GetDividend(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Dividend{}, ErrNotFound
	}
	if err != nil {
		return Dividend{}, err
	}
	return convert(row), nil
}

// Filter narrows List.
type Filter struct {
	Status  string
	AssetID int64
}

// List returns payments, newest record date first.
func (s *Service) List(ctx context.Context, f Filter) ([]Dividend, error) {
	p := db.ListDividendsParams{}
	if f.Status != "" {
		p.Status = f.Status
	}
	if f.AssetID > 0 {
		p.AssetID = f.AssetID
	}
	rows, err := s.q.ListDividends(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]Dividend, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}
	return out, nil
}

// Delete removes a payment and its cash credit.
func (s *Service) Delete(ctx context.Context, id int64) error {
	d, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.MovementID != nil {
		if err := s.cash.DeleteMovement(ctx, *d.MovementID); err != nil && !errors.Is(err, cash.ErrNotFound) {
			return err
		}
	}
	_, err = s.q.DeleteDividend(ctx, id)
	return err
}

// Undeployed is the net BRL of confirmed payments not yet included in a
// contribution plan.
func (s *Service) Undeployed(ctx context.Context) (decimal.Decimal, error) {
	rows, err := s.q.UndeployedDividends(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(convert(r).NetBRL.Decimal)
	}
	return sum, nil
}

// MarkDeployed ties every undeployed payment to a contribution plan.
func (s *Service) MarkDeployed(ctx context.Context, contributionID int64) error {
	return s.q.MarkDividendsDeployed(ctx, sql.NullInt64{Int64: contributionID, Valid: true})
}

// TrailingNetBRL sums confirmed net BRL per asset paid in the 12 months up to asof.
func (s *Service) TrailingNetBRL(ctx context.Context, asof time.Time) (map[int64]decimal.Decimal, error) {
	list, err := s.List(ctx, Filter{Status: StatusConfirmed})
	if err != nil {
		return nil, err
	}
	from := asof.AddDate(-1, 0, 0).Format(dateLayout)
	to := asof.Format(dateLayout)
	out := map[int64]decimal.Decimal{}
	for _, d := range list {
		day := d.RecordDate
		if d.PayDate != nil {
			day = *d.PayDate
		}
		if day > from && day <= to {
			out[d.AssetID] = out[d.AssetID].Add(d.NetBRL.Decimal)
		}
	}
	return out, nil
}
