package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

type assetJSON struct {
	ID        int64  `json:"id"`
	Ticker    string `json:"ticker"`
	Name      string `json:"name"`
	Market    string `json:"market"`
	Currency  string `json:"currency"`
	Kind      string `json:"kind"`
	Sleeve    string `json:"sleeve"`
	Status    string `json:"status"`
	Benchmark string `json:"benchmark"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toJSON(a db.Asset) assetJSON {
	return assetJSON{a.ID, a.Ticker, a.Name, a.Market, a.Currency, a.Kind, a.Sleeve, a.Status, a.Benchmark, a.CreatedAt, a.UpdatedAt}
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func assetID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "invalid asset id")
		return 0, false
	}
	return id, true
}

func mountAssets(r chi.Router, svc *assets.Service) {
	r.Route("/api/assets", func(r chi.Router) {
		r.Post("/", func(w http.ResponseWriter, req *http.Request) {
			var in struct {
				Ticker    string `json:"ticker"`
				Name      string `json:"name"`
				Market    string `json:"market"`
				Currency  string `json:"currency"`
				Kind      string `json:"kind"`
				Sleeve    string `json:"sleeve"`
				Benchmark string `json:"benchmark"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			a, err := svc.Create(req.Context(), assets.CreateInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, toJSON(a))
		})
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			q := req.URL.Query()
			list, err := svc.List(req.Context(), assets.Filter{
				Status: q.Get("status"), Sleeve: q.Get("sleeve"), Market: q.Get("market"),
			})
			if err != nil {
				writeError(w, err)
				return
			}
			out := make([]assetJSON, 0, len(list))
			for _, a := range list {
				out = append(out, toJSON(a))
			}
			writeJSON(w, http.StatusOK, out)
		})
		r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			a, err := svc.Get(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toJSON(a))
		})
		r.Patch("/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Name      *string `json:"name"`
				Sleeve    *string `json:"sleeve"`
				Benchmark *string `json:"benchmark"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			a, err := svc.Update(req.Context(), id, assets.UpdateInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toJSON(a))
		})
		r.Post("/{id}/status", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Status string `json:"status"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			a, err := svc.Transition(req.Context(), id, in.Status)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, toJSON(a))
		})
	})
}
