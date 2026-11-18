package engine

import (
	"github.com/shopspring/decimal"
)

// Expected-versus-actual math (spec §12.4). The base scenario of a scenario DCF
// version implies a revenue path and, with its terminal margin, an earnings
// path. Quarters are numbered from the end of the base fiscal year: quarter 1
// is the first quarter after it, quarter 20 the last of the 5-year horizon.
//
// Revenue: the annual path is R_k = base x (1+g_1) x ... x (1+g_k). A year is
// split into four quarters growing geometrically at the year's own rate,
// r = (1+g_k)^(1/4), so that they sum to R_k: quarter i of the year is
// R_k x r^(i-1) / (1 + r + r^2 + r^3). Splitting evenly (R_k / 4) would put
// every early quarter of a growing business below its expected value.
//
// EPS: the net margin moves linearly from the base year's net margin to the
// terminal margin over 5 years, m_k = m_0 + (m_T - m_0) x k / 5 for year k, and
// quarter earnings are quarter revenue x m_k, over the version's share count.

// ExpectedPathInput is the base scenario of one scenario DCF version.
type ExpectedPathInput struct {
	// BaseRevenue is the annual revenue of the base fiscal year (year 0).
	BaseRevenue decimal.Decimal
	// BaseNetMargin is the net margin of the base year (net income over
	// revenue). Nil leaves the expected EPS undefined.
	BaseNetMargin *decimal.Decimal
	// Shares is the share count the EPS is spread over; zero leaves EPS undefined.
	Shares decimal.Decimal
	// Base is the base scenario: yearly growth and terminal margin are used.
	Base Scenario
}

// ExpectedQuarter is the base scenario's expectation for one quarter.
type ExpectedQuarter struct {
	Quarter int // 1 to 20, counted from the base year end
	Year    int // 1 to 5
	Revenue decimal.Decimal
	// EPS is nil when the margin or share count is unavailable.
	EPS *decimal.Decimal
}

// ExpectedQuarters is how many quarters the 5-year horizon covers.
const ExpectedQuarters = DCFYears * 4

const expectedRevenuePlaces = 2

// ExpectedFor returns the expectation for quarter n (1 to 20) after the base
// year end.
func ExpectedFor(in ExpectedPathInput, n int) (ExpectedQuarter, error) {
	if !in.BaseRevenue.IsPositive() {
		return ExpectedQuarter{}, invalidf("base revenue must be positive")
	}
	if n < 1 || n > ExpectedQuarters {
		return ExpectedQuarter{}, invalidf("quarter %d is outside the %d-year path", n, DCFYears)
	}
	one := decimal.NewFromInt(1)
	year := (n-1)/4 + 1
	i := (n-1)%4 + 1 // quarter within the year

	annual := in.BaseRevenue
	for k := 0; k < year; k++ {
		g := in.Base.RevenueGrowth[k]
		if g.LessThanOrEqual(one.Neg()) {
			return ExpectedQuarter{}, invalidf("revenue growth must be greater than -100%%")
		}
		annual = annual.Mul(one.Add(g))
	}
	r := sqrt(sqrt(one.Add(in.Base.RevenueGrowth[year-1])))
	sum, pow, weight := decimal.Zero, one, one // pow is r^(j-1)
	for j := 1; j <= 4; j++ {
		sum = sum.Add(pow)
		if j == i {
			weight = pow
		}
		pow = pow.Mul(r)
	}
	rev := annual.Mul(weight).Div(sum)

	out := ExpectedQuarter{Quarter: n, Year: year, Revenue: rev.Round(expectedRevenuePlaces)}
	if in.BaseNetMargin != nil && in.Shares.IsPositive() {
		m0 := *in.BaseNetMargin
		margin := m0.Add(in.Base.TerminalMargin.Sub(m0).Mul(decimal.NewFromInt(int64(year))).Div(decimal.NewFromInt(DCFYears)))
		eps := rev.Mul(margin).Div(in.Shares).Round(DCFValuePrecision)
		out.EPS = &eps
	}
	return out, nil
}

// QuartersAfter is the number of quarters from a base year end to a quarter
// end, given the days between them (a quarter is 91.3125 days). Rounding makes
// 52/53-week fiscal calendars land on the right quarter.
func QuartersAfter(days int) int {
	q := decimal.NewFromInt(int64(days)).Div(decimal.RequireFromString("91.3125")).Round(0)
	return int(q.IntPart())
}

// Outcome of comparing one actual figure with its expectation.
type Outcome string

// Outcomes.
const (
	OutcomeMet  Outcome = "met"
	OutcomeMiss Outcome = "miss"
	OutcomeNA   Outcome = "na" // no expectation or no actual
)

// Compare checks an actual against its expectation. A miss is an actual below
// expected - tolerance x |expected|, so the tolerance (a fraction) also works
// for a negative expectation. Delta is (actual - expected) / |expected| and is
// nil when the expectation is zero.
func Compare(actual, expected, tolerance decimal.Decimal) (delta *decimal.Decimal, out Outcome) {
	if !expected.IsZero() {
		d := actual.Sub(expected).Div(expected.Abs()).Round(6)
		delta = &d
	}
	if actual.LessThan(expected.Sub(tolerance.Mul(expected.Abs()))) {
		return delta, OutcomeMiss
	}
	return delta, OutcomeMet
}

// MissStreak is the number of consecutive misses at the end of outcomes, which
// are ordered oldest to newest. A quarter without an outcome (n/a) ends the
// streak, as does one that was met.
func MissStreak(outcomes []Outcome) int {
	n := 0
	for i := len(outcomes) - 1; i >= 0 && outcomes[i] == OutcomeMiss; i-- {
		n++
	}
	return n
}
