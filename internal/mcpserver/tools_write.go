package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/analyst"
	"github.com/lucasgoveia/stock-portfolio/internal/candidates"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func object(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// outProps returns the properties of the analyst output schema.
func outProps() map[string]any {
	return analyst.OutputSchema()["properties"].(map[string]any)
}

func dcfScenariosSchema() map[string]any {
	p := outProps()["scenarios"].(map[string]any)
	return p
}

// decode parses tool arguments strictly: unknown fields are an error so a
// misnamed field is not silently dropped.
func decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func (s *Server) registerCompute() {
	dcfProps := map[string]any{
		"asset":        strProp("Optional ticker of an asset in the app: base_revenue, net_debt and shares default to its stored fundamentals"),
		"base_revenue": numProp("Latest annual revenue (year 0), in the asset's currency"),
		"net_debt":     numProp("Net debt, subtracted for ebit_ev scenarios (default 0)"),
		"shares":       numProp("Shares outstanding"),
		"scenarios":    dcfScenariosSchema(),
		"initiatives":  outProps()["initiatives"],
	}
	s.srv.AddTool(&mcp.Tool{
		Name:        "preview_scenario_dcf",
		Description: "Compute a scenario DCF (bear, base, bull with probabilities, optional new-business initiatives) and return the per-scenario and probability-weighted value per share. Nothing is saved. Same engine and validation as the app's fair value versions.",
		InputSchema: object(dcfProps, "scenarios"), Annotations: readOnly("Preview scenario DCF"),
	}, raw(s.previewDCF))
	rev := map[string]any{
		"asset":           strProp("Optional ticker of an asset in the app: price, base_revenue, net_debt and shares default to its stored values"),
		"price":           numProp("Current price per share"),
		"base_revenue":    numProp("Latest annual revenue"),
		"net_debt":        numProp("Net debt (default 0)"),
		"shares":          numProp("Shares outstanding"),
		"terminal_margin": numProp("Margin on year-5 revenue, fraction in (0, 1]"),
		"exit_multiple":   numProp("Multiple applied to year-5 earnings on the basis"),
		"discount_rate":   numProp("Annual discount rate as a fraction; start from cost_of_equity in get_asset_context (CAPM)"),
		"basis":           map[string]any{"type": "string", "enum": []string{engine.BasisEBITEV, engine.BasisNetPE}, "description": "ebit_ev (default) or net_pe"},
		"start_margin":    numProp("Margin today on the same basis"),
		"tax_rate":        numProp("Tax rate fraction for ebit_ev"),
		"cash_conversion": numProp("Fraction of earnings returned to shareholders each year"),
		"initiatives":     outProps()["initiatives"],
		"scenario":        map[string]any{"type": "string", "enum": []string{"bear", "base", "bull"}, "description": "Which initiative success probability to use (default base)"},
	}
	s.srv.AddTool(&mcp.Tool{
		Name:        "reverse_dcf",
		Description: "Solve the constant yearly revenue growth over 5 years that the current price implies, given a terminal margin, exit multiple and discount rate. Nothing is saved.",
		InputSchema: object(rev, "terminal_margin", "exit_multiple", "discount_rate"), Annotations: readOnly("Reverse DCF"),
	}, raw(s.reverseDCF))
}

// baseFor returns the DCF base figures of an asset in the app.
func (s *Server) baseFor(ctx context.Context, ticker string) (*analyst.DCFBase, *decimal.Decimal, error) {
	a, err := s.requireAsset(ctx, ticker, "")
	if err != nil {
		return nil, nil, err
	}
	panel, err := s.d.Fundamentals.Get(ctx, a.ID, fundamentals.Params{})
	if err != nil {
		return nil, nil, err
	}
	b := analyst.DCFBaseOf(panel)
	if b == nil {
		return nil, nil, fmt.Errorf("%s has no usable fundamentals (%s); pass base_revenue, net_debt and shares", a.Ticker, panel.Status)
	}
	var price *decimal.Decimal
	if panel.FundamentalMetrics.Price != nil {
		p := panel.FundamentalMetrics.Price.Decimal
		price = &p
	}
	return b, price, nil
}

func (s *Server) previewDCF(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Asset       string              `json:"asset"`
		BaseRevenue *decimal.Decimal    `json:"base_revenue"`
		NetDebt     *decimal.Decimal    `json:"net_debt"`
		Shares      *decimal.Decimal    `json:"shares"`
		Scenarios   analyst.Scenarios   `json:"scenarios"`
		Initiatives []engine.Initiative `json:"initiatives"`
	}
	if err := decode(req.Params.Arguments, &in); err != nil {
		return nil, err
	}
	d := engine.DCFInput{Initiatives: in.Initiatives}
	if in.Asset != "" {
		b, _, err := s.baseFor(ctx, in.Asset)
		if err != nil {
			return nil, err
		}
		d.BaseRevenue, d.NetDebt, d.Shares = b.BaseRevenue, b.NetDebt, b.Shares
	}
	if in.BaseRevenue != nil {
		d.BaseRevenue = *in.BaseRevenue
	}
	if in.NetDebt != nil {
		d.NetDebt = *in.NetDebt
	}
	if in.Shares != nil {
		d.Shares = *in.Shares
	}
	for name, sc := range map[string]analyst.ScenarioDraft{engine.ScenarioBear: in.Scenarios.Bear, engine.ScenarioBase: in.Scenarios.Base, engine.ScenarioBull: in.Scenarios.Bull} {
		if len(sc.RevenueGrowth) != engine.DCFYears {
			return nil, fmt.Errorf("%s needs %d yearly revenue growth rates, got %d", name, engine.DCFYears, len(sc.RevenueGrowth))
		}
	}
	out := analyst.Output{Scenarios: in.Scenarios, Initiatives: in.Initiatives}
	dcf := out.DCF(analyst.DCFBase{BaseRevenue: d.BaseRevenue, NetDebt: d.NetDebt, Shares: d.Shares})
	res, err := valuation.Preview(dcf)
	if err != nil {
		return nil, err
	}
	return jsonResult(res)
}

func (s *Server) reverseDCF(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Asset string `json:"asset"`
		engine.ReverseDCFInput
	}
	if err := decode(req.Params.Arguments, &in); err != nil {
		return nil, err
	}
	r := in.ReverseDCFInput
	if in.Asset != "" {
		b, price, err := s.baseFor(ctx, in.Asset)
		if err != nil {
			return nil, err
		}
		if r.BaseRevenue.IsZero() {
			r.BaseRevenue = b.BaseRevenue
		}
		if r.NetDebt.IsZero() {
			r.NetDebt = b.NetDebt
		}
		if r.Shares.IsZero() {
			r.Shares = b.Shares
		}
		if r.Price.IsZero() && price != nil {
			r.Price = *price
		}
	}
	res, err := engine.ReverseDCF(r)
	if err != nil {
		return nil, err
	}
	return jsonResult(res)
}

func (s *Server) registerSubmit() {
	props := map[string]any{}
	for k, v := range outProps() {
		props[k] = v
	}
	props["asset"] = strProp("Ticker of an asset in the app (see list_assets)")
	props["model"] = strProp("The model you are, for the record, for example claude-opus-4-8")
	props["summary"] = strProp("Optional research note (markdown) the investor can save as a note: the reasoning behind the analysis")
	s.srv.AddTool(&mcp.Tool{
		Name:        "submit_analysis",
		Description: "Submit your analysis of an asset the app holds as a DRAFT the user reviews in the asset's AI analyst tab (nothing is applied automatically). Same structured output as the in-app analyst: catalysts, market size notes, bear/base/bull scenarios (with basis, start_margin, cash_conversion, tax_rate), new-business initiatives, a fair value range, a thesis check and sources. Cite every source by full URL: the ones the app did not serve are shown as external, unverified. The app records your client name, the model, the time and the filings you read in this session.",
		InputSchema: object(props, "asset", "model", "catalysts", "market_size_notes", "scenarios", "initiatives", "fair_value_range", "thesis_check", "sources"),
	}, raw(s.submitAnalysis))

	cprops := map[string]any{
		"ticker":            strProp("Ticker of the proposed company"),
		"market":            map[string]any{"type": "string", "enum": []string{"B3", "US"}},
		"name":              strProp("Company name"),
		"cnpj":              strProp("B3 only, required: the CNPJ, verified against CVM's register"),
		"cik":               strProp("US only, optional: the SEC CIK"),
		"model":             strProp("The model you are, for the record"),
		"note":              strProp("Research note (markdown): what the company does, why it fits the investor, risks. Required unless a draft is given"),
		"scenarios":         outProps()["scenarios"],
		"initiatives":       outProps()["initiatives"],
		"fair_value_range":  outProps()["fair_value_range"],
		"catalysts":         outProps()["catalysts"],
		"market_size_notes": outProps()["market_size_notes"],
		"sources":           outProps()["sources"],
	}
	s.srv.AddTool(&mcp.Tool{
		Name:        "submit_candidate",
		Description: "Propose a company the app does not hold as a CANDIDATE. The user accepts or rejects it on the Watchlist page; accepting creates the watchlist asset (with its CNPJ/CIK) and turns your note, scenarios and fair value range into drafts the user approves. A B3 ticker needs its CNPJ, checked against CVM's register. Counts against the daily limit of new symbols.",
		InputSchema: object(cprops, "ticker", "market", "model"),
	}, raw(s.submitCandidate))
}

// provenanceFor records the client (from the MCP handshake), the model the
// client reported, the documents read and the external URLs.
func provenanceFor(ss *mcp.ServerSession, model string, docs []analyst.Document, ext []string) analyst.Provenance {
	return analyst.Provenance{
		MCP:       analyst.MCPProvenance{Client: clientName(ss), Model: strings.TrimSpace(model), ExternalURLs: ext},
		Documents: docs,
	}
}

func (s *Server) submitAnalysis(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Asset string `json:"asset"`
		Model string `json:"model"`
		analyst.Output
	}
	if err := decode(req.Params.Arguments, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Model) == "" {
		return nil, errors.New("model is required")
	}
	a, err := s.requireAsset(ctx, in.Asset, "")
	if err != nil {
		return nil, err
	}
	out := in.Output
	docs, served := s.sessionLog(req.Session).provenance(a.Ticker)
	ext := analyst.MarkExternal(&out, served)
	run, err := s.d.Analyst.Submit(ctx, a.ID, out, provenanceFor(req.Session, in.Model, docs, ext), false)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"run_id": run.ID, "status": run.Status, "asset_id": a.ID, "ticker": a.Ticker, "source": run.Source,
		"documents_recorded": len(docs), "external_sources": ext,
		"message": fmt.Sprintf("Saved as draft #%d. The user reviews it in the app, on the AI analyst tab of %s; nothing is applied until they approve parts of it.", run.ID, a.Ticker),
	})
}

func (s *Server) submitCandidate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var in struct {
		Ticker string `json:"ticker"`
		Market string `json:"market"`
		Name   string `json:"name"`
		CNPJ   string `json:"cnpj"`
		CIK    string `json:"cik"`
		Model  string `json:"model"`
		Note   string `json:"note"`
		analyst.Output
	}
	if err := decode(req.Params.Arguments, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Model) == "" {
		return nil, errors.New("model is required")
	}
	// Same identity checks and daily cap as the lookup tools.
	t, err := s.resolve(ctx, in.Ticker, in.Market, in.CNPJ, in.CIK)
	if err != nil {
		return nil, err
	}
	if t.Known {
		return nil, fmt.Errorf("%s is already in the app (asset %d): use submit_analysis instead", t.Ticker, t.assetID())
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = t.Name
	}
	out := in.Output
	docs, served := s.sessionLog(req.Session).provenance(t.Ticker)
	ext := analyst.MarkExternal(&out, served)
	c, err := s.d.Candidates.Submit(ctx, candidates.SubmitInput{
		Ticker: t.Ticker, Market: t.Market, Name: name, CNPJ: t.CNPJ, CIK: t.CIK, Note: in.Note,
		Output: out, Provenance: provenanceFor(req.Session, in.Model, docs, ext),
	})
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{
		"candidate_id": c.ID, "status": c.Status, "ticker": c.Ticker, "market": c.Market, "name": c.Name,
		"external_sources": ext,
		"message":          "Saved as candidate #" + strconv.FormatInt(c.ID, 10) + ". The user accepts or rejects it on the Watchlist page; nothing exists for it until they accept.",
	})
}
