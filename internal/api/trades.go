package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/trades"
)

// WithTrades mounts trade and position routes.
func WithTrades(svc *trades.Service) Option {
	return func(r chi.Router) {
		r.Post("/api/assets/{id}/trades", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Date     string         `json:"date"`
				Side     string         `json:"side"`
				Quantity money.Decimal  `json:"quantity"`
				Price    money.Decimal  `json:"price"`
				Fees     *money.Decimal `json:"fees"`
				Currency string         `json:"currency"`
				FXRate   *money.Decimal `json:"fx_rate"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body (decimals must be JSON strings)")
				return
			}
			t, err := svc.Create(req.Context(), id, trades.CreateInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, t)
		})
		r.Get("/api/assets/{id}/trades", func(w http.ResponseWriter, req *http.Request) {
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
		r.Get("/api/assets/{id}/position", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			p, err := svc.Position(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, p)
		})
		r.Get("/api/positions", func(w http.ResponseWriter, req *http.Request) {
			list, err := svc.Positions(req.Context())
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		})
		r.Delete("/api/trades/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
			if err != nil || id <= 0 {
				badRequest(w, "invalid trade id")
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
