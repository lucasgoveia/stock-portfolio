package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
)

// fundamentalsIn is the path and the optional helper inputs. Rates, margins
// and yields are fractions (0.06 means 6%).
type fundamentalsIn struct {
	IDPath
	BazinYield     string `query:"bazin_yield" doc:"Required dividend yield for Bazin (default 0.06)"`
	TerminalMargin string `query:"terminal_margin" doc:"Reverse DCF terminal EBIT margin (default: latest margin)"`
	ExitMultiple   string `query:"exit_multiple" doc:"Reverse DCF exit EV/EBIT multiple (default 10)"`
	DiscountRate   string `query:"discount_rate" doc:"Reverse DCF discount rate (default 0.14 B3, 0.10 US)"`
}

type draftIn struct {
	IDPath
	Method         string `query:"method" doc:"bazin or graham"`
	BazinYield     string `query:"bazin_yield" doc:"Required dividend yield for Bazin (default 0.06)"`
	TerminalMargin string `query:"terminal_margin"`
	ExitMultiple   string `query:"exit_multiple"`
	DiscountRate   string `query:"discount_rate"`
}

func parseParams(bazin, margin, exit, rate string) (fundamentals.Params, error) {
	p, err := fundamentals.ParseParams(bazin, margin, exit, rate)
	if err != nil {
		return p, apiError(err)
	}
	return p, nil
}

// WithFundamentals mounts the fundamentals panel routes.
func WithFundamentals(svc *fundamentals.Service) Option {
	return func(s *server) {
		const tag = "fundamentals"
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/fundamentals", "getFundamentals", "Fundamentals panel: multiples, history, ranges and valuation helpers", tag),
			func(ctx context.Context, in *fundamentalsIn) (*Body[fundamentals.FundamentalsPanel], error) {
				p, err := parseParams(in.BazinYield, in.TerminalMargin, in.ExitMultiple, in.DiscountRate)
				if err != nil {
					return nil, err
				}
				return ok(svc.Get(ctx, in.ID, p))
			})
		huma.Register(s.api, op(http.MethodPost, "/api/assets/{id}/fundamentals/refresh", "refreshFundamentals", "Fetch statements from CVM or SEC now", tag),
			func(ctx context.Context, in *IDPath) (*Body[fundamentals.FundamentalsPanel], error) {
				return ok(svc.Refresh(ctx, in.ID))
			})
		huma.Register(s.api, op(http.MethodGet, "/api/assets/{id}/fundamentals/fair-value-draft", "fundamentalsFairValueDraft", "Fair value version proposed from Bazin or Graham", tag),
			func(ctx context.Context, in *draftIn) (*Body[fundamentals.FairValueDraft], error) {
				p, err := parseParams(in.BazinYield, in.TerminalMargin, in.ExitMultiple, in.DiscountRate)
				if err != nil {
					return nil, err
				}
				return ok(svc.Draft(ctx, in.ID, in.Method, p))
			})
	}
}
