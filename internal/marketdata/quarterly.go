package marketdata

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// QuarterFact holds the income statement figures of one fiscal quarter. Values
// are the three-month figures; YTD are the fiscal-year-to-date figures as
// reported (for Q1 they equal Values). Concepts used: ConceptRevenue,
// ConceptNetIncome, ConceptEPSDiluted, ConceptSharesDilAvg or ConceptShares.
type QuarterFact struct {
	End     time.Time // civil date, UTC midnight
	Quarter int       // fiscal quarter, 1 to 4
	Filed   string    // filing date, or the document version when unknown
	Values  map[string]money.Decimal
	YTD     map[string]money.Decimal
	// Derived marks a Q4 computed as the annual figure minus the Q3 year to date.
	Derived bool
}

// quarterFlows are the concepts carried by quarterly facts.
var quarterFlows = []string{ConceptRevenue, ConceptNetIncome, ConceptEPSDiluted}

// Duration classes of a flow fact.
const (
	durationOther = iota
	durationQuarter
	durationYTD
)

// durationClass classifies a duration in days: three months, or a 6 to 9 month
// year-to-date stretch. Anything else (a full year, an odd stub) is other.
func durationClass(days int) int {
	switch {
	case days >= 80 && days <= 100:
		return durationQuarter
	case days >= 170 && days <= 290:
		return durationYTD
	}
	return durationOther
}

// DeriveFourthQuarters adds a Q4 for every annual period that has a Q3 with a
// year-to-date figure three months before the year end: Q4 = annual minus the
// Q3 year to date, for revenue, net income and diluted EPS. When the annual
// period has no EPS but has net income and a share count, the Q4 EPS is the Q4
// net income over those shares. A Q4 that already exists is left alone.
func DeriveFourthQuarters(annual []FinancialPeriod, quarters []QuarterFact) []QuarterFact {
	out := append([]QuarterFact(nil), quarters...)
	have := map[string]bool{}
	for _, q := range quarters {
		have[q.End.Format("2006-01-02")] = true
	}
	for _, a := range annual {
		if a.Type != PeriodAnnual || have[a.End.Format("2006-01-02")] {
			continue
		}
		var q3 *QuarterFact
		for i := range quarters {
			q := &quarters[i]
			if q.Quarter != 3 {
				continue
			}
			gap := a.End.Sub(q.End).Hours() / 24
			if gap >= 60 && gap <= 125 {
				q3 = q
				break
			}
		}
		if q3 == nil {
			continue
		}
		d := QuarterFact{End: a.End, Quarter: 4, Filed: a.Filed, Derived: true,
			Values: map[string]money.Decimal{}, YTD: map[string]money.Decimal{}}
		for _, c := range quarterFlows {
			av, aok := a.Values[c]
			yv, yok := q3.YTD[c]
			if aok && yok {
				d.Values[c] = money.New(av.Sub(yv.Decimal))
			}
			if aok {
				d.YTD[c] = av
			}
		}
		if _, ok := d.Values[ConceptRevenue]; !ok {
			continue
		}
		if sh, ok := a.Values[ConceptSharesDilAvg]; ok {
			d.Values[ConceptSharesDilAvg] = sh
		} else if sh, ok := a.Values[ConceptShares]; ok {
			d.Values[ConceptShares] = sh
		}
		if _, ok := d.Values[ConceptEPSDiluted]; !ok {
			if ni, ok := d.Values[ConceptNetIncome]; ok {
				if sh, ok := quarterShares(d.Values); ok {
					d.Values[ConceptEPSDiluted] = money.New(ni.Div(sh).Round(6))
				}
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].End.Before(out[j].End) })
	return out
}

func quarterShares(v map[string]money.Decimal) (decimal.Decimal, bool) {
	for _, c := range []string{ConceptSharesDilAvg, ConceptShares} {
		if s, ok := v[c]; ok && s.IsPositive() {
			return s.Decimal, true
		}
	}
	return decimal.Zero, false
}
