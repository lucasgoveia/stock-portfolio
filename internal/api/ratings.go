package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/ratings"
)

func weightsJSON(w engine.Weights, err error) (*Body[ratings.WeightsInput], error) {
	if err != nil {
		return nil, apiError(err)
	}
	return &Body[ratings.WeightsInput]{Body: ratings.WeightsInput{Volatility: money.New(w.Volatility), Understanding: money.New(w.Understanding),
		Moat: money.New(w.Moat), Predictability: money.New(w.Predictability), Horizon: money.New(w.Horizon)}}, nil
}

// RatingCreate is the body of POST /api/assets/{id}/ratings. Factors are 1-5.
type RatingCreate struct {
	RatedOn        string `json:"rated_on" required:"false" doc:"YYYY-MM-DD; defaults to today"`
	Volatility     int    `json:"volatility" required:"false" doc:"0 or absent derives it from beta"`
	Understanding  int    `json:"understanding"`
	Moat           int    `json:"moat"`
	Predictability int    `json:"predictability"`
	Horizon        int    `json:"horizon"`
	Reason         string `json:"reason" required:"false"`
}

// WithRatings mounts rating history and factor weight routes.
func WithRatings(svc *ratings.Service) Option {
	return func(s *server) {
		const tag = "ratings"
		huma.Register(s.api, opStatus(http.MethodPost, "/api/assets/{id}/ratings", "addRating", "Append a rating", tag, http.StatusCreated),
			func(ctx context.Context, in *BodyIn[RatingCreate]) (*Body[ratings.Rating], error) {
				return ok(svc.Add(ctx, in.ID, ratings.AddInput(in.Body)))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/beta", "getBeta", "Latest computed beta", tag),
			func(ctx context.Context, in *IDPath) (*Body[ratings.Beta], error) {
				return ok(svc.GetBeta(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/ratings", "ratingHistory", "Rating history, newest first", tag),
			func(ctx context.Context, in *IDPath) (*Body[[]ratings.Rating], error) {
				return ok(svc.History(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/rating", "latestRating", "Current rating", tag),
			func(ctx context.Context, in *IDPath) (*Body[ratings.Rating], error) {
				return ok(svc.Latest(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/settings/rating-weights", "getRatingWeights", "Factor weights", tag),
			func(ctx context.Context, _ *struct{}) (*Body[ratings.WeightsInput], error) {
				return weightsJSON(svc.Weights(ctx))
			})
		huma.Register(s.api, op(http.MethodPut, "/api/settings/rating-weights", "setRatingWeights", "Replace factor weights", tag),
			func(ctx context.Context, in *Body[ratings.WeightsInput]) (*Body[ratings.WeightsInput], error) {
				return weightsJSON(svc.SetWeights(ctx, in.Body))
			})
	}
}
