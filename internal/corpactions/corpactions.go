// Package corpactions manages corporate actions (spec §14): splits, reverse
// splits and bonificações detected from data sources or entered by hand,
// cross-checked against raw closes, confirmed by the user, and applied to
// positions by the trades replay.
package corpactions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// Errors.
var (
	ErrValidation = errors.New("corporate action validation error")
	ErrNotFound   = errors.New("corporate action not found")
	ErrConflict   = errors.New("corporate action conflict")
)

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Kinds and statuses.
const (
	KindSplit        = "split"
	KindReverseSplit = "reverse_split"
	KindBonus        = "bonus"

	StatusDetected  = "detected"
	StatusConfirmed = "confirmed"
	StatusRejected  = "rejected"
)

// Alert types raised by Evaluate.
const (
	AlertPending   = "corporate_action_pending"
	AlertPriceJump = "price_jump_unexplained"
)

// Heuristics.
var (
	// JumpRatio flags a day-over-day raw close ratio at or beyond 1.8x either way.
	JumpRatio = decimal.RequireFromString("1.8")
	// MatchTolerance is how far the price ratio may sit from the factor (15%).
	MatchTolerance = decimal.RequireFromString("0.15")
	// jumpWindow is how many days around a jump an event explains it.
	jumpWindow = 7
)

const dateLayout = "2006-01-02"

// Action is a stored corporate action.
type Action struct {
	ID           int64         `json:"id"`
	AssetID      int64         `json:"asset_id"`
	Kind         string        `json:"kind" enum:"split,reverse_split,bonus"`
	ExDate       string        `json:"ex_date"`
	Factor       money.Decimal `json:"factor"`
	CostPerShare money.Decimal `json:"cost_per_share"`
	Status       string        `json:"status" enum:"detected,confirmed,rejected"`
	Source       string        `json:"source"`
	// PriceRatio is raw close before / raw close on the ex-date, when known.
	PriceRatio *string `json:"price_ratio"`
	// RatioMatches is true when the price ratio is within 15% of the factor.
	RatioMatches *bool  `json:"ratio_matches"`
	Note         string `json:"note"`
	CreatedAt    string `json:"created_at"`
}

// Service manages corporate actions.
type Service struct {
	q      *db.Queries
	assets *assets.Service
	trades *trades.Service
	alerts *alerts.Service
	now    func() time.Time
}

// NewService builds a Service. alerts may be nil; a nil clock uses time.Now.
func NewService(conn db.DBTX, a *assets.Service, t *trades.Service, al *alerts.Service, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{q: db.New(conn), assets: a, trades: t, alerts: al, now: clock}
}

func convert(r db.CorporateAction) Action {
	f, _ := money.NewFromString(r.Factor)
	c, _ := money.NewFromString(r.CostPerShare)
	a := Action{ID: r.ID, AssetID: r.AssetID, Kind: r.Kind, ExDate: r.ExDate, Factor: f, CostPerShare: c,
		Status: r.Status, Source: r.Source, Note: r.Note, CreatedAt: r.CreatedAt}
	if r.PriceRatio.Valid {
		v := r.PriceRatio.String
		a.PriceRatio = &v
		if ratio, err := decimal.NewFromString(v); err == nil {
			m := RatioMatches(ratio, f.Decimal)
			a.RatioMatches = &m
		}
	}
	return a
}

// RatioMatches reports whether before/after closes agree with a factor.
func RatioMatches(ratio, factor decimal.Decimal) bool {
	if !factor.IsPositive() {
		return false
	}
	return ratio.Div(factor).Sub(decimal.NewFromInt(1)).Abs().LessThanOrEqual(MatchTolerance)
}

// Input is a manual corporate action.
type Input struct {
	Kind         string
	ExDate       string
	Factor       money.Decimal
	CostPerShare *money.Decimal // bonus only, required
	Note         string
}

func validate(kind string, factor decimal.Decimal, exDate string) error {
	switch kind {
	case KindSplit:
		if !factor.GreaterThan(decimal.NewFromInt(1)) {
			return validationf("a split multiplies shares: factor must be above 1 (2 for 2-for-1)")
		}
	case KindReverseSplit:
		if !factor.IsPositive() || !factor.LessThan(decimal.NewFromInt(1)) {
			return validationf("a reverse split divides shares: factor must be between 0 and 1 (0.1 for 1-for-10)")
		}
	case KindBonus:
		if !factor.GreaterThan(decimal.NewFromInt(1)) {
			return validationf("a bonificação adds shares: factor must be above 1 (1.1 for 10%%)")
		}
	default:
		return validationf("kind must be split, reverse_split or bonus")
	}
	if _, err := time.Parse(dateLayout, exDate); err != nil {
		return validationf("ex_date must be YYYY-MM-DD")
	}
	return nil
}

// Record stores a confirmed manual action and applies it.
func (s *Service) Record(ctx context.Context, assetID int64, in Input) (Action, error) {
	if _, err := s.assets.Get(ctx, assetID); err != nil {
		return Action{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if err := validate(kind, in.Factor.Decimal, in.ExDate); err != nil {
		return Action{}, err
	}
	cost := decimal.Zero
	if kind == KindBonus {
		if in.CostPerShare == nil || in.CostPerShare.IsNegative() {
			return Action{}, validationf("a bonificação needs the cost per new share declared by the company")
		}
		cost = in.CostPerShare.Decimal
	}
	ratio, _ := s.priceRatio(ctx, assetID, in.ExDate)
	row, err := s.q.CreateCorporateAction(ctx, db.CreateCorporateActionParams{
		AssetID: assetID, Kind: kind, ExDate: in.ExDate, Factor: in.Factor.String(), CostPerShare: cost.String(),
		Status: StatusConfirmed, Source: "manual", PriceRatio: ratio, Note: in.Note,
	})
	if err != nil {
		return Action{}, err
	}
	if err := s.trades.Revalidate(ctx, assetID); err != nil {
		_, _ = s.q.DeleteCorporateAction(ctx, row.ID)
		return Action{}, fmt.Errorf("%w: the trade history does not hold with this event: %v", ErrValidation, err)
	}
	return convert(row), nil
}

// Detect queues an action announced by a data source with its price check.
func (s *Service) Detect(ctx context.Context, assetID int64, kind, exDate string, factor decimal.Decimal, source, note string) (bool, error) {
	if err := validate(kind, factor, exDate); err != nil {
		return false, err
	}
	ratio, err := s.priceRatio(ctx, assetID, exDate)
	if err != nil {
		return false, err
	}
	n, err := s.q.InsertDetectedCorporateAction(ctx, db.InsertDetectedCorporateActionParams{
		AssetID: assetID, Kind: kind, ExDate: exDate, Factor: factor.String(), CostPerShare: "0",
		Source: source, PriceRatio: ratio, Note: note,
	})
	return n > 0, err
}

// Confirm applies a detected action. Bonus actions need the declared cost.
func (s *Service) Confirm(ctx context.Context, id int64, costPerShare *money.Decimal) (Action, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return Action{}, err
	}
	if a.Status != StatusDetected {
		return Action{}, fmt.Errorf("%w: action %d is %s", ErrConflict, id, a.Status)
	}
	cost := decimal.Zero
	if a.Kind == KindBonus {
		if costPerShare == nil || costPerShare.IsNegative() {
			return Action{}, validationf("enter the cost per new share declared by the company")
		}
		cost = costPerShare.Decimal
	}
	if _, err := s.q.SetCorporateActionStatus(ctx, db.SetCorporateActionStatusParams{
		Status: StatusConfirmed, CostPerShare: cost.String(), ID: id,
	}); err != nil {
		return Action{}, err
	}
	if err := s.trades.Revalidate(ctx, a.AssetID); err != nil {
		_ = s.q.ForceCorporateActionStatus(ctx, db.ForceCorporateActionStatusParams{Status: StatusDetected, ID: id})
		return Action{}, fmt.Errorf("%w: the trade history does not hold with this event: %v", ErrValidation, err)
	}
	return s.Get(ctx, id)
}

// Reject dismisses a detected action.
func (s *Service) Reject(ctx context.Context, id int64) (Action, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return Action{}, err
	}
	if a.Status != StatusDetected {
		return Action{}, fmt.Errorf("%w: only detected actions can be rejected", ErrConflict)
	}
	if _, err := s.q.SetCorporateActionStatus(ctx, db.SetCorporateActionStatusParams{
		Status: StatusRejected, CostPerShare: a.CostPerShare.String(), ID: id,
	}); err != nil {
		return Action{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes an action; positions follow on the next replay. It is
// refused when later sales need the shares the action created.
func (s *Service) Delete(ctx context.Context, id int64) error {
	row, err := s.q.GetCorporateAction(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.Status == StatusConfirmed {
		// Unapply first so a history that needs the event is caught.
		if err := s.q.ForceCorporateActionStatus(ctx, db.ForceCorporateActionStatusParams{Status: StatusRejected, ID: id}); err != nil {
			return err
		}
		if err := s.trades.Revalidate(ctx, row.AssetID); err != nil {
			_ = s.q.ForceCorporateActionStatus(ctx, db.ForceCorporateActionStatusParams{Status: StatusConfirmed, ID: id})
			return fmt.Errorf("%w: later sales need the shares from this event: %v", ErrConflict, err)
		}
	}
	_, err = s.q.DeleteCorporateAction(ctx, id)
	return err
}

// Get returns one action.
func (s *Service) Get(ctx context.Context, id int64) (Action, error) {
	row, err := s.q.GetCorporateAction(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Action{}, ErrNotFound
	}
	if err != nil {
		return Action{}, err
	}
	return convert(row), nil
}

// List returns actions, newest ex-date first.
func (s *Service) List(ctx context.Context, status string, assetID int64) ([]Action, error) {
	p := db.ListCorporateActionsParams{}
	if status != "" {
		p.Status = status
	}
	if assetID > 0 {
		p.AssetID = assetID
	}
	rows, err := s.q.ListCorporateActions(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]Action, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}
	return out, nil
}

// rawSource is where unadjusted closes come from per market.
func rawSource(market string) string {
	if market == assets.MarketUS {
		return "tiingo"
	}
	return "cotahist"
}

// priceRatio is the last raw close before exDate over the first on or after it.
func (s *Service) priceRatio(ctx context.Context, assetID int64, exDate string) (sql.NullString, error) {
	a, err := s.assets.Get(ctx, assetID)
	if err != nil {
		return sql.NullString{}, err
	}
	d, err := time.Parse(dateLayout, exDate)
	if err != nil {
		return sql.NullString{}, nil
	}
	rows, err := s.q.AssetPriceHistory(ctx, db.AssetPriceHistoryParams{
		AssetID: assetID, Source: rawSource(a.Market),
		FromDate: d.AddDate(0, 0, -10).Format(dateLayout), ToDate: d.AddDate(0, 0, 10).Format(dateLayout),
	})
	if err != nil {
		return sql.NullString{}, err
	}
	var before, after *decimal.Decimal
	for _, r := range rows {
		v, err := decimal.NewFromString(r.Close)
		if err != nil || !v.IsPositive() {
			continue
		}
		if r.RefDate < exDate {
			before = &v
		} else if after == nil {
			after = &v
		}
	}
	if before == nil || after == nil {
		return sql.NullString{}, nil
	}
	return sql.NullString{String: before.Div(*after).Round(4).String(), Valid: true}, nil
}

// Jump is a day-over-day raw close move beyond JumpRatio.
type Jump struct {
	Date   string          `json:"date"`
	Before decimal.Decimal `json:"before"`
	After  decimal.Decimal `json:"after"`
	Ratio  decimal.Decimal `json:"ratio"` // before / after
}

// FindJumps scans raw closes (sorted by date) for moves of JumpRatio or more.
func FindJumps(dates []string, closes []decimal.Decimal) []Jump {
	var out []Jump
	inv := decimal.NewFromInt(1).Div(JumpRatio)
	for i := 1; i < len(closes) && i < len(dates); i++ {
		if !closes[i].IsPositive() || !closes[i-1].IsPositive() {
			continue
		}
		r := closes[i-1].Div(closes[i])
		if r.GreaterThanOrEqual(JumpRatio) || r.LessThanOrEqual(inv) {
			out = append(out, Jump{Date: dates[i], Before: closes[i-1], After: closes[i], Ratio: r.Round(4)})
		}
	}
	return out
}

// Evaluate raises corporate_action_pending per asset with detected actions
// and price_jump_unexplained for recent raw-close jumps no action explains,
// then resolves alerts that no longer apply.
func (s *Service) Evaluate(ctx context.Context) error {
	if s.alerts == nil {
		return nil
	}
	actions, err := s.List(ctx, "", 0)
	if err != nil {
		return err
	}
	pendingKeys, jumpKeys := []string{}, []string{}
	byAsset := map[int64][]Action{}
	for _, a := range actions {
		byAsset[a.AssetID] = append(byAsset[a.AssetID], a)
	}
	held, err := s.assets.List(ctx, assets.Filter{Status: assets.StatusHolding})
	if err != nil {
		return err
	}
	ticker := map[int64]string{}
	for _, h := range held {
		ticker[h.ID] = h.Ticker
	}
	for id, list := range byAsset {
		n := 0
		for _, a := range list {
			if a.Status == StatusDetected {
				n++
			}
		}
		if n == 0 {
			continue
		}
		aid := id
		if _, err := s.alerts.Upsert(ctx, AlertPending, &aid, "", map[string]any{"ticker": ticker[id], "count": n}); err != nil {
			return err
		}
		pendingKeys = append(pendingKeys, alerts.Key(AlertPending, &aid, ""))
	}
	today := s.now()
	from := today.AddDate(0, 0, -30).Format(dateLayout)
	for _, h := range held {
		rows, err := s.q.AssetPriceHistory(ctx, db.AssetPriceHistoryParams{
			AssetID: h.ID, Source: rawSource(h.Market), FromDate: from, ToDate: today.Format(dateLayout),
		})
		if err != nil {
			return err
		}
		dates := make([]string, 0, len(rows))
		closes := make([]decimal.Decimal, 0, len(rows))
		for _, r := range rows {
			v, err := decimal.NewFromString(r.Close)
			if err != nil {
				continue
			}
			dates, closes = append(dates, r.RefDate), append(closes, v)
		}
		for _, j := range FindJumps(dates, closes) {
			if explained(byAsset[h.ID], j.Date) {
				continue
			}
			aid := h.ID
			if _, err := s.alerts.Upsert(ctx, AlertPriceJump, &aid, j.Date, map[string]any{
				"ticker": h.Ticker, "date": j.Date, "before": j.Before, "after": j.After, "ratio": j.Ratio,
			}); err != nil {
				return err
			}
			jumpKeys = append(jumpKeys, alerts.Key(AlertPriceJump, &aid, j.Date))
		}
	}
	if _, err := s.alerts.ResolveMissing(ctx, AlertPending, pendingKeys); err != nil {
		return err
	}
	_, err = s.alerts.ResolveMissing(ctx, AlertPriceJump, jumpKeys)
	return err
}

// explained reports whether a non-rejected action sits within jumpWindow days of date.
func explained(list []Action, date string) bool {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return false
	}
	for _, a := range list {
		if a.Status == StatusRejected {
			continue
		}
		ex, err := time.Parse(dateLayout, a.ExDate)
		if err != nil {
			continue
		}
		if diff := d.Sub(ex).Hours() / 24; diff >= -float64(jumpWindow) && diff <= float64(jumpWindow) {
			return true
		}
	}
	return false
}

// NextBusinessDay is the first Monday-to-Friday after d (holidays ignored):
// brapi gives the last day with the right, the ex-date is the next session.
func NextBusinessDay(d time.Time) time.Time {
	n := d.AddDate(0, 0, 1)
	for n.Weekday() == time.Saturday || n.Weekday() == time.Sunday {
		n = n.AddDate(0, 0, 1)
	}
	return n
}

// KindFromLabel maps brapi stockDividends labels to kinds.
func KindFromLabel(label string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "DESDOBRAMENTO":
		return KindSplit, true
	case "GRUPAMENTO":
		return KindReverseSplit, true
	case "BONIFICACAO", "BONIFICAÇÃO":
		return KindBonus, true
	}
	return "", false
}
