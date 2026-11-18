package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/portfolio"
)

// WithPortfolio mounts the snapshot, planner and trim routes.
func WithPortfolio(svc *portfolio.Service) Option {
	return func(r chi.Router) {
		r.Get("/api/portfolio", func(w http.ResponseWriter, req *http.Request) {
			s, err := svc.Snapshot(req.Context())
			if err != nil {
				portfolioError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, s)
		})
		r.Post("/api/plans/contribution", func(w http.ResponseWriter, req *http.Request) {
			var in struct {
				Amount money.Decimal `json:"amount"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			pl, err := svc.PlanContribution(req.Context(), in.Amount.Decimal)
			if err != nil {
				portfolioError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, pl)
		})
		r.Get("/api/trims", func(w http.ResponseWriter, req *http.Request) {
			t, err := svc.Trims(req.Context())
			if err != nil {
				portfolioError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, t)
		})
		r.Put("/api/portfolio/pending-us", func(w http.ResponseWriter, req *http.Request) {
			var in struct {
				Amount money.Decimal `json:"amount"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			if err := svc.SetPendingUS(req.Context(), in.Amount.Decimal); err != nil {
				portfolioError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}

func portfolioError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, portfolio.ErrValidation):
		badRequest(w, err.Error())
	case errors.Is(err, portfolio.ErrNoFX):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeError(w, err)
	}
}
