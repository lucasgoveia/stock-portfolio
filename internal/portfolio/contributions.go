package portfolio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// ErrNotFound is returned for a missing contribution or order.
var ErrNotFound = errors.New("portfolio: not found")

// ErrConflict is returned when a discarded plan is changed.
var ErrConflict = errors.New("portfolio: conflict")

// Contribution statuses. Status is derived from the orders unless discarded.
const (
	ContributionPlanned   = "planned"   // no order executed yet
	ContributionPartial   = "partial"   // some orders done or skipped
	ContributionExecuted  = "executed"  // every order done or skipped
	ContributionDiscarded = "discarded" // abandoned by the user
)

// Order execution statuses.
const (
	OrderOpen    = "open"
	OrderDone    = "done"
	OrderSkipped = "skipped"
)

// OrderState is the execution state of one plan order.
type OrderState struct {
	Seq     int64  `json:"seq"`
	Status  string `json:"status" enum:"open,done,skipped"`
	TradeID *int64 `json:"trade_id"`
}

// Contribution is a stored plan with its execution state.
type Contribution struct {
	ID          int64            `json:"id"`
	Amount      decimal.Decimal  `json:"amount"`
	Status      string           `json:"status" enum:"planned,partial,executed,discarded"`
	CreatedAt   string           `json:"created_at"`
	DiscardedAt *string          `json:"discarded_at"`
	Plan        ContributionPlan `json:"plan"`
	Orders      []OrderState     `json:"orders"`
}

// CreateContribution plans `amount` BRL and stores the plan with every order open.
func (s *Service) CreateContribution(ctx context.Context, amount decimal.Decimal) (Contribution, error) {
	pl, err := s.PlanContribution(ctx, amount)
	if err != nil {
		return Contribution{}, err
	}
	raw, err := json.Marshal(pl)
	if err != nil {
		return Contribution{}, err
	}
	row, err := s.q.CreateContribution(ctx, db.CreateContributionParams{Amount: amount.String(), Plan: string(raw)})
	if err != nil {
		return Contribution{}, err
	}
	for _, o := range pl.Orders {
		if err := s.q.CreateContributionOrder(ctx, db.CreateContributionOrderParams{ContributionID: row.ID, Seq: int64(o.Seq)}); err != nil {
			return Contribution{}, err
		}
	}
	return s.GetContribution(ctx, row.ID)
}

// GetContribution loads one stored plan.
func (s *Service) GetContribution(ctx context.Context, id int64) (Contribution, error) {
	row, err := s.q.GetContribution(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Contribution{}, fmt.Errorf("%w: contribution %d", ErrNotFound, id)
	}
	if err != nil {
		return Contribution{}, err
	}
	return s.load(ctx, row)
}

// ListContributions returns the most recent stored plans, newest first.
func (s *Service) ListContributions(ctx context.Context, limit int) ([]Contribution, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.q.ListContributions(ctx, int64(limit))
	if err != nil {
		return nil, err
	}
	out := make([]Contribution, 0, len(rows))
	for _, r := range rows {
		c, err := s.load(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// DiscardContribution marks a plan abandoned. Orders already done keep their trades.
func (s *Service) DiscardContribution(ctx context.Context, id int64) (Contribution, error) {
	c, err := s.GetContribution(ctx, id)
	if err != nil {
		return Contribution{}, err
	}
	if c.Status == ContributionDiscarded {
		return c, nil
	}
	if _, err := s.q.DiscardContribution(ctx, id); err != nil {
		return Contribution{}, err
	}
	return s.GetContribution(ctx, id)
}

// SetOrder records an order's execution: done (optionally linked to the
// trade that executed it), skipped, or reopened.
func (s *Service) SetOrder(ctx context.Context, id, seq int64, status string, tradeID *int64) (Contribution, error) {
	c, err := s.GetContribution(ctx, id)
	if err != nil {
		return Contribution{}, err
	}
	if c.Status == ContributionDiscarded {
		return Contribution{}, fmt.Errorf("%w: contribution %d is discarded", ErrConflict, id)
	}
	if status != OrderOpen && status != OrderDone && status != OrderSkipped {
		return Contribution{}, fmt.Errorf("%w: status must be open, done or skipped", ErrValidation)
	}
	var order *engine.Order
	for i := range c.Plan.Orders {
		if int64(c.Plan.Orders[i].Seq) == seq {
			order = &c.Plan.Orders[i]
		}
	}
	if order == nil {
		return Contribution{}, fmt.Errorf("%w: order %d of contribution %d", ErrNotFound, seq, id)
	}
	tid := sql.NullInt64{}
	if tradeID != nil {
		if status != OrderDone {
			return Contribution{}, fmt.Errorf("%w: trade_id only applies to done orders", ErrValidation)
		}
		t, err := s.q.GetTrade(ctx, *tradeID)
		if errors.Is(err, sql.ErrNoRows) {
			return Contribution{}, fmt.Errorf("%w: trade %d does not exist", ErrValidation, *tradeID)
		}
		if err != nil {
			return Contribution{}, err
		}
		if order.Kind != engine.OrderBuy || strconv.FormatInt(t.AssetID, 10) != order.AssetID || t.Side != "buy" {
			return Contribution{}, fmt.Errorf("%w: trade %d is not a buy of this order's asset", ErrValidation, *tradeID)
		}
		tid = sql.NullInt64{Int64: *tradeID, Valid: true}
	}
	if _, err := s.q.SetContributionOrder(ctx, db.SetContributionOrderParams{
		Status: status, TradeID: tid, ContributionID: id, Seq: seq,
	}); err != nil {
		return Contribution{}, err
	}
	return s.GetContribution(ctx, id)
}

func (s *Service) load(ctx context.Context, row db.Contribution) (Contribution, error) {
	amount, err := decimal.NewFromString(row.Amount)
	if err != nil {
		return Contribution{}, err
	}
	c := Contribution{ID: row.ID, Amount: amount, CreatedAt: row.CreatedAt, Orders: []OrderState{}}
	if err := json.Unmarshal([]byte(row.Plan), &c.Plan); err != nil {
		return Contribution{}, fmt.Errorf("contribution %d: bad plan JSON: %w", row.ID, err)
	}
	orders, err := s.q.ListContributionOrders(ctx, row.ID)
	if err != nil {
		return Contribution{}, err
	}
	open := 0
	for _, o := range orders {
		st := OrderState{Seq: o.Seq, Status: o.Status}
		if o.TradeID.Valid {
			v := o.TradeID.Int64
			st.TradeID = &v
		}
		if o.Status == OrderOpen {
			open++
		}
		c.Orders = append(c.Orders, st)
	}
	switch {
	case row.DiscardedAt.Valid:
		c.Status = ContributionDiscarded
		v := row.DiscardedAt.String
		c.DiscardedAt = &v
	case open == len(orders):
		c.Status = ContributionPlanned
		if len(orders) == 0 {
			c.Status = ContributionExecuted
		}
	case open == 0:
		c.Status = ContributionExecuted
	default:
		c.Status = ContributionPartial
	}
	return c, nil
}
