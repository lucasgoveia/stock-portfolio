package marketdata

import (
	"sort"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// maxQuarterFacts bounds how many 10-Q quarters are kept (three years).
const maxQuarterFacts = 12

// qPoint is a quarterly flow value with its fiscal quarter and filing date.
type qPoint struct {
	Val     money.Decimal
	Filed   string
	Quarter int
}

// quarterSeries returns the 10-Q flow facts of one concept, keyed by period
// end: the 3-month values and the fiscal-year-to-date values (6 and 9 months).
// The fiscal quarter comes from the filing's fp field (Q1 to Q3); the latest
// filing wins per period end. The first name that has a value for an end wins.
func (d *edgarDoc) quarterSeries(unit string, names ...string) (three, ytd map[string]qPoint) {
	three, ytd = map[string]qPoint{}, map[string]qPoint{}
	for _, name := range names {
		t, y := map[string]qPoint{}, map[string]qPoint{}
		for _, f := range d.Facts["us-gaap"][name].Units[unit] {
			if f.Start == "" || f.End == "" || !strings.HasPrefix(f.Form, "10-Q") {
				continue
			}
			q := 0
			switch f.FP {
			case "Q1":
				q = 1
			case "Q2":
				q = 2
			case "Q3":
				q = 3
			default:
				continue
			}
			s, err1 := time.Parse("2006-01-02", f.Start)
			e, err2 := time.Parse("2006-01-02", f.End)
			v, err3 := jsonDecimal(f.Val)
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			dst := map[string]qPoint(nil)
			switch durationClass(int(e.Sub(s).Hours() / 24)) {
			case durationQuarter:
				dst = t
				if q == 1 { // a Q1 three-month fact is also the year to date
					if cur, ok := y[f.End]; !ok || cur.Filed <= f.Filed {
						y[f.End] = qPoint{Val: v, Filed: f.Filed, Quarter: q}
					}
				}
			case durationYTD:
				dst = y
			default:
				continue
			}
			if cur, ok := dst[f.End]; ok && cur.Filed > f.Filed {
				continue
			}
			dst[f.End] = qPoint{Val: v, Filed: f.Filed, Quarter: q}
		}
		for end, p := range t {
			if _, ok := three[end]; !ok {
				three[end] = p
			}
		}
		for end, p := range y {
			if _, ok := ytd[end]; !ok {
				ytd[end] = p
			}
		}
	}
	return three, ytd
}

// quarters builds the Q1 to Q3 facts from 10-Q filings: revenue, net income,
// diluted EPS and diluted average shares for the 3-month period and the year
// to date. Q4 is not reported on its own; see DeriveFourthQuarters.
func (d *edgarDoc) quarters() []QuarterFact {
	byEnd := map[string]*QuarterFact{}
	add := func(concept string, three, ytd map[string]qPoint) {
		for end, p := range three {
			q, ok := byEnd[end]
			if !ok {
				e, err := time.Parse("2006-01-02", end)
				if err != nil {
					continue
				}
				q = &QuarterFact{End: e.UTC(), Quarter: p.Quarter, Values: map[string]money.Decimal{}, YTD: map[string]money.Decimal{}}
				byEnd[end] = q
			}
			if p.Filed > q.Filed {
				q.Filed = p.Filed
			}
			q.Values[concept] = p.Val
			if y, ok := ytd[end]; ok {
				q.YTD[concept] = y.Val
			}
		}
	}
	rev3, revY := d.quarterSeries("USD", "RevenueFromContractWithCustomerExcludingAssessedTax", "Revenues", "SalesRevenueNet",
		"RevenueFromContractWithCustomerIncludingAssessedTax", "SalesRevenueGoodsNet")
	ni3, niY := d.quarterSeries("USD", "NetIncomeLoss", "ProfitLoss")
	eps3, epsY := d.quarterSeries("USD/shares", "EarningsPerShareDiluted")
	sh3, _ := d.quarterSeries("shares", "WeightedAverageNumberOfDilutedSharesOutstanding")
	add(ConceptRevenue, rev3, revY)
	add(ConceptNetIncome, ni3, niY)
	add(ConceptEPSDiluted, eps3, epsY)
	add(ConceptSharesDilAvg, sh3, nil)

	out := make([]QuarterFact, 0, len(byEnd))
	for _, q := range byEnd {
		if _, ok := q.Values[ConceptRevenue]; ok {
			out = append(out, *q)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].End.Before(out[j].End) })
	if len(out) > maxQuarterFacts {
		out = out[len(out)-maxQuarterFacts:]
	}
	return out
}
