package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/jobs"
)

// WithJobs mounts the daily job routes.
func WithJobs(r *jobs.Runner) Option {
	return func(rt chi.Router) {
		rt.Post("/api/jobs/daily/run", func(w http.ResponseWriter, req *http.Request) {
			res, err := r.Run(req.Context())
			if errors.Is(err, jobs.ErrRunning) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		})
		rt.Get("/api/jobs/daily/last", func(w http.ResponseWriter, req *http.Request) {
			res, err := r.Last(req.Context())
			if errors.Is(err, jobs.ErrNoRuns) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		})
	}
}
