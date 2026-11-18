package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/lucasgoveia/stock-portfolio/internal/research"
)

// WithResearch mounts research note routes.
func WithResearch(svc *research.Service) Option {
	return func(r chi.Router) {
		r.Post("/api/assets/{id}/notes", func(w http.ResponseWriter, req *http.Request) {
			id, ok := assetID(w, req)
			if !ok {
				return
			}
			var in struct {
				Text                 string   `json:"text"`
				Sources              []string `json:"sources"`
				Author               string   `json:"author"`
				Opportunity          bool     `json:"opportunity"`
				OpportunityExpiresOn string   `json:"opportunity_expires_on"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			n, err := svc.Create(req.Context(), id, research.CreateInput{
				Body: in.Text, Sources: in.Sources, Author: in.Author,
				Opportunity: in.Opportunity, OpportunityExpiresOn: in.OpportunityExpiresOn,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, n)
		})
		r.Get("/api/assets/{id}/notes", func(w http.ResponseWriter, req *http.Request) {
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
		r.Get("/api/notes/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "note id")
			if !ok {
				return
			}
			n, err := svc.Get(req.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, n)
		})
		r.Patch("/api/notes/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "note id")
			if !ok {
				return
			}
			var in struct {
				Text                 *string   `json:"text"`
				Sources              *[]string `json:"sources"`
				Opportunity          *bool     `json:"opportunity"`
				OpportunityExpiresOn *string   `json:"opportunity_expires_on"`
			}
			if err := decode(req, &in); err != nil {
				badRequest(w, "invalid JSON body")
				return
			}
			n, err := svc.Update(req.Context(), id, research.UpdateInput{
				Body: in.Text, Sources: in.Sources, Opportunity: in.Opportunity, OpportunityExpiresOn: in.OpportunityExpiresOn,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, n)
		})
		r.Delete("/api/notes/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, ok := pathID(w, req, "id", "note id")
			if !ok {
				return
			}
			if err := svc.Delete(req.Context(), id); err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		r.Get("/api/opportunities", func(w http.ResponseWriter, req *http.Request) {
			q := req.URL.Query()
			var asset *int64
			if v := q.Get("asset_id"); v != "" {
				id, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					badRequest(w, "invalid asset_id")
					return
				}
				asset = &id
			}
			list, err := svc.ActiveOpportunities(req.Context(), q.Get("as_of"), asset)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, list)
		})
	}
}
