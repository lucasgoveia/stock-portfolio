package thesis

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

type env struct {
	svc    *Service
	alerts *alerts.Service
	now    *time.Time
	a1, a2 int64
}

func setup(t *testing.T) *env {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	now := time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	as := assets.NewService(conn)
	al := alerts.New(conn, clock)
	a1, _ := as.Create(context.Background(), assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	a2, _ := as.Create(context.Background(), assets.CreateInput{Ticker: "ITUB4", Market: "B3", Kind: "stock"})
	return &env{svc: NewService(conn, as, al, clock), alerts: al, now: &now, a1: a1.ID, a2: a2.ID}
}

func ptr[T any](v T) *T { return &v }

func (e *env) open(t *testing.T, typ string) []alerts.Alert {
	t.Helper()
	l, err := e.alerts.List(context.Background(), alerts.Filter{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPutGetAndConditions(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	if _, err := e.svc.Get(ctx, e.a1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no thesis: %v", err)
	}
	if ok, _ := e.svc.HasBreakCondition(ctx, e.a1); ok {
		t.Fatal("no thesis, no condition")
	}
	th, created, err := e.svc.Put(ctx, e.a1, PutInput{Text: " Electrification winner ", NextReviewOn: ptr("2025-06-01")})
	if err != nil || !created || th.Status != StatusActive || th.Text != "Electrification winner" || *th.NextReviewOn != "2025-06-01" {
		t.Fatalf("put: %+v %v %v", th, created, err)
	}
	c1, err := e.svc.AddCondition(ctx, e.a1, "Margin below 15% for 2 quarters")
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := e.svc.AddCondition(ctx, e.a1, "CEO leaves")
	if ok, _ := e.svc.HasBreakCondition(ctx, e.a1); !ok {
		t.Fatal("expected break condition")
	}
	// Edit and delete conditions individually.
	up, err := e.svc.UpdateCondition(ctx, c1.ID, "Margin below 12% for 2 quarters")
	if err != nil || up.Text != "Margin below 12% for 2 quarters" {
		t.Fatalf("update condition: %+v %v", up, err)
	}
	if err := e.svc.DeleteCondition(ctx, c2.ID); err != nil {
		t.Fatal(err)
	}
	th, _ = e.svc.Get(ctx, e.a1)
	if len(th.BreakConditions) != 1 || th.BreakConditions[0].Text != "Margin below 12% for 2 quarters" {
		t.Fatalf("conditions: %+v", th.BreakConditions)
	}
	if err := e.svc.DeleteCondition(ctx, c2.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if _, err := e.svc.UpdateCondition(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	// Replace keeps conditions and status, clears the review date when omitted.
	th, created, err = e.svc.Put(ctx, e.a1, PutInput{Text: "v2"})
	if err != nil || created || th.Text != "v2" || th.NextReviewOn != nil || len(th.BreakConditions) != 1 {
		t.Fatalf("replace: %+v %v %v", th, created, err)
	}
	// Validation.
	if _, _, err := e.svc.Put(ctx, e.a1, PutInput{Text: " "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty text: %v", err)
	}
	if _, _, err := e.svc.Put(ctx, e.a1, PutInput{Text: "x", NextReviewOn: ptr("soon")}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad date: %v", err)
	}
	if _, _, err := e.svc.Put(ctx, 99, PutInput{Text: "x"}); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("missing asset: %v", err)
	}
	if _, err := e.svc.AddCondition(ctx, e.a2, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("condition without thesis: %v", err)
	}
	if _, err := e.svc.AddCondition(ctx, e.a1, " "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty condition: %v", err)
	}
	if err := e.svc.Delete(ctx, e.a1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, e.a1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestBrokenAlertLifecycle(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, _, _ = e.svc.Put(ctx, e.a1, PutInput{Text: "t1"})
	_, _, _ = e.svc.Put(ctx, e.a2, PutInput{Text: "t2"})

	if len(e.open(t, AlertBroken)) != 0 {
		t.Fatal("no alerts yet")
	}
	if _, err := e.svc.SetStatus(ctx, e.a1, "broken"); err != nil {
		t.Fatal(err)
	}
	l := e.open(t, AlertBroken)
	if len(l) != 1 || *l[0].AssetID != e.a1 || l[0].State != alerts.StateOpen {
		t.Fatalf("broken alert: %+v", l)
	}
	var p map[string]any
	_ = json.Unmarshal(l[0].Payload, &p)
	if p["suggestion"] != "full_exit" || p["ticker"] != "WEGE3" {
		t.Fatalf("payload: %v", p)
	}
	// Idempotent: setting again does not duplicate.
	_, _ = e.svc.SetStatus(ctx, e.a1, "broken")
	if len(e.open(t, AlertBroken)) != 1 {
		t.Fatal("alert duplicated")
	}
	// under_review keeps neither alert; alert resolves.
	if _, err := e.svc.SetStatus(ctx, e.a1, "under_review"); err != nil {
		t.Fatal(err)
	}
	if len(e.open(t, AlertBroken)) != 0 {
		t.Fatal("broken alert should be resolved")
	}
	all, _ := e.alerts.List(ctx, alerts.Filter{Type: AlertBroken, State: alerts.StateResolved})
	if len(all) != 1 {
		t.Fatalf("resolved history: %v", all)
	}
	if _, err := e.svc.SetStatus(ctx, e.a1, "nope"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad status: %v", err)
	}
	if _, err := e.svc.SetStatus(ctx, 99, "active"); !errors.Is(err, assets.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// Deleting a broken thesis resolves its alert.
	_, _ = e.svc.SetStatus(ctx, e.a2, "broken")
	if len(e.open(t, AlertBroken)) != 1 {
		t.Fatal("a2 broken alert")
	}
	_ = e.svc.Delete(ctx, e.a2)
	if len(e.open(t, AlertBroken)) != 0 {
		t.Fatal("delete should resolve alert")
	}
}

func TestReviewDueAlertAndReview(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	_, _, _ = e.svc.Put(ctx, e.a1, PutInput{Text: "t", NextReviewOn: ptr("2025-03-15")})
	if len(e.open(t, AlertReviewDue)) != 0 {
		t.Fatal("not due yet")
	}
	// The date passes; the daily job (or any mutation) evaluates.
	*e.now = time.Date(2025, 3, 15, 8, 0, 0, 0, time.UTC)
	if err := e.svc.Evaluate(ctx); err != nil {
		t.Fatal(err)
	}
	if l := e.open(t, AlertReviewDue); len(l) != 1 || *l[0].AssetID != e.a1 {
		t.Fatalf("review due: %+v", l)
	}
	if err := e.svc.Evaluate(ctx); err != nil || len(e.open(t, AlertReviewDue)) != 1 {
		t.Fatalf("evaluate must be idempotent: %v", err)
	}
	// A review with a future date resolves it and returns under_review to active.
	_, _ = e.svc.SetStatus(ctx, e.a1, "under_review")
	rv, err := e.svc.Review(ctx, e.a1, ReviewInput{Note: "Still intact", NextReviewOn: ptr("2025-09-15")})
	if err != nil || rv.StatusAfter != StatusActive || rv.ReviewedOn != "2025-03-15" {
		t.Fatalf("review: %+v %v", rv, err)
	}
	if len(e.open(t, AlertReviewDue)) != 0 {
		t.Fatal("review should resolve the due alert")
	}
	th, _ := e.svc.Get(ctx, e.a1)
	if th.Status != StatusActive || *th.NextReviewOn != "2025-09-15" {
		t.Fatalf("thesis after review: %+v", th)
	}
	// A review can break the thesis.
	if _, err := e.svc.Review(ctx, e.a1, ReviewInput{Note: "Margins collapsed", Status: "broken"}); err != nil {
		t.Fatal(err)
	}
	if len(e.open(t, AlertBroken)) != 1 {
		t.Fatal("review to broken should alert")
	}
	rs, err := e.svc.Reviews(ctx, e.a1)
	if err != nil || len(rs) != 2 || rs[0].StatusAfter != "broken" {
		t.Fatalf("reviews: %+v %v", rs, err)
	}
	// Broken suppresses review_due even if the date has passed.
	*e.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _, _ = e.svc.Put(ctx, e.a1, PutInput{Text: "t", NextReviewOn: ptr("2025-01-01")})
	if len(e.open(t, AlertReviewDue)) != 0 {
		t.Fatal("broken thesis should not raise review due")
	}
	for name, in := range map[string]ReviewInput{
		"no note":    {Note: " "},
		"bad status": {Note: "x", Status: "meh"},
		"bad date":   {Note: "x", ReviewedOn: "yesterday"},
		"bad next":   {Note: "x", NextReviewOn: ptr("later")},
	} {
		if _, err := e.svc.Review(ctx, e.a1, in); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.Review(ctx, e.a2, ReviewInput{Note: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("review without thesis: %v", err)
	}
}
