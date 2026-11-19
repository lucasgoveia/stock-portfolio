package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestShareFactorAfter(t *testing.T) {
	d := decimal.RequireFromString
	ev := func(date, f string) ShareEvent { return ShareEvent{ExDate: date, Factor: d(f)} }
	const rec, today = "2026-03-10", "2026-11-18"
	cases := []struct {
		name   string
		events []ShareEvent
		want   string
	}{
		{"none", nil, "1"},
		{"split 2:1 after", []ShareEvent{ev("2026-06-01", "2")}, "2"},
		{"reverse 1:10 after", []ShareEvent{ev("2026-06-01", "0.1")}, "0.1"},
		{"bonus 10% after", []ShareEvent{ev("2026-06-01", "1.1")}, "1.1"},
		{"two events", []ShareEvent{ev("2026-04-01", "2"), ev("2026-09-01", "1.1")}, "2.2"},
		{"before record date ignored", []ShareEvent{ev("2026-01-05", "2")}, "1"},
		{"ex-date equal to record date ignored", []ShareEvent{ev("2026-03-10", "2")}, "1"},
		{"day after record date counts", []ShareEvent{ev("2026-03-11", "2")}, "2"},
		{"future ex-date ignored", []ShareEvent{ev("2026-12-01", "2")}, "1"},
		{"ex-date today counts", []ShareEvent{ev(today, "2")}, "2"},
		{"mixed before and after", []ShareEvent{ev("2026-01-05", "2"), ev("2026-05-01", "0.5")}, "0.5"},
		{"invalid factor skipped", []ShareEvent{ev("2026-05-01", "0")}, "1"},
	}
	for _, c := range cases {
		if got := ShareFactorAfter(rec, today, c.events); !got.Equal(d(c.want)) {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
