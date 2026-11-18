package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
)

// WithJobs mounts the daily job routes.
func WithJobs(r *jobs.Runner) Option {
	return func(s *server) {
		const tag = "jobs"
		huma.Register(s.api, op(http.MethodPost, "/api/jobs/daily/run", "runDailyJob", "Run the daily pipeline now", tag),
			func(ctx context.Context, _ *struct{}) (*Body[jobs.RunResult], error) {
				return ok(r.Run(ctx))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/jobs/daily/last", "lastDailyJob", "Last daily pipeline run", tag),
			func(ctx context.Context, _ *struct{}) (*Body[jobs.RunResult], error) {
				return ok(r.Last(ctx))
			})
	}
}
