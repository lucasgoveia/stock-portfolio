package api

import (
	"context"
	"fmt"
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

type yearIn struct {
	Year int `path:"year" minimum:"2000" maximum:"2100"`
}

type csvOut struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

// WithTaxes mounts the tax report routes.
func WithTaxes(svc *tax.Service) Option {
	return func(s *server) {
		huma.Register(s.api, op(http.MethodGet, "/api/taxes/b3", "b3Taxes", "Monthly B3 tax: exemption, gains, carried losses and DARF", "taxes"),
			func(ctx context.Context, _ *struct{}) (*Body[B3Taxes], error) {
				months, err := svc.Months(ctx)
				return ok(B3Taxes{Params: svc.Params(), Months: months}, err)
			})
		huma.Register(s.api, op(http.MethodGet, "/api/taxes/annual/{year}", "annualTaxes", "Annual report: US 15% tax, income and positions at 31/12 for the DIRPF", "taxes"),
			func(ctx context.Context, in *yearIn) (*Body[tax.AnnualReport], error) {
				return ok(svc.Annual(ctx, in.Year))
			})
		csvOp := op(http.MethodGet, "/api/taxes/annual/{year}/csv", "annualTaxesCSV", "The annual report as CSV", "taxes")
		csvOp.Responses = map[string]*huma.Response{"200": {Description: "CSV file", Content: map[string]*huma.MediaType{"text/csv": {}}}}
		huma.Register(s.api, csvOp,
			func(ctx context.Context, in *yearIn) (*csvOut, error) {
				r, err := svc.Annual(ctx, in.Year)
				if err != nil {
					return nil, apiError(err)
				}
				b, err := r.CSV()
				if err != nil {
					return nil, apiError(err)
				}
				return &csvOut{ContentType: "text/csv; charset=utf-8",
					ContentDisposition: fmt.Sprintf(`attachment; filename="dirpf-%d.csv"`, in.Year), Body: b}, nil
			})
	}
}
