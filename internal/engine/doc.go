// Package engine holds the pure, deterministic rule math of the portfolio
// system (spec §5-§11): scoring, targets and bands, dynamic cash target, buy
// gate, contribution planner and trim engine.
//
// The package performs no I/O and never reads the clock: every date is passed
// in. All percentages and amounts are shopspring decimals; percentages are
// fractions (0.02 means 2%). Every function takes plain input structs so the
// caller (daily job, API) maps its own storage types.
package engine
