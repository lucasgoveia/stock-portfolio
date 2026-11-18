package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/performance"
)

type performanceIn struct {
	From string `query:"from" doc:"YYYY-MM-DD; default one year before to"`
	To   string `query:"to" doc:"YYYY-MM-DD; default today"`
}

// BackfillResult is the body of POST /api/performance/backfill.
type BackfillResult struct {
	Days int `json:"days"`
}

// WithPerformance mounts the performance routes.
func WithPerformance(svc *performance.Service) Option {
	return func(s *server) {
		const tag = "performance"
		huma.Register(s.api, op(http.MethodGet, "/api/performance", "getPerformance", "Time-weighted returns per segment and vs CDI, IBOV and S&P 500 in BRL", tag),
			func(ctx context.Context, in *performanceIn) (*Body[performance.Report], error) {
				return ok(svc.Report(ctx, in.From, in.To))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/performance/backfill", "backfillPerformance", "Rebuild daily snapshots from trades, cash and stored prices", tag),
			func(ctx context.Context, _ *struct{}) (*Body[BackfillResult], error) {
				n, err := svc.Backfill(ctx, time.Time{}, time.Now())
				if err != nil {
					return nil, &Error{status: http.StatusConflict, Message: fmt.Sprintf("backfill stopped after %d days: %v", n, err)}
				}
				return &Body[BackfillResult]{Body: BackfillResult{Days: n}}, nil
			})
	}
}
