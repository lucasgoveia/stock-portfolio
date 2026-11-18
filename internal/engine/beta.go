package engine

import (
	"math"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// PricePoint is a dated close.
type PricePoint struct {
	Date  time.Time
	Close decimal.Decimal
}

// BetaMinWeeks is the minimum number of weekly returns for an automatic beta
// (one year); below it the volatility factor must be rated by hand (§5.2).
const BetaMinWeeks = 52

// maxWeeklyMove drops weekly log returns beyond ±ln(1.5) on either side, which
// almost always come from unadjusted corporate actions (splits) in raw
// COTAHIST closes rather than from the market.
var maxWeeklyMove = math.Log(1.5)

// BetaResult is a weekly beta estimate.
type BetaResult struct {
	Beta  decimal.Decimal
	Weeks int // weekly returns used
	OK    bool
}

// weekKey groups dates by ISO week.
func weekKey(t time.Time) int {
	y, w := t.ISOWeek()
	return y*100 + w
}

// weeklyCloses keeps the last close of each ISO week.
func weeklyCloses(pts []PricePoint) map[int]float64 {
	sorted := append([]PricePoint(nil), pts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	out := map[int]float64{}
	for _, p := range sorted {
		if f, _ := p.Close.Float64(); f > 0 {
			out[weekKey(p.Date)] = f
		}
	}
	return out
}

// WeeklyBeta estimates beta = cov(asset, index) / var(index) from weekly log
// returns over the weeks both series share. Statistics use float64; the result
// is rounded to 4 decimals. OK is false with fewer than BetaMinWeeks returns.
func WeeklyBeta(asset, index []PricePoint) BetaResult {
	a, ix := weeklyCloses(asset), weeklyCloses(index)
	keys := make([]int, 0, len(a))
	for k := range a {
		if _, ok := ix[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Ints(keys)
	var ra, ri []float64
	for i := 1; i < len(keys); i++ {
		x := math.Log(a[keys[i]] / a[keys[i-1]])
		y := math.Log(ix[keys[i]] / ix[keys[i-1]])
		if math.Abs(x) > maxWeeklyMove || math.Abs(y) > maxWeeklyMove {
			continue
		}
		ra, ri = append(ra, x), append(ri, y)
	}
	n := len(ra)
	if n < BetaMinWeeks {
		return BetaResult{Beta: decimal.Zero, Weeks: n}
	}
	var ma, mi float64
	for i := range ra {
		ma += ra[i]
		mi += ri[i]
	}
	ma /= float64(n)
	mi /= float64(n)
	var cov, vari float64
	for i := range ra {
		cov += (ra[i] - ma) * (ri[i] - mi)
		vari += (ri[i] - mi) * (ri[i] - mi)
	}
	if vari == 0 {
		return BetaResult{Beta: decimal.Zero, Weeks: n}
	}
	return BetaResult{Beta: decimal.NewFromFloat(cov / vari).Round(4), Weeks: n, OK: true}
}

// VolatilityScore maps beta to the 1-5 volatility factor (§5.2). Buckets are
// half-open: <0.7 -> 5, [0.7,0.9) -> 4, [0.9,1.1) -> 3, [1.1,1.4] -> 2, >1.4 -> 1.
func VolatilityScore(beta decimal.Decimal) int {
	switch {
	case beta.LessThan(decimal.RequireFromString("0.7")):
		return 5
	case beta.LessThan(decimal.RequireFromString("0.9")):
		return 4
	case beta.LessThan(decimal.RequireFromString("1.1")):
		return 3
	case beta.LessThanOrEqual(decimal.RequireFromString("1.4")):
		return 2
	default:
		return 1
	}
}
