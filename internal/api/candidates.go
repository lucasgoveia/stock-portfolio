package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/candidates"
)

// PendingReview counts what awaits the user's review: MCP analyst drafts and
// pending candidates.
type PendingReview struct {
	MCPDrafts  int `json:"mcp_drafts" doc:"MCP analyst drafts not yet approved or discarded"`
	Candidates int `json:"candidates" doc:"Pending new-ticker candidates"`
	Total      int `json:"total"`
}

// WithCandidates mounts the candidate routes (new-ticker proposals submitted
// over MCP) and the count of drafts awaiting review.
func WithCandidates(svc *candidates.Service, an *analyst.Service) Option {
	return func(s *server) {
		const tag = "candidates"
		huma.Register(s.api, op(http.MethodGet, "/api/candidates", "listCandidates", "New-ticker candidates proposed over MCP, pending first", tag),
			func(ctx context.Context, _ *struct{}) (*Body[[]candidates.Candidate], error) {
				return ok(svc.List(ctx))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/candidates/{id}", "getCandidate", "One candidate", tag),
			func(ctx context.Context, in *IDPath) (*Body[candidates.Candidate], error) {
				return ok(svc.Get(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/candidates/{id}/accept", "acceptCandidate",
			"Create the watchlist asset and an MCP analyst draft from the candidate's note, scenarios and fair value", tag),
			func(ctx context.Context, in *IDPath) (*Body[candidates.Candidate], error) {
				return ok(svc.Accept(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/candidates/{id}/reject", "rejectCandidate", "Reject a candidate (the row is kept)", tag),
			func(ctx context.Context, in *IDPath) (*Body[candidates.Candidate], error) {
				return ok(svc.Reject(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/review/pending", "getPendingReview", "Count of MCP drafts and candidates awaiting review", tag),
			func(ctx context.Context, _ *struct{}) (*Body[PendingReview], error) {
				d, err := an.CountMCPDrafts(ctx)
				if err != nil {
					return nil, apiError(err)
				}
				c, err := svc.CountPending(ctx)
				if err != nil {
					return nil, apiError(err)
				}
				return &Body[PendingReview]{Body: PendingReview{MCPDrafts: d, Candidates: c, Total: d + c}}, nil
			})
	}
}
