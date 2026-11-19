package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
)

func promptResult(desc, text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{Description: desc, Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}}}
}

// The analyst rules come from the analyst package, the same file the in-app
// analyst's system prompt is built from.
func rules() string { return analyst.Rules(analyst.ModeMCP) }

func (s *Server) registerPrompts() {
	s.srv.AddPrompt(&mcp.Prompt{
		Name: "analyze_asset", Title: "Analyze an asset",
		Description: "Analyze an asset the app holds (watchlist or position): thesis check, scenarios, initiatives and fair value range, saved as a draft for review.",
		Arguments:   []*mcp.PromptArgument{{Name: "ticker", Description: "Ticker of an asset in the app, for example WEGE3", Required: true}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		ticker := strings.ToUpper(strings.TrimSpace(req.Params.Arguments["ticker"]))
		if ticker == "" {
			return nil, fmt.Errorf("ticker is required")
		}
		return promptResult("Analyze "+ticker, fmt.Sprintf(`Analyze %[1]s for the investor and save the result as a draft in the Stock Portfolio app.

%[2]s

Workflow:
1. get_asset_context(ticker=%[1]q): the position, rating, thesis and break conditions, fair value versions, fundamentals, expected versus actual and notes.
2. list_filings(ticker=%[1]q) and read_filing for the latest annual report, the latest quarterly reports and the recent announcements. Read in pages (offset/length) and stop when you have what you need.
3. Use your own web search and fetch for what the app does not serve (investor relations pages, earnings-call transcripts, news, industry data). Cite every URL you rely on.
4. Test your scenarios with preview_scenario_dcf(asset=%[1]q, ...) and check what the market already prices in with reverse_dcf(asset=%[1]q, ...). Adjust until they are consistent.
5. Call submit_analysis with the structured result (asset=%[1]q and your model name). Tell the investor the draft id and where to review it: the AI analyst tab of %[1]s.`, ticker, rules())), nil
	})
	s.srv.AddPrompt(&mcp.Prompt{
		Name: "research_ticker", Title: "Research a new ticker",
		Description: "Research a ticker the app does not hold and propose it as a candidate for the watchlist, with a note and draft scenarios.",
		Arguments: []*mcp.PromptArgument{
			{Name: "ticker", Description: "Ticker to research", Required: true},
			{Name: "market", Description: "B3 or US (guessed from the ticker when omitted)"},
			{Name: "cnpj", Description: "B3 only: the company's CNPJ if you already know it"},
		},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		a := req.Params.Arguments
		ticker := strings.ToUpper(strings.TrimSpace(a["ticker"]))
		if ticker == "" {
			return nil, fmt.Errorf("ticker is required")
		}
		market := strings.ToUpper(strings.TrimSpace(a["market"]))
		if market == "" {
			market = inferMarket(ticker)
		}
		id := "For a US company the app resolves the SEC CIK itself (lookup_company)."
		if market == "B3" {
			id = "A B3 company needs its CNPJ, which you find on the web (the investor relations page or CVM). Pass it as cnpj to lookup_company: the app checks its digits and matches it to CVM's register of listed companies and returns the registered name; confirm that it is the right company before going on."
			if c := strings.TrimSpace(a["cnpj"]); c != "" {
				id += " The investor gave " + c + "."
			}
		}
		return promptResult("Research "+ticker, fmt.Sprintf(`Research %[1]s (%[2]s), a company that is not in the Stock Portfolio app, and propose it as a candidate.

%[3]s

%[4]s

Workflow:
1. lookup_company(ticker=%[1]q, market=%[2]q) to confirm the identity. The app limits how many new symbols can be looked up per day.
2. get_financials, list_filings and read_filing (the latest annual report and quarterlies) for the company, using the same ticker, market and cnpj. Use your own web search and fetch for the business model, competitors, market size, earnings calls and news. Cite every URL.
3. Draft bear/base/bull scenarios and test them with preview_scenario_dcf (pass base_revenue, net_debt and shares from the financials) and reverse_dcf.
4. Call submit_candidate with the ticker, market, CNPJ or CIK, a research note and, when you can support them, the scenarios, initiatives and fair value range (and your model name). The investor accepts or rejects it on the Watchlist page.`, ticker, market, id, rules())), nil
	})
	s.srv.AddPrompt(&mcp.Prompt{
		Name: "portfolio_review", Title: "Review the portfolio",
		Description: "Review the whole portfolio against the investor's rules and theses and suggest what to look at, without saving anything.",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return promptResult("Portfolio review", fmt.Sprintf(`Review the investor's portfolio in the Stock Portfolio app and report what deserves attention. Nothing needs to be saved.

%s

Workflow:
1. get_portfolio_snapshot and list_alerts: weights against targets and bands, cash against its target, overvalued positions, stale fair values, open alerts.
2. For the positions that stand out, get_asset_context to read the thesis, break conditions, fair value versions and expected versus actual; read recent filings with list_filings and read_filing when a break condition may be in play.
3. preview_contribution_plan with a plausible amount (ask the investor if unsure) to see where new money would go.
4. Report concisely: what changed, what breaks or strains a thesis, which assets need a fresh analysis (suggest the analyze_asset prompt for them), and any rule that looks out of place. Do not recommend trades the rules forbid. Web research is allowed; cite URLs.`, rules())), nil
	})
}
