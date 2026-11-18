package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
)

// WithThesis mounts thesis, break condition and review routes.
func WithThesis(svc *thesis.Service) Option {
	return func(r chi.Router) {
		r.Put("/api/assets/{id}/thesis", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Text         string  `json:"text"`
				NextReviewOn *string `json:"next_review_on"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			t, created, err := svc.Put(req.Context(), id, thesis.PutInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			status := http.StatusOK
			if created {
				status = http.StatusCreated
			}
			writeJSON(w, status, t)
		})
		r.Get("/api/assets/{id}/thesis", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			t, err := svc.Get(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, t)
		})
		r.Delete("/api/assets/{id}/thesis", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			if err := svc.Delete(req.Context(), id); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		r.Post("/api/assets/{id}/thesis/status", func(w http.ResponseWriter, req *http.Request) {
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
			t, err := svc.SetStatus(req.Context(), id, in.Status)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, t)
		})
		r.Post("/api/assets/{id}/thesis/review", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				ReviewedOn   string  `json:"reviewed_on"`
				Note         string  `json:"note"`
				Status       string  `json:"status"`
				NextReviewOn *string `json:"next_review_on"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			rv, err := svc.Review(req.Context(), id, thesis.ReviewInput(in))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, rv)
		})
		r.Get("/api/assets/{id}/thesis/reviews", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			list, err := svc.Reviews(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		})
		r.Post("/api/assets/{id}/thesis/conditions", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Text string `json:"text"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			c, err := svc.AddCondition(req.Context(), id, in.Text)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, c)
		})
		r.Patch("/api/break-conditions/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "condition id")
			if !ok {
				return
			}
			var in struct {
				Text string `json:"text"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			c, err := svc.UpdateCondition(req.Context(), id, in.Text)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, c)
		})
		r.Delete("/api/break-conditions/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "condition id")
			if !ok {
				return
			}
			if err := svc.DeleteCondition(req.Context(), id); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
}
