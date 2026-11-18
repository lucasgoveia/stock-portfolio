package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/tax"
)

// B3Taxes is the body of GET /api/taxes/b3.
type B3Taxes struct {
	Params engine.TaxParams  `json:"params"`
	Months []engine.TaxMonth `json:"months"`
}

// WithTaxes mounts the tax report routes.
func WithTaxes(svc *tax.Service) Option {
	return func(s *server) {
		huma.Register(s.api, op(http.MethodGet, "/api/taxes/b3", "b3Taxes", "Monthly B3 tax: exemption, gains, carried losses and DARF", "taxes"),
			func(ctx context.Context, _ *struct{}) (*Body[B3Taxes], error) {
				months, err := svc.Months(ctx)
				return ok(B3Taxes{Params: svc.Params(), Months: months}, err)
			})
	}
}
