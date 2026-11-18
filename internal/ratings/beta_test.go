package ratings

import (
	"context"
	"errors"
	"testing"
)

func TestAutomaticVolatilityFromBeta(t *testing.T) {
	ctx := context.Background()
	svc, id := setup(t)

	// No beta yet: volatility must be given.
	if _, err := svc.Add(ctx, id, in("", [5]int{0, 4, 5, 4, 5}, "first look")); !errors.Is(err, ErrValidation) {
		t.Fatalf("auto without beta: %v", err)
	}
	if _, err := svc.GetBeta(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta before compute: %v", err)
	}
	// Beta exists but no rating yet: stored, nothing appended.
	if ch, err := svc.ApplyBeta(ctx, id, "IBOV", d("0.8"), 104); err != nil || ch {
		t.Fatalf("apply without rating: %v %v", ch, err)
	}
	b, err := svc.GetBeta(ctx, id)
	if err != nil || b.Score != 4 || b.Weeks != 104 {
		t.Fatalf("beta = %+v, %v", b, err)
	}
	r, err := svc.Add(ctx, id, in("", [5]int{0, 4, 5, 4, 5}, "auto vol"))
	if err != nil || r.Volatility != 4 || !r.VolatilityAuto {
		t.Fatalf("auto rating = %+v, %v", r, err)
	}
	// Same bucket: no new rating.
	if ch, _ := svc.ApplyBeta(ctx, id, "IBOV", d("0.85"), 104); ch {
		t.Fatal("same bucket must not append")
	}
	// New bucket: one rating appended with only volatility changed.
	ch, err := svc.ApplyBeta(ctx, id, "IBOV", d("1.5"), 104)
	if err != nil || !ch {
		t.Fatalf("bucket change: %v %v", ch, err)
	}
	last, _ := svc.Latest(ctx, id)
	if last.Volatility != 1 || !last.VolatilityAuto || last.Moat != 5 || last.Reason != "auto: beta 1.50 over 104 weeks vs IBOV" {
		t.Fatalf("latest = %+v", last)
	}
	// Manual override sticks.
	if _, err := svc.Add(ctx, id, in("", [5]int{3, 4, 5, 4, 5}, "I know better")); err != nil {
		t.Fatal(err)
	}
	if ch, _ := svc.ApplyBeta(ctx, id, "IBOV", d("0.5"), 104); ch {
		t.Fatal("manual override must not be replaced")
	}
	if h, _ := svc.History(ctx, id); len(h) != 3 {
		t.Fatalf("history = %d rows", len(h))
	}
}
