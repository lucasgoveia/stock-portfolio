package engine

import "github.com/shopspring/decimal"

// How US exposure is bought (planner setting us_vehicle).
const (
	USVehicleDirect = "direct" // US shares through a remittance, batched
	USVehicleBDR    = "bdr"    // the linked BDR on B3 when one exists
)

func mustDec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// Params holds every threshold of the rules. Percentages are fractions.
type Params struct {
	Weights Weights

	// Targets (§6).
	RawBase        decimal.Decimal // raw weight at score 1 (2%)
	RawSlope       decimal.Decimal // raw weight added per score point (2%)
	ClampMin       decimal.Decimal // 2%
	ClampMax       decimal.Decimal // 10%
	BandLowerMult  decimal.Decimal // 0.75
	BandUpperMult  decimal.Decimal // 1.25
	BandUpperCap   decimal.Decimal // 12%
	HardFloor      decimal.Decimal // 1.5%
	HardCap        decimal.Decimal // 12%
	BootstrapBelow decimal.Decimal // BRL portfolio value

	// Cash (§7).
	CashBase       decimal.Decimal // 4%
	CashSpan       decimal.Decimal // 16%
	OvervaluedMult decimal.Decimal // 1.3
	CashBandMin    decimal.Decimal // 4%
	CashBandMax    decimal.Decimal // 20%

	// Buy gate and opportunity (§8.1, §8.2).
	MinScore           decimal.Decimal // 2.5
	FairValueMaxMonths int             // 12
	OpportunityMult    decimal.Decimal // 0.8
	FairValueTolerance decimal.Decimal // 5%: core buys pass while price <= fair value x (1 + tolerance)

	// Planner (§8.3, §8.4).
	MinOrderB3 decimal.Decimal // R$300
	USBatchMin decimal.Decimal // R$2,000
	RampStages int             // 3
	// MaxNewPositions bounds how many positions a single plan opens (0: no limit).
	MaxNewPositions int // 2
	// USVehicle is how US exposure is bought: USVehicleDirect or USVehicleBDR.
	USVehicle string // bdr

	// Trim (§9).
	TrimFraction      decimal.Decimal // 25%
	CooldownDays      int             // 30
	BetterScoreDelta  decimal.Decimal // 0.5
	BetterUpsideDelta decimal.Decimal // 0.20 (20 percentage points)

	// Tactical (§10).
	TacticalMaxLoss decimal.Decimal // 1% of the portfolio lost at the stop
	TacticalMinSize decimal.Decimal // 1%
	TacticalMaxSize decimal.Decimal // 2%
	TacticalMaxOpen int             // 3; a 4th open tactical position warns
}

// DefaultParams returns the spec defaults.
func DefaultParams() Params {
	return Params{
		Weights:        DefaultWeights(),
		RawBase:        mustDec("0.02"),
		RawSlope:       mustDec("0.02"),
		ClampMin:       mustDec("0.02"),
		ClampMax:       mustDec("0.10"),
		BandLowerMult:  mustDec("0.75"),
		BandUpperMult:  mustDec("1.25"),
		BandUpperCap:   mustDec("0.12"),
		HardFloor:      mustDec("0.015"),
		HardCap:        mustDec("0.12"),
		BootstrapBelow: mustDec("10000"),

		CashBase:       mustDec("0.04"),
		CashSpan:       mustDec("0.16"),
		OvervaluedMult: mustDec("1.3"),
		CashBandMin:    mustDec("0.04"),
		CashBandMax:    mustDec("0.20"),

		MinScore:           mustDec("2.5"),
		FairValueMaxMonths: 12,
		OpportunityMult:    mustDec("0.8"),
		FairValueTolerance: mustDec("0.05"),

		MinOrderB3: mustDec("300"),
		USBatchMin: mustDec("2000"),
		RampStages: 3,

		MaxNewPositions: 2,
		USVehicle:       USVehicleBDR,

		TrimFraction:      mustDec("0.25"),
		CooldownDays:      30,
		BetterScoreDelta:  mustDec("0.5"),
		BetterUpsideDelta: mustDec("0.20"),

		TacticalMaxLoss: mustDec("0.01"),
		TacticalMinSize: mustDec("0.01"),
		TacticalMaxSize: mustDec("0.02"),
		TacticalMaxOpen: 3,
	}
}
