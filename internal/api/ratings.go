package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
)

func weightsJSON(w engine.Weights) ratings.WeightsInput {
	return ratings.WeightsInput{Volatility: money.New(w.Volatility), Understanding: money.New(w.Understanding),
		Moat: money.New(w.Moat), Predictability: money.New(w.Predictability), Horizon: money.New(w.Horizon)}
}

// WithRatings mounts rating history and factor weight routes.
func WithRatings(svc *ratings.Service) Option {
	return func(r chi.Router) {
		r.Post("/api/assets/{id}/ratings", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				RatedOn        string `json:"rated_on"`
				Volatility     int    `json:"volatility"`
				Understanding  int    `json:"understanding"`
				Moat           int    `json:"moat"`
				Predictability int    `json:"predictability"`
				Horizon        int    `json:"horizon"`
				Reason         string `json:"reason"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			rt, err := svc.Add(req.Context(), id, ratings.AddInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, rt)
		})
		r.Get("/api/assets/{id}/ratings", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			list, err := svc.History(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		})
		r.Get("/api/assets/{id}/rating", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			rt, err := svc.Latest(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, rt)
		})
		r.Get("/api/settings/rating-weights", func(w http.ResponseWriter, req *http.Request) {
			ws, err := svc.Weights(req.Context())
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, weightsJSON(ws))
		})
		r.Put("/api/settings/rating-weights", func(w http.ResponseWriter, req *http.Request) {
			var in ratings.WeightsInput
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body (decimals must be JSON strings)")
				return
			}
			ws, err := svc.SetWeights(req.Context(), in)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, weightsJSON(ws))
		})
	}
}
