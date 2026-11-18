package alerts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/store"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newService(t *testing.T) (*Service, *fakeClock, *sql.DB) {
	t.Helper()
	conn, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// Alerts reference assets; seed a few so asset_id foreign keys resolve.
	for _, tk := range []string{"AAA3", "BBB3", "CCC3", "DDD3", "EEE3"} {
		if _, err := conn.Exec(`INSERT INTO assets (ticker, name, market, currency, kind, sleeve, status, benchmark) VALUES (?, ?, 'B3', 'BRL', 'stock', 'core', 'watchlist', 'IBOV')`, tk, tk); err != nil {
			t.Fatal(err)
		}
	}
	c := &fakeClock{t: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)}
	return New(conn, c.now), c, conn
}

func ptr(v int64) *int64 { return &v }

func count(t *testing.T, conn *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow("SELECT COUNT(*) FROM alerts WHERE " + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpsertDedupe(t *testing.T) {
	tests := []struct {
		name     string
		first    func(*Service) error
		second   func(*Service) error
		wantRows int
	}{
		{"same key dedupes",
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "", 1)
				return err
			},
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "", 2)
				return err
			}, 1},
		{"different asset",
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "", 1)
				return err
			},
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(2), "", 1)
				return err
			}, 2},
		{"different variant",
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "a", 1)
				return err
			},
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "b", 1)
				return err
			}, 2},
		{"different type",
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overweight", ptr(1), "", 1)
				return err
			},
			func(s *Service) error {
				_, err := s.Upsert(context.Background(), "overvalued", ptr(1), "", 1)
				return err
			}, 2},
		{"null asset dedupes",
			func(s *Service) error { _, err := s.Upsert(context.Background(), "raise_cash", nil, "", 1); return err },
			func(s *Service) error { _, err := s.Upsert(context.Background(), "raise_cash", nil, "", 2); return err }, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _, conn := newService(t)
			if err := tc.first(s); err != nil {
				t.Fatal(err)
			}
			if err := tc.second(s); err != nil {
				t.Fatal(err)
			}
			if got := count(t, conn, "1=1"); got != tc.wantRows {
				t.Fatalf("rows = %d, want %d", got, tc.wantRows)
			}
		})
	}
}

func TestUpsertUpdatesPayloadKeepsState(t *testing.T) {
	ctx := context.Background()
	s, clock, _ := newService(t)
	a, err := s.Upsert(ctx, "overweight", ptr(1), "", map[string]int{"w": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acknowledge(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(time.Hour)
	b, err := s.Upsert(ctx, "overweight", ptr(1), "", map[string]int{"w": 2})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID || b.State != StateAcknowledged || string(b.Payload) != `{"w":2}` || !b.UpdatedAt.Equal(clock.t) || !b.CreatedAt.Equal(a.CreatedAt) {
		t.Fatalf("unexpected alert after upsert: %+v", b)
	}
}

func TestUpsertAfterResolveCreatesNew(t *testing.T) {
	ctx := context.Background()
	s, _, conn := newService(t)
	a, _ := s.Upsert(ctx, "overweight", ptr(1), "", nil)
	if _, err := s.Resolve(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	b, err := s.Upsert(ctx, "overweight", ptr(1), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == a.ID || b.State != StateOpen || count(t, conn, "1=1") != 2 {
		t.Fatalf("expected a fresh open alert, got %+v", b)
	}
}

func TestUpsertInvalidType(t *testing.T) {
	s, _, _ := newService(t)
	if _, err := s.Upsert(context.Background(), "bogus", nil, "", nil); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveMissing(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		active       []string
		wantResolved int
		wantOpen     int
	}{
		{"empty active set resolves all of type", nil, 3, 1},
		{"keeps active", []string{Key("overweight", ptr(1), "")}, 2, 2},
		{"keeps all", []string{Key("overweight", ptr(1), ""), Key("overweight", ptr(2), ""), Key("overweight", ptr(3), "")}, 0, 4},
		{"unknown key ignored", []string{"x|y|z"}, 3, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _, conn := newService(t)
			for i := int64(1); i <= 3; i++ {
				a, err := s.Upsert(ctx, "overweight", ptr(i), "", nil)
				if err != nil {
					t.Fatal(err)
				}
				if i == 2 {
					_, _ = s.Acknowledge(ctx, a.ID)
				}
				if i == 3 {
					_, _ = s.Snooze(ctx, a.ID, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
				}
			}
			if _, err := s.Upsert(ctx, "overvalued", ptr(1), "", nil); err != nil {
				t.Fatal(err)
			}
			n, err := s.ResolveMissing(ctx, "overweight", tc.active)
			if err != nil {
				t.Fatal(err)
			}
			if n != tc.wantResolved {
				t.Fatalf("resolved = %d, want %d", n, tc.wantResolved)
			}
			if got := count(t, conn, "state <> 'resolved'"); got != tc.wantOpen {
				t.Fatalf("non-resolved = %d, want %d", got, tc.wantOpen)
			}
			if count(t, conn, "type = 'overvalued' AND state = 'open'") != 1 {
				t.Fatal("other type must be untouched")
			}
			if count(t, conn, "state = 'resolved' AND resolved_at IS NULL") != 0 {
				t.Fatal("resolved_at not set")
			}
		})
	}
}

func TestSnoozeExpiry(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		advance   time.Duration
		wantOpen  int
		wantSnooz int
	}{
		{"before deadline hidden", 23 * time.Hour, 0, 1},
		{"at deadline reappears", 24 * time.Hour, 1, 0},
		{"after deadline reappears", 48 * time.Hour, 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, clock, _ := newService(t)
			a, _ := s.Upsert(ctx, "overweight", ptr(1), "", nil)
			if _, err := s.Snooze(ctx, a.ID, clock.t.Add(24*time.Hour)); err != nil {
				t.Fatal(err)
			}
			clock.t = clock.t.Add(tc.advance)
			open, _ := s.List(ctx, Filter{})
			if len(open) != tc.wantOpen {
				t.Fatalf("open = %d, want %d", len(open), tc.wantOpen)
			}
			snoozed, _ := s.List(ctx, Filter{State: StateSnoozed})
			if len(snoozed) != tc.wantSnooz {
				t.Fatalf("snoozed = %d, want %d", len(snoozed), tc.wantSnooz)
			}
			got, _ := s.Get(ctx, a.ID)
			if tc.wantOpen == 1 && got.State != StateOpen {
				t.Fatalf("effective state = %s", got.State)
			}
		})
	}
}

func TestSnoozePastRejected(t *testing.T) {
	ctx := context.Background()
	s, clock, _ := newService(t)
	a, _ := s.Upsert(ctx, "overweight", ptr(1), "", nil)
	if _, err := s.Snooze(ctx, a.ID, clock.t); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestLifecycleErrors(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newService(t)
	if _, err := s.Acknowledge(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	a, _ := s.Upsert(ctx, "overweight", ptr(1), "", nil)
	if _, err := s.Resolve(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acknowledge(ctx, a.ID); !errors.Is(err, ErrResolved) {
		t.Fatalf("err = %v", err)
	}
}

func TestListFilters(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newService(t)
	a, _ := s.Upsert(ctx, "overweight", ptr(1), "", nil)
	_, _ = s.Upsert(ctx, "overweight", ptr(2), "", nil)
	_, _ = s.Upsert(ctx, "overvalued", ptr(1), "", nil)
	_, _ = s.Acknowledge(ctx, a.ID)
	tests := []struct {
		name string
		f    Filter
		want int
	}{
		{"default open", Filter{}, 2},
		{"acknowledged", Filter{State: StateAcknowledged}, 1},
		{"all", Filter{State: StateAll}, 3},
		{"type", Filter{Type: "overvalued"}, 1},
		{"asset", Filter{State: StateAll, AssetID: ptr(1)}, 2},
		{"type and asset", Filter{State: StateAll, Type: "overweight", AssetID: ptr(2)}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("len = %d, want %d", len(got), tc.want)
			}
		})
	}
	if _, err := s.List(ctx, Filter{State: "bogus"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}
