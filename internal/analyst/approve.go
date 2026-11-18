package analyst

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
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
// so its validation applies) and the range becomes a manual version at its
// midpoint. Parts already approved are refused. Anything approved makes the
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
	if len(in.Catalysts) == 0 && !in.MarketSize && !in.ThesisCheck && !in.Scenarios && !in.FairValueRange {
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
	if in.ThesisCheck && (ap.ThesisCheck || len(o.ThesisCheck) == 0) {
		return Run{}, fmt.Errorf("%w: the thesis check is empty or already approved", ErrConflict)
	}
	if in.Scenarios && ap.DCFFairValueID != nil {
		return Run{}, fmt.Errorf("%w: scenarios were already approved", ErrConflict)
	}
	if in.FairValueRange && ap.RangeFairValue != nil {
		return Run{}, fmt.Errorf("%w: the fair value range was already approved", ErrConflict)
	}

	// Validate the fair value parts before writing anything.
	var dcf *engine.DCFInput
	if in.Scenarios {
		if run.Inputs.DCFBase == nil {
			return Run{}, validationf("the run has no base revenue and share count (fundamentals were not available), so its scenarios cannot become a DCF version")
		}
		d := o.DCF(*run.Inputs.DCFBase)
		if _, err := valuation.Preview(d); err != nil {
			return Run{}, err
		}
		dcf = &d
	}
	srcs := sourceURLs(o)
	note := fmt.Sprintf("AI analyst run #%d (%s)", run.ID, run.Model)
	if dcf != nil {
		fv, err := s.d.Valuation.Create(ctx, run.AssetID, valuation.CreateInput{
			Method: valuation.MethodScenarioDCF, DCF: dcf, Sources: srcs, Note: note,
			Inputs: map[string]any{"analyst_run_id": run.ID},
		})
		if err != nil {
			return Run{}, err
		}
		ap.DCFFairValueID = &fv.ID
	}
	if in.FairValueRange {
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
	if in.ThesisCheck {
		if err := addNote(thesisCheckText(o.ThesisCheck), srcs); err != nil {
			return Run{}, err
		}
		ap.ThesisCheck = true
	}
	return s.setStatus(ctx, id, StatusApproved, ap)
}
