package engine

import "github.com/shopspring/decimal"

// FlowPoint is one day of a value series: Value at the end of the day and
// the external Flow that day (money in positive, out negative).
type FlowPoint struct {
	Date  string
	Value decimal.Decimal
	Flow  decimal.Decimal
}

// IndexPoint is a cumulative growth index (1 = start).
type IndexPoint struct {
	Date  string          `json:"date"`
	Index decimal.Decimal `json:"index"`
}

// TWR chains daily sub-period returns (spec §15): flows are assumed at the
// end of the day, so r = (V_t - F_t) / V_{t-1} - 1. On a day that starts
// from zero the inflow is the capital: r = V_t / F_t - 1. Dividends are
// internal: they must not appear in Flow.
func TWR(points []FlowPoint) []IndexPoint {
	out := make([]IndexPoint, 0, len(points))
	idx := decimal.NewFromInt(1)
	prev := zero
	for _, p := range points {
		switch {
		case prev.IsPositive():
			idx = idx.Mul(p.Value.Sub(p.Flow).Div(prev)).Round(10)
		case p.Flow.IsPositive():
			// Starting from nothing: the day's capital is its inflow, so the
			// move from trade price to close still counts.
			idx = idx.Mul(p.Value.Div(p.Flow)).Round(10)
		}
		out = append(out, IndexPoint{Date: p.Date, Index: idx})
		prev = p.Value
	}
	return out
}

// CDIIndex compounds daily CDI rates (percent per day) into an index.
func CDIIndex(dates []string, ratePct []decimal.Decimal) []IndexPoint {
	out := make([]IndexPoint, 0, len(dates))
	idx := decimal.NewFromInt(1)
	hundred := decimal.NewFromInt(100)
	for i, d := range dates {
		if i > 0 && i < len(ratePct) {
			idx = idx.Mul(decimal.NewFromInt(1).Add(ratePct[i].Div(hundred))).Round(10)
		}
		out = append(out, IndexPoint{Date: d, Index: idx})
	}
	return out
}

// PriceIndex rebases a price series to 1 at its first point.
func PriceIndex(dates []string, closes []decimal.Decimal) []IndexPoint {
	out := make([]IndexPoint, 0, len(dates))
	if len(closes) == 0 || !closes[0].IsPositive() {
		return out
	}
	for i, d := range dates {
		if i < len(closes) {
			out = append(out, IndexPoint{Date: d, Index: closes[i].Div(closes[0]).Round(10)})
		}
	}
	return out
}
