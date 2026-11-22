package analyst

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/fundamentals"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/lucasgoveia/stock-portfolio/internal/research"
	"github.com/lucasgoveia/stock-portfolio/internal/settings"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
	"github.com/lucasgoveia/stock-portfolio/internal/thesis"
	"github.com/lucasgoveia/stock-portfolio/internal/valuation"
)

const goodOutput = `{
 "catalysts": [
  {"title": "Services growth", "detail": "Services revenue keeps compounding", "expected_date": "2027-02-01", "source_urls": ["https://sec.test/10k.htm", "https://evil.test/x"]},
  {"title": "Buyback", "detail": "Authorization renewed", "expected_date": "", "source_urls": []}
 ],
 "market_size_notes": "Large installed base.",
 "scenarios": {
  "bear": {"basis": "net_pe", "start_margin": 0.25, "cash_conversion": 0.6, "tax_rate": 0.21, "revenue_growth": [0.01,0.01,0.01,0.01,0.01], "terminal_margin": 0.2, "exit_multiple": 12, "discount_rate": 0.1, "probability": 0.25, "rationale": "slow"},
  "base": {"basis": "net_pe", "start_margin": 0.25, "cash_conversion": 0.6, "tax_rate": 0.21, "revenue_growth": [0.06,0.06,0.05,0.05,0.04], "terminal_margin": 0.25, "exit_multiple": 18, "discount_rate": 0.09, "probability": 0.5, "rationale": "steady"},
  "bull": {"basis": "net_pe", "start_margin": 0.25, "cash_conversion": 0.6, "tax_rate": 0.21, "revenue_growth": [0.1,0.1,0.09,0.08,0.07], "terminal_margin": 0.3, "exit_multiple": 24, "discount_rate": 0.09, "probability": 0.25, "rationale": "fast"}
 },
 "initiatives": [
  {"name": "Vision hardware", "rationale": "New segment in the 10-K", "sources": ["https://sec.test/10k.htm", "https://evil.test/x"], "start_year": 3, "revenue_year5": 20000000, "margin": 0.15,
   "success_probability": {"bear": 0.1, "base": 0.4, "bull": 0.7}}
 ],
 "fair_value_range": {"low": 150, "high": 210, "currency": "USD"},
 "thesis_check": [
  {"condition": "Services growth below 5%", "status": "holds", "evidence": "Grew 12%"},
  {"condition": "Margin under 20%", "status": "at_risk", "evidence": "Margin fell"}
 ],
 "sources": [{"title": "10-K", "url": "https://sec.test/10k.htm"}, {"title": "made up", "url": "https://evil.test/x"}]
}`

type fakeClient struct {
	text    string
	in, out int64
	err     error
	calls   int
	last    Request
}

func (f *fakeClient) Complete(_ context.Context, r Request) (Response, error) {
	f.calls++
	f.last = r
	if f.err != nil {
		return Response{}, f.err
	}
	return Response{Text: f.text, InputTokens: f.in, OutputTokens: f.out}, nil
}

type fakeFund struct {
	panel fundamentals.FundamentalsPanel
}

func (f fakeFund) Get(context.Context, int64, fundamentals.Params) (fundamentals.FundamentalsPanel, error) {
	return f.panel, nil
}

type fakeUS struct{}

func (fakeUS) RecentFilings(context.Context, string, []string, int) ([]marketdata.Filing, error) {
	return []marketdata.Filing{
		{Form: "10-K", FilingDate: "2026-11-03", Title: "10-K filed 2026-11-03", URL: "https://sec.test/10k.htm", Dir: "https://sec.test/d1"},
		{Form: "8-K", FilingDate: "2026-11-02", Items: "2.02,9.01", Title: "8-K filed 2026-11-02", URL: "https://sec.test/8k.htm", Dir: "https://sec.test/d2"},
	}, nil
}
func (fakeUS) Exhibit99(context.Context, marketdata.Filing) (string, error) {
	return "https://sec.test/ex99.htm", nil
}
func (fakeUS) Document(_ context.Context, url string, _ int) (string, error) {
	return "TEXT OF " + url, nil
}

type fakeBR struct{}

func (fakeBR) Announcements(_ context.Context, _ string, year int, _ []string, _ int) ([]marketdata.Announcement, error) {
	if year != 2026 {
		return nil, nil
	}
	return []marketdata.Announcement{{Category: "Fato Relevante", Subject: "Dividendos", Date: "2026-03-05", URL: "https://rad.test/1"}}, nil
}

type env struct {
	svc    *Service
	client *fakeClient
	assets *assets.Service
	vals   *valuation.Service
	notes  *research.Service
	sets   *settings.Service
	now    *time.Time
	us, br int64
	fund   *fakeFund
}

func setup(t *testing.T, withKey bool) *env {
	t.Helper()
	ctx := context.Background()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	us, err := as.Create(ctx, assets.CreateInput{Ticker: "AAPL", Market: "US", Kind: "stock", CIK: "320193"})
	if err != nil {
		t.Fatal(err)
	}
	br, err := as.Create(ctx, assets.CreateInput{Ticker: "PETR4", Market: "B3", Kind: "stock", CNPJ: "33000167000101"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	al := alerts.New(conn, clock)
	th := thesis.NewService(conn, as, al, clock)
	if _, _, err := th.Put(ctx, us.ID, thesis.PutInput{Text: "Services flywheel"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"Services growth below 5%", "Margin under 20%"} {
		if _, err := th.AddCondition(ctx, us.ID, c); err != nil {
			t.Fatal(err)
		}
	}
	vals := valuation.NewService(conn, as, al, clock)
	notes := research.NewService(conn, as, clock)
	sets := settings.NewService(conn, nil)
	rev, sh := money.New(decimal.NewFromInt(400000)), money.New(decimal.NewFromInt(15000))
	fund := &fakeFund{panel: fundamentals.FundamentalsPanel{
		Status: fundamentals.StatusOK, AsOf: "2026-09-26", Currency: "USD", SourceName: "SEC",
		FundamentalMetrics: fundamentals.FundamentalMetrics{Revenue: &rev, Shares: &sh},
	}}
	e := &env{client: &fakeClient{text: goodOutput, in: 40000, out: 5000}, assets: as, vals: vals, notes: notes, sets: sets, now: &now, us: us.ID, br: br.ID, fund: fund}
	d := Deps{Conn: conn, Assets: as, Settings: sets, Thesis: th, Valuation: vals, Research: notes, Fundamentals: fund, US: fakeUS{}, BR: fakeBR{}, Clock: clock}
	if withKey {
		d.Client = e.client
	}
	e.svc = NewService(d)
	return e
}

func TestCost(t *testing.T) {
	got, err := Cost("claude-sonnet-5-5", 1_000_000, 100_000)
	if err != nil || got.String() != "3" {
		t.Fatalf("cost = %v, %v (want 2 + 1)", got, err)
	}
	got, _ = Cost("claude-sonnet-5-5", 40000, 5000)
	if got.String() != "0.13" {
		t.Errorf("small run cost = %s", got)
	}
	if _, err := Cost("claude-nope", 1, 1); err == nil {
		t.Error("unknown model should have no price")
	}
}

func TestRunDraftUS(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, err := e.svc.Run(ctx, e.us)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusDraft || run.Model != "claude-sonnet-5-5" || run.InputTokens != 40000 || run.OutputTokens != 5000 || run.CostUSD.String() != "0.13" {
		t.Fatalf("run = %+v", run)
	}
	if run.Output == nil || len(run.Output.Catalysts) != 2 {
		t.Fatalf("output = %+v", run.Output)
	}
	// Only provided documents may be cited.
	if len(run.Output.Sources) != 1 || run.Output.Sources[0].URL != "https://sec.test/10k.htm" || run.Inputs.DroppedSources != 3 {
		t.Errorf("sources not restricted: %+v dropped=%d", run.Output.Sources, run.Inputs.DroppedSources)
	}
	if len(run.Output.Catalysts[0].SourceURLs) != 1 {
		t.Errorf("catalyst sources = %v", run.Output.Catalysts[0].SourceURLs)
	}
	// Documents recorded (no text stored), 8-K earnings uses exhibit 99.1.
	if len(run.Inputs.Documents) != 2 || run.Inputs.Documents[1].Kind != "8-K exhibit 99.1" || run.Inputs.Documents[1].URL != "https://sec.test/ex99.htm" {
		t.Errorf("documents = %+v", run.Inputs.Documents)
	}
	if run.Inputs.DCFBase == nil || run.Inputs.DCFBase.Shares.String() != "15000" {
		t.Errorf("dcf base = %+v", run.Inputs.DCFBase)
	}
	// The prompt carries the thesis, conditions and document text.
	u := e.client.last.User
	for _, want := range []string{"Services flywheel", "Margin under 20%", "TEXT OF https://sec.test/10k.htm", "TEXT OF https://sec.test/ex99.htm"} {
		if !strings.Contains(u, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if e.client.last.Model != "claude-sonnet-5-5" || e.client.last.Schema == nil {
		t.Errorf("request = %+v", e.client.last.Model)
	}
	// Persisted and listed.
	list, err := e.svc.List(ctx, e.us)
	if err != nil || len(list) != 1 || list[0].ID != run.ID {
		t.Fatalf("list = %+v %v", list, err)
	}
	// Nothing was applied.
	if ns, _ := e.notes.List(ctx, e.us); len(ns) != 0 {
		t.Errorf("notes created without approval: %v", ns)
	}
	if fvs, _ := e.vals.List(ctx, e.us); len(fvs) != 0 {
		t.Errorf("fair values created without approval: %v", fvs)
	}
}

func TestRunBR(t *testing.T) {
	e := setup(t, true)
	run, err := e.svc.Run(context.Background(), e.br)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Inputs.Documents) != 1 || run.Inputs.Documents[0].Kind != "fato relevante" || run.Inputs.Documents[0].URL != "https://rad.test/1" {
		t.Errorf("documents = %+v", run.Inputs.Documents)
	}
	if !strings.Contains(e.client.last.User, "(link only)") || !strings.Contains(e.client.last.User, "No thesis is recorded") {
		t.Errorf("prompt = %s", e.client.last.User)
	}
}

func TestMissingKey(t *testing.T) {
	ctx := context.Background()
	e := setup(t, false)
	if _, err := e.svc.Run(ctx, e.us); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
	u, err := e.svc.Usage(ctx)
	if err != nil || u.CanRun || u.KeyConfigured || !strings.Contains(u.BlockedReason, "API key") {
		t.Errorf("usage = %+v %v", u, err)
	}
}

func TestCapRefusal(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	cap, _ := money.NewFromString("0.20")
	if _, err := e.sets.SetAI(ctx, settings.AI{Model: "claude-sonnet-5-5", MonthlyCapUSD: cap}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Run(ctx, e.us); err != nil { // 0.13 spent of 0.20
		t.Fatal(err)
	}
	if _, err := e.svc.Run(ctx, e.us); err != nil { // allowed: still under the cap before the call
		t.Fatal(err)
	}
	calls := e.client.calls
	_, err := e.svc.Run(ctx, e.us) // 0.26 >= 0.20
	if !errors.Is(err, ErrCapReached) || !strings.Contains(err.Error(), "$0.26 of $0.20") {
		t.Fatalf("err = %v", err)
	}
	if e.client.calls != calls {
		t.Error("the model was called past the cap")
	}
	u, _ := e.svc.Usage(ctx)
	if u.CanRun || u.SpentUSD.String() != "0.26" || u.Runs != 2 || u.RemainingUSD.String() != "0" {
		t.Errorf("usage = %+v", u)
	}
	// The next month starts fresh (calendar month in America/Sao_Paulo).
	*e.now = time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC) // still Nov 30 22:00 in Sao Paulo
	if u, _ := e.svc.Usage(ctx); u.Month != "2026-11" || u.CanRun {
		t.Errorf("still november in Sao Paulo: %+v", u)
	}
	*e.now = time.Date(2026, 12, 1, 4, 0, 0, 0, time.UTC)
	if u, _ := e.svc.Usage(ctx); u.Month != "2026-12" || !u.CanRun || u.SpentUSD.String() != "0" {
		t.Errorf("december: %+v", u)
	}
	// Status ai_usage_usd reads the same ledger.
	e.sets.UseUsage(e.svc.MonthSpend)
	if st, _ := e.sets.Status(ctx); st.AIUsageUSD.String() != "0" {
		t.Errorf("status usage = %s", st.AIUsageUSD)
	}
}

func TestUnknownModelRefused(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	if _, err := e.sets.SetAI(ctx, settings.AI{Model: "claude-mystery-9", MonthlyCapUSD: money.New(decimal.NewFromInt(20))}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Run(ctx, e.us); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "no price") {
		t.Fatalf("err = %v", err)
	}
	if e.client.calls != 0 {
		t.Error("model called without a price")
	}
}

func TestInvalidOutputStoresFailedRun(t *testing.T) {
	ctx := context.Background()
	cases := map[string]string{
		"bad json":    `{"catalysts": [`,
		"prob sum":    strings.Replace(goodOutput, `"probability": 0.5`, `"probability": 0.4`, 1),
		"growth len":  strings.Replace(goodOutput, `[0.01,0.01,0.01,0.01,0.01]`, `[0.01,0.01]`, 1),
		"range":       strings.Replace(goodOutput, `"low": 150, "high": 210`, `"low": 300, "high": 210`, 1),
		"check state": strings.Replace(goodOutput, `"holds"`, `"fine"`, 1),
		"no basis":    strings.Replace(goodOutput, `"basis": "net_pe"`, `"basis": ""`, 1),
		"bad basis":   strings.Replace(goodOutput, `"basis": "net_pe"`, `"basis": "fcf"`, 1),
		"cash conv":   strings.Replace(goodOutput, `"cash_conversion": 0.6`, `"cash_conversion": 1.6`, 1),
		"init year":   strings.Replace(goodOutput, `"start_year": 3`, `"start_year": 7`, 1),
		"init prob":   strings.Replace(goodOutput, `"bull": 0.7`, `"bull": 1.7`, 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			e := setup(t, true)
			e.client.text = text
			_, err := e.svc.Run(ctx, e.us)
			if !errors.Is(err, ErrUpstream) {
				t.Fatalf("err = %v", err)
			}
			list, _ := e.svc.List(ctx, e.us)
			if len(list) != 1 || list[0].Status != StatusFailed || list[0].Error == "" || list[0].Output != nil {
				t.Fatalf("failed run = %+v", list)
			}
			// Tokens were spent, so the cap counts them.
			if spent, _ := e.svc.MonthSpend(ctx); spent.String() != "0.13" {
				t.Errorf("failed run cost = %s", spent)
			}
		})
	}
}

func TestModelErrorStoresFailedRun(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	e.client.err = errors.New("anthropic API error 529: overloaded")
	_, err := e.svc.Run(ctx, e.us)
	if !errors.Is(err, ErrUpstream) || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("err = %v", err)
	}
	list, _ := e.svc.List(ctx, e.us)
	if len(list) != 1 || list[0].Status != StatusFailed || list[0].CostUSD.String() != "0" {
		t.Errorf("list = %+v", list)
	}
}

func TestApproveToNotes(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, err := e.svc.Run(ctx, e.us)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Approve(ctx, run.ID, ApproveInput{Catalysts: []int{0}, MarketSize: true, ThesisCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApproved || len(got.Approvals.NoteIDs) != 3 || !got.Approvals.MarketSize || !got.Approvals.ThesisCheck {
		t.Fatalf("run = %+v", got)
	}
	notes, _ := e.notes.List(ctx, e.us)
	if len(notes) != 3 {
		t.Fatalf("notes = %d", len(notes))
	}
	var cat *research.Note
	for i := range notes {
		if notes[i].Author != research.AuthorAIApproved {
			t.Errorf("author = %s", notes[i].Author)
		}
		if strings.HasPrefix(notes[i].Body, "Catalyst: Services growth") {
			cat = &notes[i]
		}
	}
	if cat == nil || len(cat.Sources) != 1 || cat.Sources[0] != "https://sec.test/10k.htm" || !strings.Contains(cat.Body, "expected 2027-02-01") || !strings.Contains(cat.Body, "run #") {
		t.Fatalf("catalyst note = %+v", cat)
	}
	// A part cannot be approved twice; the other catalyst still can.
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{Catalysts: []int{0}}); !errors.Is(err, ErrConflict) {
		t.Errorf("repeat approve err = %v", err)
	}
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{Catalysts: []int{1}}); err != nil {
		t.Errorf("second catalyst: %v", err)
	}
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{Catalysts: []int{9}}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad index err = %v", err)
	}
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty selection err = %v", err)
	}
}

func TestApproveToFairValues(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, err := e.svc.Run(ctx, e.us)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Approve(ctx, run.ID, ApproveInput{Scenarios: true, FairValueRange: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApproved || got.Approvals.DCFFairValueID == nil || got.Approvals.RangeFairValue == nil {
		t.Fatalf("approvals = %+v", got.Approvals)
	}
	dcf, err := e.vals.Get(ctx, *got.Approvals.DCFFairValueID)
	if err != nil || dcf.Method != valuation.MethodScenarioDCF || !dcf.ValuePerShare.IsPositive() {
		t.Fatalf("dcf fv = %+v %v", dcf, err)
	}
	var in map[string]json.RawMessage
	if err := json.Unmarshal(dcf.Inputs, &in); err != nil || string(in["analyst_run_id"]) != "1" || in["dcf"] == nil || in["result"] == nil {
		t.Errorf("dcf inputs = %s", dcf.Inputs)
	}
	// The basis, margins, cash conversion, tax and initiatives reach the version.
	var stored struct {
		DCF engine.DCFInput `json:"dcf"`
	}
	if err := json.Unmarshal(dcf.Inputs, &stored); err != nil {
		t.Fatal(err)
	}
	base := stored.DCF.Scenarios[1]
	if base.Basis != engine.BasisNetPE || base.StartMargin == nil || base.StartMargin.String() != "0.25" ||
		base.CashConversion.String() != "0.6" || base.TaxRate.String() != "0.21" {
		t.Errorf("stored base scenario = %+v", base)
	}
	if len(stored.DCF.Initiatives) != 1 || stored.DCF.Initiatives[0].StartYear != 3 || stored.DCF.Initiatives[0].SuccessProbability.Base.String() != "0.4" {
		t.Errorf("stored initiatives = %+v", stored.DCF.Initiatives)
	}
	if srcs := stored.DCF.Initiatives[0].Sources; len(srcs) != 1 || srcs[0] != "https://sec.test/10k.htm" {
		t.Errorf("initiative sources = %v (the unprovided URL must be dropped)", srcs)
	}
	// One version only: the range lives in the DCF version's inputs.
	if *got.Approvals.RangeFairValue != *got.Approvals.DCFFairValueID {
		t.Errorf("range fv id %d != dcf id %d", *got.Approvals.RangeFairValue, *got.Approvals.DCFFairValueID)
	}
	var rng struct {
		R struct{ Low, High, Currency string } `json:"fair_value_range"`
	}
	if err := json.Unmarshal(dcf.Inputs, &rng); err != nil || rng.R.Low != "150" || rng.R.High != "210" || rng.R.Currency != "USD" {
		t.Errorf("range in inputs = %+v %v", rng, err)
	}
	if !strings.Contains(dcf.Note, "150") {
		t.Errorf("note = %q", dcf.Note)
	}
	if list, _ := e.vals.List(ctx, e.us); len(list) != 1 {
		t.Errorf("versions = %d, want 1", len(list))
	}
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{Scenarios: true}); !errors.Is(err, ErrConflict) {
		t.Errorf("second dcf approve err = %v", err)
	}
}

func TestApproveScenariosNeedsBase(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	e.fund.panel = fundamentals.FundamentalsPanel{Status: fundamentals.StatusNotFetched, Message: "not fetched"}
	run, err := e.svc.Run(ctx, e.us)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Approve(ctx, run.ID, ApproveInput{Scenarios: true, Catalysts: []int{0}})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "base revenue") {
		t.Fatalf("err = %v", err)
	}
	// Nothing was written, not even the notes of the same request.
	if ns, _ := e.notes.List(ctx, e.us); len(ns) != 0 {
		t.Errorf("notes written: %v", ns)
	}
	if got, _ := e.svc.Get(ctx, run.ID); got.Status != StatusDraft {
		t.Errorf("status = %s", got.Status)
	}
	// The range alone can still be approved.
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{FairValueRange: true}); err != nil {
		t.Errorf("range: %v", err)
	}
}

func TestDiscard(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, _ := e.svc.Run(ctx, e.us)
	got, err := e.svc.Discard(ctx, run.ID)
	if err != nil || got.Status != StatusDiscarded {
		t.Fatalf("discard = %+v %v", got, err)
	}
	if _, err := e.svc.Approve(ctx, run.ID, ApproveInput{MarketSize: true}); !errors.Is(err, ErrConflict) {
		t.Errorf("approve discarded err = %v", err)
	}
	if _, err := e.svc.Discard(ctx, run.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("discard twice err = %v", err)
	}
	// Discarded runs still count towards spend.
	if spent, _ := e.svc.MonthSpend(ctx); spent.String() != "0.13" {
		t.Errorf("spent = %s", spent)
	}
	run2, _ := e.svc.Run(ctx, e.us)
	if _, err := e.svc.Approve(ctx, run2.ID, ApproveInput{MarketSize: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Discard(ctx, run2.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("discard approved err = %v", err)
	}
	if _, err := e.svc.Get(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing run err = %v", err)
	}
}

func TestExcerptKeepsMDA(t *testing.T) {
	toc := "Item 7. Management's Discussion and Analysis of Financial Condition and Results of Operations 20\n"
	body := strings.Repeat("risk factor text. ", 2000) + "\nItem 7. Management's Discussion and Analysis of Financial Condition and Results of Operations\nREAL MDA CONTENT"
	got := excerpt(toc + body)
	if !strings.Contains(got, "REAL MDA CONTENT") || !strings.Contains(got, "[...]") {
		t.Errorf("MD&A section lost: %d chars", len(got))
	}
}

// submitJSON stores an MCP-style draft built from goodOutput with edits.
func (e *env) submitJSON(t *testing.T, asset int64, edit func(m map[string]any)) (Run, error) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(goodOutput), &m); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(m)
	}
	raw, _ := json.Marshal(m)
	var out Output
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return e.svc.Submit(context.Background(), asset, out, Provenance{MCP: MCPProvenance{Model: "m"}}, m["scenarios"] == nil)
}

func earningsMode(m map[string]any) {
	for _, k := range []string{"bear", "base", "bull"} {
		sc := m["scenarios"].(map[string]any)[k].(map[string]any)
		sc["start_margin"], sc["terminal_margin"] = 1, 1
	}
}

func TestSubmitRejectsFullMarginWithoutDCFBase(t *testing.T) {
	e := setup(t, true)
	_, err := e.submitJSON(t, e.us, earningsMode)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "dcf_base") {
		t.Fatalf("err = %v", err)
	}
	// Only the start margin at 1 is enough to ask for it.
	_, err = e.submitJSON(t, e.us, func(m map[string]any) {
		m["scenarios"].(map[string]any)["base"].(map[string]any)["start_margin"] = 0.96
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("start margin err = %v", err)
	}
	// Margins above 1 stay invalid.
	_, err = e.submitJSON(t, e.us, func(m map[string]any) {
		earningsMode(m)
		m["dcf_base"] = map[string]any{"base_revenue": 100, "net_debt": 0, "shares": 10}
		m["scenarios"].(map[string]any)["bull"].(map[string]any)["terminal_margin"] = 1.2
	})
	if !errors.Is(err, ErrValidation) {
		t.Errorf("margin > 1 err = %v", err)
	}
	// With dcf_base it is accepted.
	if _, err := e.submitJSON(t, e.us, func(m map[string]any) {
		earningsMode(m)
		m["dcf_base"] = map[string]any{"base_revenue": 100, "net_debt": 0, "shares": 10}
	}); err != nil {
		t.Fatalf("with dcf_base: %v", err)
	}
	// A non-positive base is refused.
	if _, err := e.submitJSON(t, e.us, func(m map[string]any) {
		m["dcf_base"] = map[string]any{"base_revenue": 0, "net_debt": 0, "shares": 10}
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("zero base err = %v", err)
	}
}

func TestApproveUsesDCFBase(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, err := e.submitJSON(t, e.us, func(m map[string]any) {
		earningsMode(m)
		m["initiatives"] = []any{}
		m["dcf_base"] = map[string]any{"base_revenue": 1000, "net_debt": 5, "shares": 100}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Approve(ctx, run.ID, ApproveInput{Scenarios: true})
	if err != nil {
		t.Fatal(err)
	}
	fv, err := e.vals.Get(ctx, *got.Approvals.DCFFairValueID)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		DCF engine.DCFInput `json:"dcf"`
	}
	if err := json.Unmarshal(fv.Inputs, &stored); err != nil {
		t.Fatal(err)
	}
	d := stored.DCF
	if d.BaseRevenue.String() != "1000" || d.NetDebt.String() != "5" || d.Shares.String() != "100" {
		t.Errorf("stored base = %s %s %s (the stored fundamentals are 400000 / 15000)", d.BaseRevenue, d.NetDebt, d.Shares)
	}
}

func TestApproveRangeOnlyWithoutScenarios(t *testing.T) {
	ctx := context.Background()
	e := setup(t, true)
	run, err := e.submitJSON(t, e.us, func(m map[string]any) {
		m["scenarios"] = nil
		m["initiatives"] = []any{}
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Approve(ctx, run.ID, ApproveInput{FairValueRange: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Approvals.DCFFairValueID != nil || got.Approvals.RangeFairValue == nil {
		t.Fatalf("approvals = %+v", got.Approvals)
	}
	man, err := e.vals.Get(ctx, *got.Approvals.RangeFairValue)
	if err != nil || man.Method != valuation.MethodManual || man.ValuePerShare.String() != "180" || man.Currency != "USD" {
		t.Fatalf("range fv = %+v %v", man, err)
	}
}
