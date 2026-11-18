package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

// WithValuation mounts fair value routes.
func WithValuation(svc *valuation.Service) Option {
	return func(r chi.Router) {
		r.Post("/api/valuation/dcf", func(w http.ResponseWriter, req *http.Request) {
			var in engine.DCFInput
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			res, err := valuation.Preview(in)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, res)
		})
		r.Post("/api/assets/{id}/fair-values", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Method        string           `json:"method"`
				ValuePerShare *money.Decimal   `json:"value_per_share"`
				DCF           *engine.DCFInput `json:"dcf"`
				Currency      string           `json:"currency"`
				AsOf          string           `json:"as_of"`
				Sources       []string         `json:"sources"`
				Note          string           `json:"note"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			fv, err := svc.Create(req.Context(), id, valuation.CreateInput{
				Method: in.Method, ValuePerShare: in.ValuePerShare, DCF: in.DCF, Currency: in.Currency,
				AsOf: in.AsOf, Sources: in.Sources, Note: in.Note,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, fv)
		})
		r.Get("/api/assets/{id}/fair-values", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			list, err := svc.List(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		})
		r.Get("/api/assets/{id}/fair-values/latest", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			fv, err := svc.Latest(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, fv)
		})
		r.Delete("/api/fair-values/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "fair value id")
			if !ok {
				return
			}
			if err := svc.Delete(req.Context(), id); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}
