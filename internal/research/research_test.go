package research

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

type env struct {
	svc *Service
	now *time.Time
	a1  int64
	a2  int64
}

func setup(t *testing.T) *env {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	as := assets.NewService(conn)
	a1, _ := as.Create(context.Background(), assets.CreateInput{Ticker: "WEGE3", Market: "B3", Kind: "stock"})
	a2, _ := as.Create(context.Background(), assets.CreateInput{Ticker: "ITUB4", Market: "B3", Kind: "stock"})
	now := time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)
	return &env{svc: NewService(conn, as, func() time.Time { return now }), now: &now, a1: a1.ID, a2: a2.ID}
}

func ptr[T any](v T) *T { return &v }

func TestCRUD(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	n, err := e.svc.Create(ctx, e.a1, CreateInput{Body: " Tariff relief ", Sources: []string{"https://x.test/a", "  ", "10-K"}})
	if err != nil || n.Body != "Tariff relief" || n.Author != "user" || len(n.Sources) != 2 || n.Opportunity || n.OpportunityActive {
		t.Fatalf("create: %+v %v", n, err)
	}
	upd, err := e.svc.Update(ctx, n.ID, UpdateInput{Body: ptr("Tariff relief v2"), Sources: ptr([]string{"a"})})
	if err != nil || upd.Body != "Tariff relief v2" || len(upd.Sources) != 1 {
		t.Fatalf("update: %+v %v", upd, err)
	}
	got, _ := e.svc.Get(ctx, n.ID)
	if got.Body != "Tariff relief v2" {
		t.Fatalf("get: %+v", got)
	}
	if list, _ := e.svc.List(ctx, e.a1); len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	if err := e.svc.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if _, err := e.svc.Get(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	tests := []struct {
		name string
		in   CreateInput
	}{
		{"empty text", CreateInput{Body: "  "}},
		{"bad author", CreateInput{Body: "x", Author: "bot"}},
		{"opportunity without expiry", CreateInput{Body: "x", Opportunity: true}},
		{"opportunity bad expiry", CreateInput{Body: "x", Opportunity: true, OpportunityExpiresOn: "tomorrow"}},
		{"expiry without opportunity", CreateInput{Body: "x", OpportunityExpiresOn: "2025-04-01"}},
	}
	for _, tc := range tests {
		if _, err := e.svc.Create(ctx, e.a1, tc.in); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if _, err := e.svc.Create(ctx, 99, CreateInput{Body: "x"}); !errors.Is(err, assets.ErrNotFound) {
		t.Errorf("missing asset: %v", err)
	}
	plain, _ := e.svc.Create(ctx, e.a1, CreateInput{Body: "x"})
	if _, err := e.svc.Update(ctx, plain.ID, UpdateInput{Opportunity: ptr(true)}); !errors.Is(err, ErrValidation) {
		t.Errorf("flag without expiry: %v", err)
	}
	if _, err := e.svc.Update(ctx, plain.ID, UpdateInput{OpportunityExpiresOn: ptr("2025-04-01")}); !errors.Is(err, ErrValidation) {
		t.Errorf("expiry without flag: %v", err)
	}
	if _, err := e.svc.Update(ctx, plain.ID, UpdateInput{Body: ptr(" ")}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty body: %v", err)
	}
	if _, err := e.svc.Update(ctx, 99, UpdateInput{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: %v", err)
	}
}

func TestOpportunityExpiry(t *testing.T) {
	ctx := context.Background()
	e := setup(t)
	mk := func(asset int64, exp string) Note {
		n, err := e.svc.Create(ctx, asset, CreateInput{Body: "catalyst", Opportunity: true, OpportunityExpiresOn: exp})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	soon := mk(e.a1, "2025-03-15")
	mk(e.a2, "2025-03-10") // expires today: still active (inclusive)
	mk(e.a2, "2025-03-09") // already expired
	if _, err := e.svc.Create(ctx, e.a1, CreateInput{Body: "plain note"}); err != nil {
		t.Fatal(err)
	}

	list, err := e.svc.ActiveOpportunities(ctx, "", nil)
	if err != nil || len(list) != 2 {
		t.Fatalf("active today: %v %v", list, err)
	}
	if !list[0].OpportunityActive {
		t.Fatalf("active flag: %+v", list[0])
	}
	if ok, _ := e.svc.HasActiveOpportunity(ctx, e.a2, ""); !ok {
		t.Fatal("a2 should be active on its expiry day")
	}

	// Time passes: expiry is respected without any sweep.
	*e.now = time.Date(2025, 3, 11, 9, 0, 0, 0, time.UTC)
	list, _ = e.svc.ActiveOpportunities(ctx, "", nil)
	if len(list) != 1 || list[0].ID != soon.ID {
		t.Fatalf("active next day: %v", list)
	}
	if ok, _ := e.svc.HasActiveOpportunity(ctx, e.a2, ""); ok {
		t.Fatal("a2 should have expired")
	}
	got, _ := e.svc.Get(ctx, soon.ID)
	if !got.Opportunity || !got.OpportunityActive {
		t.Fatalf("soon: %+v", got)
	}
	// as_of query in the past/future.
	if l, _ := e.svc.ActiveOpportunities(ctx, "2025-03-16", nil); len(l) != 0 {
		t.Fatalf("as_of after all expiries: %v", l)
	}
	if l, _ := e.svc.ActiveOpportunities(ctx, "2025-03-09", &e.a2); len(l) != 2 {
		t.Fatalf("as_of past, asset filter: %v", l)
	}
	if _, err := e.svc.ActiveOpportunities(ctx, "bad", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad as_of: %v", err)
	}
	// Expired note read back: flag set, not active.
	exp, _ := e.svc.List(ctx, e.a2)
	for _, n := range exp {
		if n.OpportunityActive {
			t.Fatalf("expired note reads active: %+v", n)
		}
	}
	// Clearing the flag drops the expiry; extending revives it.
	cleared, err := e.svc.Update(ctx, soon.ID, UpdateInput{Opportunity: ptr(false)})
	if err != nil || cleared.Opportunity || cleared.ExpiresOn != nil {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	revived, err := e.svc.Update(ctx, soon.ID, UpdateInput{Opportunity: ptr(true), OpportunityExpiresOn: ptr("2025-06-01")})
	if err != nil || !revived.OpportunityActive {
		t.Fatalf("revive: %+v %v", revived, err)
	}
}
