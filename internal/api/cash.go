package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/cash"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

func pathID(w http.ResponseWriter, r *http.Request, param, label string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, param), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "invalid "+label)
		return 0, false
	}
	return id, true
}

// WithCash mounts the cash instrument routes.
func WithCash(svc *cash.Service) Option {
	return func(r chi.Router) {
		r.Route("/api/cash", func(r chi.Router) {
			r.Get("/summary", func(w http.ResponseWriter, req *http.Request) {
				s, err := svc.Summary(req.Context(), req.URL.Query().Get("as_of"))
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, s)
			})
			r.Post("/instruments", func(w http.ResponseWriter, req *http.Request) {
				var in struct {
					Kind   string         `json:"kind"`
					Name   string         `json:"name"`
					PctCDI *money.Decimal `json:"pct_cdi"`
				}
				if err := decode(req, &in); err != nil {
					badRequest(w, "invalid JSON body (decimals must be JSON strings)")
					return
				}
				i, err := svc.Create(req.Context(), cash.CreateInput(in))
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusCreated, i)
			})
			r.Get("/instruments", func(w http.ResponseWriter, req *http.Request) {
				list, err := svc.List(req.Context())
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, list)
			})
			r.Get("/instruments/{id}/value", func(w http.ResponseWriter, req *http.Request) {
				id, ok := pathID(w, req, "id", "instrument id")
				if !ok {
					return
				}
				v, err := svc.Value(req.Context(), id, req.URL.Query().Get("as_of"))
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, v)
			})
			r.Get("/instruments/{id}/movements", func(w http.ResponseWriter, req *http.Request) {
				id, ok := pathID(w, req, "id", "instrument id")
				if !ok {
					return
				}
				list, err := svc.Movements(req.Context(), id)
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, list)
			})
			r.Post("/instruments/{id}/movements", func(w http.ResponseWriter, req *http.Request) {
				id, ok := pathID(w, req, "id", "instrument id")
				if !ok {
					return
				}
				var in struct {
					Date     string         `json:"date"`
					Type     string         `json:"type"`
					Amount   money.Decimal  `json:"amount"`
					Quantity *money.Decimal `json:"quantity"`
				}
				if err := decode(req, &in); err != nil {
					badRequest(w, "invalid JSON body (decimals must be JSON strings)")
					return
				}
				m, err := svc.AddMovement(req.Context(), id, cash.MovementInput(in))
				if err != nil {
					writeError(w, err)
					return
				}
				writeJSON(w, http.StatusCreated, m)
			})
			r.Delete("/movements/{id}", func(w http.ResponseWriter, req *http.Request) {
				id, ok := pathID(w, req, "id", "movement id")
				if !ok {
					return
				}
				if err := svc.DeleteMovement(req.Context(), id); err != nil {
					writeError(w, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
		})
	}
}
