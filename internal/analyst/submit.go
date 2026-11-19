package analyst

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// Provenance is where an MCP draft came from: the client and model, when it
// was submitted, the app-served documents the session read and the cited URLs
// the app did not serve.
type Provenance struct {
	MCP       MCPProvenance `json:"mcp"`
	Documents []Document    `json:"documents"`
}

// MarkExternal flags each source the app did not serve ("external,
// unverified") and returns the distinct URLs cited anywhere in the output
// (sources, catalysts, initiatives) that are not in served.
func MarkExternal(o *Output, served map[string]bool) []string {
	ext := []string{}
	note := func(u string) {
		if u != "" && !served[u] && !slices.Contains(ext, u) {
			ext = append(ext, u)
		}
	}
	for i := range o.Sources {
		o.Sources[i].External = !served[o.Sources[i].URL]
		note(o.Sources[i].URL)
	}
	for _, c := range o.Catalysts {
		for _, u := range c.SourceURLs {
			note(u)
		}
	}
	for _, in := range o.Initiatives {
		for _, u := range in.Sources {
			note(u)
		}
	}
	return ext
}

// Submit stores an analysis written in the user's Claude client over MCP as a
// draft run on an existing asset. The output is validated like the in-app
// analyst's (with partial, scenarios and the fair value range may be left
// out). The run costs 0 and is never counted toward the monthly cap.
func (s *Service) Submit(ctx context.Context, assetID int64, out Output, prov Provenance, partial bool) (Run, error) {
	asset, err := s.d.Assets.Get(ctx, assetID)
	if err != nil {
		return Run{}, err
	}
	if err := out.Validate(partial); err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	in, err := s.baseInputs(ctx, asset)
	if err != nil {
		return Run{}, err
	}
	in.MCP = &prov.MCP
	in.Documents = prov.Documents
	if in.Documents == nil {
		in.Documents = []Document{}
	}
	if in.MCP.ExternalURLs == nil {
		in.MCP.ExternalURLs = []string{}
	}
	if in.MCP.SubmittedAt == "" {
		in.MCP.SubmittedAt = s.ts()
	}
	inJSON, err := json.Marshal(in)
	if err != nil {
		return Run{}, err
	}
	outJSON, err := json.Marshal(out)
	if err != nil {
		return Run{}, err
	}
	model := strings.TrimSpace(prov.MCP.Model)
	if model == "" {
		model = "unknown"
	}
	row, err := s.q.CreateAnalystRun(ctx, dbRunParams(asset.ID, model, string(inJSON), string(outJSON), s.ts()))
	if err != nil {
		return Run{}, fmt.Errorf("store analyst run: %w", err)
	}
	return s.convert(row)
}

func dbRunParams(assetID int64, model, inputs, output, createdAt string) db.CreateAnalystRunParams {
	return db.CreateAnalystRunParams{
		AssetID: assetID, Model: model, Status: StatusDraft, Inputs: inputs, Output: output,
		CostUsd: "0", CreatedAt: createdAt, Source: SourceMCP,
	}
}
