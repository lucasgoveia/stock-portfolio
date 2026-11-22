package analyst

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

func sourceURLs(o *Output) []string {
	urls := make([]string, 0, len(o.Sources))
	for _, s := range o.Sources {
		urls = append(urls, s.URL)
	}
	return urls
}

func uniq(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func catalystText(c Catalyst) string {
	var b strings.Builder
	b.WriteString("Catalyst: " + strings.TrimSpace(c.Title))
	if d := strings.TrimSpace(c.Detail); d != "" {
		b.WriteString(". " + d)
	}
	if c.ExpectedDate != "" {
		b.WriteString(" (expected " + c.ExpectedDate + ")")
	}
	return b.String()
}

func thesisCheckText(checks []ThesisCheck) string {
	lines := []string{"Thesis check (AI analyst):"}
	for _, c := range checks {
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", c.Status, c.Condition, c.Evidence))
	}
	return strings.Join(lines, "\n")
}

// Approve applies the selected parts of a draft: catalysts, market size and
// the thesis check become research notes (author ai_approved, with sources),
// the scenarios become a scenario DCF version (through the valuation service,
// so its validation applies, on the run's dcf_base when it has one) with the
// range stored in its inputs; only a run without scenarios turns its range into
// a manual version at its midpoint. Parts already approved are refused. Anything approved makes the
// run approved.
func (s *Service) Approve(ctx context.Context, id int64, in ApproveInput) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.Get(ctx, id)
	if err != nil {
		return Run{}, err
	}
	if run.Status != StatusDraft && run.Status != StatusApproved {
		return Run{}, fmt.Errorf("%w: a %s run cannot be approved", ErrConflict, run.Status)
	}
	if run.Output == nil {
		return Run{}, fmt.Errorf("%w: run has no output", ErrConflict)
	}
	o, ap := run.Output, run.Approvals
	if len(in.Catalysts) == 0 && !in.MarketSize && !in.Summary && !in.ThesisCheck && !in.Scenarios && !in.FairValueRange {
		return Run{}, validationf("select at least one part to approve")
	}
	idx := slices.Clone(in.Catalysts)
	slices.Sort(idx)
	idx = slices.Compact(idx)
	for _, i := range idx {
		if i < 0 || i >= len(o.Catalysts) {
			return Run{}, validationf("catalyst %d does not exist", i)
		}
		if slices.Contains(ap.Catalysts, i) {
			return Run{}, fmt.Errorf("%w: catalyst %d was already approved", ErrConflict, i)
		}
	}
	if in.MarketSize && (ap.MarketSize || strings.TrimSpace(o.MarketSizeNotes) == "") {
		return Run{}, fmt.Errorf("%w: market size notes are empty or already approved", ErrConflict)
	}
	if in.Summary && (ap.Summary || strings.TrimSpace(o.Summary) == "") {
		return Run{}, fmt.Errorf("%w: the research note is empty or already approved", ErrConflict)
	}
	if in.ThesisCheck && (ap.ThesisCheck || len(o.ThesisCheck) == 0) {
		return Run{}, fmt.Errorf("%w: the thesis check is empty or already approved", ErrConflict)
	}
	if in.Scenarios && ap.DCFFairValueID != nil {
		return Run{}, fmt.Errorf("%w: scenarios were already approved", ErrConflict)
	}
	if in.FairValueRange && !o.hasRange() {
		return Run{}, validationf("the run has no fair value range")
	}
	if in.FairValueRange && ap.RangeFairValue != nil {
		return Run{}, fmt.Errorf("%w: the fair value range was already approved", ErrConflict)
	}
	// With scenarios the range is stored inside the scenario DCF version (made
	// now or by an earlier approval), so no second version competes with it. The
	// range alone, with the scenarios left unapproved, still makes the manual
	// midpoint version: the user asked for exactly that.
	rangeInDCF := in.FairValueRange && o.hasScenarios() && (in.Scenarios || ap.DCFFairValueID != nil)

	// Validate the fair value parts before writing anything.
	var dcf *engine.DCFInput
	if in.Scenarios {
		if !o.hasScenarios() {
			return Run{}, validationf("the run has no scenarios")
		}
		base := o.DCFBase
		if base == nil {
			base = run.Inputs.DCFBase
		}
		if base == nil {
			// The draft was made before the fundamentals existed (a new candidate):
			// use the stored ones now.
			if a, err := s.d.Assets.Get(ctx, run.AssetID); err == nil {
				if panel, err := s.d.Fundamentals.Get(ctx, a.ID, fundamentals.Params{}); err == nil {
					base = dcfBase(panel)
				}
			}
		}
		if base == nil {
			return Run{}, validationf("the run has no base revenue and share count (fundamentals were not available), so its scenarios cannot become a DCF version; refresh the asset's fundamentals and try again")
		}
		d := o.DCF(*base)
		if _, err := valuation.Preview(d); err != nil {
			return Run{}, err
		}
		dcf = &d
	}
	srcs := sourceURLs(o)
	note := fmt.Sprintf("AI analyst run #%d (%s)", run.ID, run.Model)
	if run.Source == SourceMCP {
		client := ""
		if run.Inputs.MCP != nil && run.Inputs.MCP.Client != "" {
			client = " via " + run.Inputs.MCP.Client
		}
		note = fmt.Sprintf("MCP analyst draft #%d (%s%s)", run.ID, run.Model, client)
	}
	if dcf != nil {
		inputs := map[string]any{"analyst_run_id": run.ID}
		dcfNote := note
		if o.hasRange() {
			r := o.FairValueRange
			inputs["fair_value_range"] = map[string]any{"low": r.Low.String(), "high": r.High.String(), "currency": r.Currency}
			dcfNote += fmt.Sprintf(": proposed range %s to %s %s", r.Low, r.High, r.Currency)
		}
		fv, err := s.d.Valuation.Create(ctx, run.AssetID, valuation.CreateInput{
			Method: valuation.MethodScenarioDCF, DCF: dcf, Sources: srcs, Note: dcfNote, Inputs: inputs,
		})
		if err != nil {
			return Run{}, err
		}
		ap.DCFFairValueID = &fv.ID
	}
	if rangeInDCF {
		id := *ap.DCFFairValueID
		ap.RangeFairValue = &id
	} else if in.FairValueRange {
		r := o.FairValueRange
		mid := r.Low.Add(r.High).Div(decimal.NewFromInt(2)).Round(4)
		v := money.New(mid)
		fv, err := s.d.Valuation.Create(ctx, run.AssetID, valuation.CreateInput{
			Method: valuation.MethodManual, ValuePerShare: &v, Currency: r.Currency, Sources: srcs, Note: note + ": midpoint of the proposed range",
			Inputs: map[string]any{"analyst_run_id": run.ID, "low": r.Low.String(), "high": r.High.String(), "basis": "midpoint"},
		})
		if err != nil {
			return Run{}, err
		}
		ap.RangeFairValue = &fv.ID
	}
	addNote := func(text string, sources []string) error {
		n, err := s.d.Research.Create(ctx, run.AssetID, research.CreateInput{
			Body: text + "\n\n(" + note + ")", Sources: uniq(sources), Author: research.AuthorAIApproved,
		})
		if err != nil {
			return err
		}
		ap.NoteIDs = append(ap.NoteIDs, n.ID)
		return nil
	}
	for _, i := range idx {
		c := o.Catalysts[i]
		if err := addNote(catalystText(c), c.SourceURLs); err != nil {
			return Run{}, err
		}
		ap.Catalysts = append(ap.Catalysts, i)
	}
	if in.MarketSize {
		if err := addNote("Market size: "+strings.TrimSpace(o.MarketSizeNotes), srcs); err != nil {
			return Run{}, err
		}
		ap.MarketSize = true
	}
	if in.Summary {
		if err := addNote(strings.TrimSpace(o.Summary), srcs); err != nil {
			return Run{}, err
		}
		ap.Summary = true
	}
	if in.ThesisCheck {
		if err := addNote(thesisCheckText(o.ThesisCheck), srcs); err != nil {
			return Run{}, err
		}
		ap.ThesisCheck = true
	}
	return s.setStatus(ctx, id, StatusApproved, ap)
}
