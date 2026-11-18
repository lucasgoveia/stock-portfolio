package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/research"
)

// NoteCreate is the body of POST /api/assets/{id}/notes.
type NoteCreate struct {
	Text                 string   `json:"text"`
	Sources              []string `json:"sources,omitempty"`
	Author               string   `json:"author,omitempty" doc:"user (default) or ai_approved"`
	Opportunity          bool     `json:"opportunity,omitempty"`
	OpportunityExpiresOn string   `json:"opportunity_expires_on,omitempty" doc:"YYYY-MM-DD; required when opportunity is true"`
}

// NoteUpdate is the body of PATCH /api/notes/{id}.
type NoteUpdate struct {
	Text                 *string   `json:"text,omitempty"`
	Sources              *[]string `json:"sources,omitempty"`
	Opportunity          *bool     `json:"opportunity,omitempty"`
	OpportunityExpiresOn *string   `json:"opportunity_expires_on,omitempty"`
}

type opportunitiesIn struct {
	AsOf    string `query:"as_of" doc:"YYYY-MM-DD; defaults to today"`
	AssetID int64  `query:"asset_id" minimum:"0" doc:"Filter by asset; 0 or absent means any"`
}

// WithResearch mounts research note routes.
func WithResearch(svc *research.Service) Option {
	return func(s *server) {
		const tag = "research"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/notes", "createNote", "Add a research note", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[NoteCreate]) (*Body[research.Note], error) {
				b := in.Body
				return ok(svc.Create(ctx, in.ID, research.CreateInput{
					Body: b.Text, Sources: b.Sources, Author: b.Author,
					Opportunity: b.Opportunity, OpportunityExpiresOn: b.OpportunityExpiresOn,
				}))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/notes", "listNotes", "List an asset's notes", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]research.Note], error) {
				return ok(svc.List(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/notes/{id}", "getNote", "Get a note", tag),
			func(ctx context.Context, in *IDPath) (*Body[research.Note], error) {
				return ok(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPatch, "/api/notes/{id}", "updateNote", "Update a note", tag),
			func(ctx context.Context, in *BodyIn[NoteUpdate]) (*Body[research.Note], error) {
				b := in.Body
				return ok(svc.Update(ctx, in.ID, research.UpdateInput{
					Body: b.Text, Sources: b.Sources, Opportunity: b.Opportunity, OpportunityExpiresOn: b.OpportunityExpiresOn,
				}))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/notes/{id}", "deleteNote", "Delete a note", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/opportunities", "listOpportunities", "Active opportunity notes", tag),
			func(ctx context.Context, in *opportunitiesIn) (*Body[[]research.Note], error) {
				var asset *int64
				if in.AssetID > 0 {
					asset = &in.AssetID
				}
				return ok(svc.ActiveOpportunities(ctx, in.AsOf, asset))
			})
	}
}
