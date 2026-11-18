package engine

import (
	"reflect"
	"testing"
)

func TestTacticalSize(t *testing.T) {
	p := DefaultParams()
	for _, c := range []struct {
		name, entry, stop string
		size, dist, loss  string
		bad               bool
	}{
		// 1% / 10% = 10% -> capped at 2%; loss 0.2%.
		{"tight stop caps at 2%", "100", "90", "0.02", "0.1", "0.002", false},
		// 1% / 50% = 2% exactly.
		{"stop at 50% gives 2%", "100", "50", "0.02", "0.5", "0.01", false},
		// 1% / 80% = 1.25%.
		{"wide stop sizes between", "100", "20", "0.0125", "0.8", "0.01", false},
		// 1% / 100%-ish floors at 1%: stop 0.5 of 100 -> dist 0.995 -> 1.005%.
		{"very wide stop near 1%", "100", "0.5", "0.0100502512562814", "0.995", "0.01", false},
		{"stop above entry", "100", "110", "", "", "", true},
		{"stop equals entry", "100", "100", "", "", "", true},
		{"zero stop", "100", "0", "", "", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := p.TacticalSize(d(c.entry), d(c.stop))
			if c.bad {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !got.Size.Round(16).Equal(d(c.size)) || !got.StopDistance.Equal(d(c.dist)) || !got.LossAtStop.Round(10).Equal(d(c.loss)) {
				t.Fatalf("got %+v", got)
			}
			if got.LossAtStop.GreaterThan(p.TacticalMaxLoss) {
				t.Fatalf("loss at stop %s above max", got.LossAtStop)
			}
		})
	}
}

func TestTacticalSignals(t *testing.T) {
	for _, c := range []struct {
		name, price, today string
		want               []TacticalSignal
	}{
		{"inside range", "100", "2026-11-18", nil},
		{"target touched", "130", "2026-11-18", []TacticalSignal{TacticalTargetHit}},
		{"stop touched", "90", "2026-11-18", []TacticalSignal{TacticalStopHit}},
		{"expiry day still open", "100", "2026-12-31", nil},
		{"expired", "100", "2027-01-01", []TacticalSignal{TacticalExpired}},
		{"stop and expired", "80", "2027-01-01", []TacticalSignal{TacticalStopHit, TacticalExpired}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := TacticalSignals(d(c.price), d("130"), d("90"), "2026-12-31", c.today)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
