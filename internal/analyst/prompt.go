package analyst

import (
	"fmt"
	"strings"
)

const systemPrompt = `You are an equity research analyst assisting a long-term investor who keeps a written investment thesis per stock. You produce a draft for the investor to review; nothing you write is applied automatically.

Rules:
- Use ONLY the information in the user message: the thesis, break conditions, current fair value, fundamentals and the listed documents. Do not use outside knowledge of the company's recent events, and do not invent figures, dates or quotes. If the documents do not support something, say so plainly instead of guessing.
- Cite only the provided documents. In "sources" and in each catalyst's "source_urls", copy URLs exactly as listed in the documents section; never cite a URL that was not provided.
- Documents marked "link only" are announcements whose text was not provided; you may mention their subject lines but cannot describe their content.
- Scenarios: exactly bear, base and bull. revenue_growth has exactly 5 yearly rates as fractions (0.08 means 8%). terminal_margin is a fraction between 0 and 1, exit_multiple and discount_rate are positive (discount_rate a fraction, for example 0.12). The three probabilities are fractions that sum to exactly 1. Explain the assumptions in "rationale", tied to the documents and the reported fundamentals.
- fair_value_range is per share, in the asset's currency, with 0 < low <= high, consistent with the scenarios and the share count given.
- thesis_check: one entry for each break condition, copying the condition text. holds = no evidence it is triggered; at_risk = evidence that it is deteriorating; broken = evidence it has occurred. If the documents say nothing, answer holds and state that there was no evidence either way.
- catalysts: at most 12 upcoming or recent events that could move the price, each with a short detail. Use an empty expected_date when no date is known.
- Be concise and factual. Write in the language of the thesis.`

// userPrompt renders the inputs as the user message.
func userPrompt(in Inputs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Asset: %s (%s market, currency %s)\n\n", in.Ticker, in.Market, in.Currency)
	b.WriteString("## Investment thesis\n")
	if in.Thesis == nil {
		b.WriteString("No thesis is recorded for this asset.\n")
	} else {
		fmt.Fprintf(&b, "Status: %s\n%s\n\nBreak conditions:\n", in.Thesis.Status, in.Thesis.Text)
		if len(in.Thesis.BreakConditions) == 0 {
			b.WriteString("(none recorded)\n")
		}
		for i, c := range in.Thesis.BreakConditions {
			fmt.Fprintf(&b, "%d. %s\n", i+1, c)
		}
	}
	b.WriteString("\n## Current fair value\n")
	if in.FairValue == nil {
		b.WriteString("None recorded.\n")
	} else {
		fv := in.FairValue
		fmt.Fprintf(&b, "%s %s per share (%s, as of %s)\n", fv.ValuePerShare, fv.Currency, fv.Method, fv.AsOf)
		if fv.Scenarios != nil {
			if raw, ok := fv.Scenarios.(interface{ MarshalJSON() ([]byte, error) }); ok {
				if j, err := raw.MarshalJSON(); err == nil {
					fmt.Fprintf(&b, "Inputs and scenarios: %s\n", j)
				}
			}
		}
	}
	b.WriteString("\n## Fundamentals\n")
	b.WriteString(in.fundamentalsJSON + "\n")
	if in.DCFBase != nil {
		fmt.Fprintf(&b, "\nThe scenario DCF will be computed on base revenue %s, net debt %s and %s shares outstanding; size your scenarios and fair value range accordingly.\n",
			in.DCFBase.BaseRevenue, in.DCFBase.NetDebt, in.DCFBase.Shares)
	}
	b.WriteString("\n## Documents\n")
	if len(in.Documents) == 0 {
		b.WriteString("No documents could be fetched. Base the answer on the thesis and fundamentals only, and say that no filings were available.\n")
	}
	for i, d := range in.Documents {
		fmt.Fprintf(&b, "\n### Document %d: %s\nKind: %s\nDate: %s\nURL: %s\n", i+1, d.Title, d.Kind, d.Date, d.URL)
		if d.Text == "" {
			b.WriteString("(link only)\n")
			continue
		}
		b.WriteString("Text:\n" + d.Text + "\n")
	}
	return b.String()
}
