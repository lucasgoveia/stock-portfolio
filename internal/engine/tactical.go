package engine

import "github.com/shopspring/decimal"

// TacticalSizing is the entry size of a tactical position (spec §10.1).
type TacticalSizing struct {
	// StopDistance is (entry - stop) / entry.
	StopDistance decimal.Decimal `json:"stop_distance"`
	// Size is the position as a fraction of the portfolio:
	// clamp(max loss / stop distance, min size, max size).
	Size decimal.Decimal `json:"size"`
	// LossAtStop is Size x StopDistance, the portfolio fraction lost at the stop.
	LossAtStop decimal.Decimal `json:"loss_at_stop"`
}

// TacticalSize sizes a tactical entry so the loss at the stop is at most
// TacticalMaxLoss of the portfolio, within [TacticalMinSize, TacticalMaxSize].
// It needs 0 < stop < entry.
func (p Params) TacticalSize(entry, stop decimal.Decimal) (TacticalSizing, error) {
	if !entry.IsPositive() || !stop.IsPositive() {
		return TacticalSizing{}, invalidf("entry and stop must be positive")
	}
	if !stop.LessThan(entry) {
		return TacticalSizing{}, invalidf("stop must be below entry")
	}
	dist := entry.Sub(stop).Div(entry)
	size := p.TacticalMaxLoss.Div(dist)
	if size.LessThan(p.TacticalMinSize) {
		size = p.TacticalMinSize
	}
	if size.GreaterThan(p.TacticalMaxSize) {
		size = p.TacticalMaxSize
	}
	return TacticalSizing{StopDistance: dist, Size: size, LossAtStop: size.Mul(dist)}, nil
}

// TacticalSignal is a tactical alert condition (spec §10.2).
type TacticalSignal string

// Tactical signals; each maps to the alert type of the same name.
const (
	TacticalTargetHit TacticalSignal = "tactical_target_hit"
	TacticalStopHit   TacticalSignal = "tactical_stop_hit"
	TacticalExpired   TacticalSignal = "tactical_expired"
)

// TacticalSignals reports which exits fired for a held tactical position.
// Dates are YYYY-MM-DD strings; expiry fires the day after expiresOn.
func TacticalSignals(price, target, stop decimal.Decimal, expiresOn, today string) []TacticalSignal {
	var out []TacticalSignal
	if price.GreaterThanOrEqual(target) {
		out = append(out, TacticalTargetHit)
	}
	if price.LessThanOrEqual(stop) {
		out = append(out, TacticalStopHit)
	}
	if today > expiresOn {
		out = append(out, TacticalExpired)
	}
	return out
}
