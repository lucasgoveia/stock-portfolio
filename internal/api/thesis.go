package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
)

// ThesisPut is the body of PUT /api/assets/{id}/thesis.
type ThesisPut struct {
	Text         string  `json:"text"`
	NextReviewOn *string `json:"next_review_on,omitempty" doc:"YYYY-MM-DD"`
}

// ThesisReview is the body of POST /api/assets/{id}/thesis/review.
type ThesisReview struct {
	ReviewedOn   string  `json:"reviewed_on" required:"false" doc:"YYYY-MM-DD; defaults to today"`
	Note         string  `json:"note" doc:"What the review found"`
	Status       string  `json:"status" required:"false" doc:"New status; empty returns under_review to active"`
	NextReviewOn *string `json:"next_review_on,omitempty" doc:"YYYY-MM-DD"`
}

// ConditionText is the body of break condition writes.
type ConditionText struct {
	Text string `json:"text"`
}

type thesisOut struct {
	Status int
	Body   thesis.Thesis
}

// WithThesis mounts thesis, break condition and review routes.
func WithThesis(svc *thesis.Service) Option {
	return func(s *server) {
		const tag = "thesis"
		put := op(http.MethodPut, "/api/assets/{id}/thesis", "putThesis", "Create (201) or replace (200) the thesis", tag)
		put.Responses = map[string]*huma.Response{"201": {Description: "Created"}}
		huma.Register(s.api, put,
			func(ctx context.Context, in *BodyIn[ThesisPut]) (*thesisOut, error) {
				t, created, err := svc.Put(ctx, in.ID, thesis.PutInput(in.Body))
				if err != nil {
					return nil, apiError(err)
				}
				out := &thesisOut{Status: http.StatusOK, Body: t}
				if created {
					out.Status = http.StatusCreated
				}
				return out, nil
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/thesis", "getThesis", "Get the thesis with break conditions", tag),
			func(ctx context.Context, in *IDPath) (*Body[thesis.Thesis], error) {
				return ok(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/assets/{id}/thesis", "deleteThesis", "Delete the thesis", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.Delete(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/assets/{id}/thesis/status", "setThesisStatus", "Set thesis status", tag),
			func(ctx context.Context, in *BodyIn[StatusChange]) (*Body[thesis.Thesis], error) {
				return ok(svc.SetStatus(ctx, in.ID, in.Body.Status))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/thesis/review", "reviewThesis", "Record a thesis review", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[ThesisReview]) (*Body[thesis.Review], error) {
				return ok(svc.Review(ctx, in.ID, thesis.ReviewInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/thesis/reviews", "listThesisReviews", "Thesis review history", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]thesis.Review], error) {
				return ok(svc.Reviews(ctx, in.ID))
			})
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/thesis/conditions", "addBreakCondition", "Add a break condition", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[ConditionText]) (*Body[thesis.BreakCondition], error) {
				return ok(svc.AddCondition(ctx, in.ID, in.Body.Text))
			})
		huma.Register(s.api, op(http.MethodPatch, "/api/break-conditions/{id}", "updateBreakCondition", "Edit a break condition", tag),
			func(ctx context.Context, in *BodyIn[ConditionText]) (*Body[thesis.BreakCondition], error) {
				return ok(svc.UpdateCondition(ctx, in.ID, in.Body.Text))
			})
		huma.Register(s.api, opStatus(http.MethodDelete, "/api/break-conditions/{id}", "deleteBreakCondition", "Delete a break condition", tag, http.StatusNoContent),
			func(ctx context.Context, in *IDPath) (*struct{}, error) {
				return noContent(svc.DeleteCondition(ctx, in.ID))
			})
	}
}
