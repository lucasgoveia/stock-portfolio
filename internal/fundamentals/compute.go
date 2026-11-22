package fundamentals

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/lucasgoveia/stock-portfolio/internal/engine"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// Decimal places of derived figures.
const (
	multiplePlaces = 2 // P/E, EV/EBIT, net debt/EBITDA
	ratioPlaces    = 4 // yields and margins as fractions
	perSharePlaces = 4
	amountPlaces   = 2
)

// priceWindowDays is how far before a fiscal year-end a stored close may be
// to stand for the price on that date.
const priceWindowDays = 10

// MaxHistory is how many fiscal years the history and ranges cover.
const MaxHistory = 5

// DefaultTerminalMargin applies to reverse DCF when no margin is known.
var DefaultTerminalMargin = decimal.RequireFromString("0.15")

// DefaultExitMultiple is the reverse DCF exit multiple (EV/EBIT).
var DefaultExitMultiple = decimal.NewFromInt(10)

// DefaultDiscountRate returns the reverse DCF discount rate for a market.
func DefaultDiscountRate(market string) decimal.Decimal {
	if market == "US" {
		return decimal.RequireFromString("0.10")
	}
	return decimal.RequireFromString("0.14")
}

// Params are the editable inputs of the helpers. Nil fields use defaults.
type Params struct {
	BazinYield     *decimal.Decimal
	TerminalMargin *decimal.Decimal
	ExitMultiple   *decimal.Decimal
	DiscountRate   *decimal.Decimal
}

// period is one stored period with its concepts.
type period struct {
	End  string // YYYY-MM-DD
	Type string // annual, quarterly or ttm
	V    map[string]decimal.Decimal
}

// pricePoint is a close on a date.
type pricePoint struct {
	Date  string
	Close decimal.Decimal
}

// computeInput is everything the pure computation needs.
type computeInput struct {
	Market  string
	Periods []period // ascending by End
	Price   *pricePoint
	// PriceOn returns the close on or shortly before a date.
	PriceOn func(end string) (decimal.Decimal, bool)
	// LocalTTM is trailing dividends per share from the local dividends table,
	// used when no feed value is stored.
	LocalTTM decimal.Decimal
	Params   Params
	// CAPMRate is the asset's CAPM cost of equity, the default discount rate;
	// nil falls back to DefaultDiscountRate.
	CAPMRate *decimal.Decimal
}

// computed is the pure output.
type computed struct {
	FundamentalMetrics FundamentalMetrics
	History            []HistoryPoint
	Ranges             Ranges
	Helpers            Helpers
	Notes              []string
	// Raw inputs the draft builder reuses.
	DPS         decimal.Decimal
	DPSNote     string
	EPS, BVPS   decimal.Decimal
	HaveEPSBVPS bool
}

func dp(d decimal.Decimal, places int32) *money.Decimal {
	v := money.New(d.Round(places))
	return &v
}

func (in computeInput) latest(concept string) (decimal.Decimal, string, bool) {
	for i := len(in.Periods) - 1; i >= 0; i-- {
		p := in.Periods[i]
		if p.Type == marketdata.PeriodTTM {
			continue
		}
		if v, ok := p.V[concept]; ok {
			return v, p.End, true
		}
	}
	return decimal.Zero, "", false
}

// latestAnnual returns the newest annual period holding every concept.
func (in computeInput) latestAnnual(concepts ...string) (period, bool) {
	for i := len(in.Periods) - 1; i >= 0; i-- {
		p := in.Periods[i]
		if p.Type != marketdata.PeriodAnnual {
			continue
		}
		all := true
		for _, c := range concepts {
			if _, ok := p.V[c]; !ok {
				all = false
				break
			}
		}
		if all {
			return p, true
		}
	}
	return period{}, false
}

func (in computeInput) annuals() []period {
	var out []period
	for _, p := range in.Periods {
		if p.Type == marketdata.PeriodAnnual {
			out = append(out, p)
		}
	}
	return out
}

// currentShares prefers the filing cover count, then the latest period-end
// count, then the latest diluted average.
func (in computeInput) currentShares() (decimal.Decimal, bool) {
	for _, c := range []string{marketdata.ConceptSharesCover, marketdata.ConceptShares, marketdata.ConceptSharesDilAvg} {
		if v, _, ok := in.latest(c); ok && v.IsPositive() {
			return v, true
		}
	}
	return decimal.Zero, false
}

// periodShares is the share count that goes with a period's earnings.
func periodShares(p period) (decimal.Decimal, bool) {
	for _, c := range []string{marketdata.ConceptSharesDilAvg, marketdata.ConceptShares} {
		if v, ok := p.V[c]; ok && v.IsPositive() {
			return v, true
		}
	}
	return decimal.Zero, false
}

func ratio(num, den decimal.Decimal) (decimal.Decimal, bool) {
	if !den.IsPositive() {
		return decimal.Zero, false
	}
	return num.Div(den), true
}

// compute derives metrics, history, ranges and helper values.
func compute(in computeInput) computed {
	var out computed
	note := func(format string, a ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, a...)) }

	annual, haveAnnual := in.latestAnnual()
	if !haveAnnual {
		note("no annual figures stored")
		return out
	}
	m := &out.FundamentalMetrics
	m.FiscalYearEnd = annual.End

	shares, haveShares := in.currentShares()
	if haveShares {
		m.Shares = dp(shares, 0)
	} else {
		note("share count unavailable: market cap, per-share values and multiples are not computed")
	}
	var price decimal.Decimal
	havePrice := in.Price != nil && in.Price.Close.IsPositive()
	if havePrice {
		price = in.Price.Close
		m.Price = dp(price, perSharePlaces)
		m.PriceDate = in.Price.Date
	} else {
		note("no stored price: market multiples are not computed")
	}

	// Income statement figures of the latest fiscal year.
	revenue, haveRev := annual.V[marketdata.ConceptRevenue]
	ebit, haveEBIT := annual.V[marketdata.ConceptEBIT]
	ni, haveNI := annual.V[marketdata.ConceptNetIncome]
	if haveRev {
		m.Revenue = dp(revenue, amountPlaces)
	}
	if haveEBIT {
		m.EBIT = dp(ebit, amountPlaces)
	}
	if haveNI {
		m.NetIncome = dp(ni, amountPlaces)
	}

	// Balance sheet: the newest values, annual or quarterly.
	debt, _, haveDebt := in.latest(marketdata.ConceptDebt)
	cash, _, haveCash := in.latest(marketdata.ConceptCash)
	if !haveDebt {
		debt = decimal.Zero
		note("no financial debt reported (taken as 0)")
	}
	if !haveCash {
		cash = decimal.Zero
		note("no cash balance reported (taken as 0)")
	}
	netDebt := debt.Sub(cash)
	m.NetDebt = dp(netDebt, amountPlaces)

	// EPS: reported diluted EPS, otherwise net income over the period's shares.
	var eps decimal.Decimal
	haveEPS := false
	if v, ok := annual.V[marketdata.ConceptEPSDiluted]; ok {
		eps, haveEPS = v, true
	} else if haveNI {
		if s, ok := periodShares(annual); ok {
			eps, haveEPS = ni.Div(s), true
		} else if haveShares {
			eps, haveEPS = ni.Div(shares), true
		}
	}
	if haveEPS {
		m.EPS = dp(eps, perSharePlaces)
	}

	var mcap decimal.Decimal
	if haveShares && havePrice {
		mcap = price.Mul(shares)
		m.MarketCap = dp(mcap, amountPlaces)
		ev := mcap.Add(netDebt)
		m.EnterpriseValue = dp(ev, amountPlaces)
		if haveEBIT {
			if v, ok := ratio(ev, ebit); ok {
				m.EVEBIT = dp(v, multiplePlaces)
			} else {
				note("EBIT is not positive: EV/EBIT is undefined")
			}
		}
	}
	if haveEPS && havePrice {
		if v, ok := ratio(price, eps); ok {
			m.PE = dp(v, multiplePlaces)
		} else {
			note("EPS is not positive: P/E is undefined")
		}
	}

	// Free cash flow.
	ocf, haveOCF := annual.V[marketdata.ConceptOCF]
	capex, haveCapex := annual.V[marketdata.ConceptCapex]
	if haveOCF && haveCapex {
		fcf := ocf.Sub(capex.Abs())
		m.FCF = dp(fcf, amountPlaces)
		if mcap.IsPositive() {
			m.FCFYield = dp(fcf.Div(mcap), ratioPlaces)
		}
	} else if haveOCF {
		note("capex unavailable: free cash flow and FCF yield are not computed")
	}

	// EBITDA and leverage.
	if da, ok := annual.V[marketdata.ConceptDA]; ok && haveEBIT {
		ebitda := ebit.Add(da)
		m.EBITDA = dp(ebitda, amountPlaces)
		if ebitda.IsPositive() {
			m.NetDebtEBITDA = dp(netDebt.Div(ebitda), multiplePlaces)
		}
	} else if haveEBIT {
		note("depreciation and amortization unavailable: EBITDA and net debt/EBITDA are not computed")
	}

	// Dividends per share: stored trailing value, then the latest fiscal
	// year's declared dividends, then the local dividends table.
	dps, dpsNote := in.dividendsPerShare(annual)
	out.DPS, out.DPSNote = dps, dpsNote
	if dps.IsPositive() {
		m.DividendsPerShare = dp(dps, perSharePlaces)
		if havePrice {
			m.DividendYield = dp(dps.Div(price), ratioPlaces)
		}
	}

	// Book value per share.
	var bvps decimal.Decimal
	haveBVPS := false
	if eq, _, ok := in.latest(marketdata.ConceptEquity); ok && haveShares {
		bvps, haveBVPS = eq.Div(shares), true
		m.BVPS = dp(bvps, perSharePlaces)
	}
	if haveEPS && haveBVPS {
		out.EPS, out.BVPS, out.HaveEPSBVPS = eps, bvps, true
	}

	out.History = in.history()
	out.Ranges = in.ranges(m)
	out.Helpers = in.helpers(&out, havePrice, price, haveShares, shares, netDebt, revenue, haveRev, ebit, haveEBIT, ni, haveNI)
	return out
}

// dividendsPerShare returns the trailing 12-month dividends per share and a
// short description of where it came from.
func (in computeInput) dividendsPerShare(annual period) (decimal.Decimal, string) {
	for i := len(in.Periods) - 1; i >= 0; i-- {
		p := in.Periods[i]
		if p.Type != marketdata.PeriodTTM {
			continue
		}
		if v, ok := p.V[marketdata.ConceptDPS]; ok {
			return v, "trailing 12 months to " + p.End
		}
	}
	if v, ok := annual.V[marketdata.ConceptDPS]; ok {
		return v, "last fiscal year (" + annual.End + ")"
	}
	if in.LocalTTM.IsPositive() {
		return in.LocalTTM, "trailing 12 months of dividends recorded in the app"
	}
	return decimal.Zero, ""
}

// history returns up to MaxHistory fiscal years, oldest first.
func (in computeInput) history() []HistoryPoint {
	ann := in.annuals()
	if len(ann) > MaxHistory {
		ann = ann[len(ann)-MaxHistory:]
	}
	out := make([]HistoryPoint, 0, len(ann))
	for _, p := range ann {
		h := HistoryPoint{PeriodEnd: p.End}
		rev, haveRev := p.V[marketdata.ConceptRevenue]
		if haveRev {
			h.Revenue = dp(rev, amountPlaces)
		}
		if v, ok := p.V[marketdata.ConceptEBIT]; ok {
			h.EBIT = dp(v, amountPlaces)
			if haveRev && rev.IsPositive() {
				h.EBITMargin = dp(v.Div(rev), ratioPlaces)
			}
		}
		if v, ok := p.V[marketdata.ConceptNetIncome]; ok {
			h.NetIncome = dp(v, amountPlaces)
			if haveRev && rev.IsPositive() {
				h.NetMargin = dp(v.Div(rev), ratioPlaces)
			}
		}
		out = append(out, h)
	}
	return out
}

// ranges builds current-versus-history bars for P/E and EV/EBIT from the
// stored price at each fiscal year-end times that year's share count.
func (in computeInput) ranges(m *FundamentalMetrics) Ranges {
	var pe, ev []RangePoint
	ann := in.annuals()
	if len(ann) > MaxHistory {
		ann = ann[len(ann)-MaxHistory:]
	}
	for _, p := range ann {
		if in.PriceOn == nil {
			break
		}
		price, ok := in.PriceOn(p.End)
		shares, okS := periodShares(p)
		if !ok || !okS {
			continue
		}
		mcap := price.Mul(shares)
		if ni, ok := p.V[marketdata.ConceptNetIncome]; ok && ni.IsPositive() {
			pe = append(pe, RangePoint{Date: p.End, Value: *dp(mcap.Div(ni), multiplePlaces)})
		}
		if ebit, ok := p.V[marketdata.ConceptEBIT]; ok && ebit.IsPositive() {
			debt, cash := p.V[marketdata.ConceptDebt], p.V[marketdata.ConceptCash]
			ev = append(ev, RangePoint{Date: p.End, Value: *dp(mcap.Add(debt).Sub(cash).Div(ebit), multiplePlaces)})
		}
	}
	return Ranges{PE: buildRange(m.PE, pe), EVEBIT: buildRange(m.EVEBIT, ev)}
}

// buildRange needs at least 2 historical points for min and max; with fewer
// only the current value is shown.
func buildRange(current *money.Decimal, pts []RangePoint) *Range {
	if current == nil && len(pts) == 0 {
		return nil
	}
	r := &Range{Current: current, Points: pts}
	if len(pts) < 2 || current == nil {
		return r
	}
	lo, hi := current.Decimal, current.Decimal
	for _, p := range pts {
		if p.Value.LessThan(lo) {
			lo = p.Value.Decimal
		}
		if p.Value.GreaterThan(hi) {
			hi = p.Value.Decimal
		}
	}
	mn, mx := money.New(lo), money.New(hi)
	r.Min, r.Max = &mn, &mx
	if hi.GreaterThan(lo) {
		r.Position = dp(current.Sub(lo).Div(hi.Sub(lo)), ratioPlaces)
	} else {
		r.Position = dp(decimal.NewFromFloat(0.5), ratioPlaces)
	}
	return r
}

// helpers evaluates Bazin, Graham and reverse DCF.
func (in computeInput) helpers(c *computed, havePrice bool, price decimal.Decimal, haveShares bool, shares, netDebt, revenue decimal.Decimal, haveRev bool, ebit decimal.Decimal, haveEBIT bool, ni decimal.Decimal, haveNI bool) Helpers {
	var h Helpers
	yield := engine.DefaultBazinYield
	if in.Params.BazinYield != nil {
		yield = *in.Params.BazinYield
	}
	h.Bazin = &HelperValue{Method: "bazin", RequiredYield: dp(yield, ratioPlaces)}
	if v, ok := engine.Bazin(c.DPS, yield); ok {
		h.Bazin.Value = &money.Decimal{Decimal: v}
		h.Bazin.Note = "dividends per share, " + c.DPSNote + ", over the required yield"
	} else {
		h.Bazin.Note = "needs positive dividends per share and a positive yield"
	}

	h.Graham = &HelperValue{Method: "graham"}
	if c.HaveEPSBVPS {
		if v, ok := engine.Graham(c.EPS, c.BVPS); ok {
			h.Graham.Value = &money.Decimal{Decimal: v}
			h.Graham.Note = "sqrt(22.5 x EPS x book value per share)"
		}
	}
	if h.Graham.Value == nil {
		h.Graham.Note = "undefined: needs positive EPS and book value per share"
	}

	// Reverse DCF defaults come from the latest fiscal year.
	// The basis follows the margin: an EBIT margin is read with EV/EBIT and the
	// net debt subtracted (ebit_ev); a net margin with P/E and no net debt
	// subtracted (net_pe).
	margin, basis := DefaultTerminalMargin, engine.BasisEBITEV
	switch {
	case haveRev && revenue.IsPositive() && haveEBIT && ebit.IsPositive():
		margin = ebit.Div(revenue).Round(ratioPlaces)
	case haveRev && revenue.IsPositive() && haveNI && ni.IsPositive():
		margin, basis = ni.Div(revenue).Round(ratioPlaces), engine.BasisNetPE
	}
	exit, disc, discSrc := DefaultExitMultiple, DefaultDiscountRate(in.Market), "default"
	if in.CAPMRate != nil {
		disc, discSrc = *in.CAPMRate, "capm"
	}
	if in.Params.TerminalMargin != nil {
		margin = *in.Params.TerminalMargin
	}
	if in.Params.ExitMultiple != nil {
		exit = *in.Params.ExitMultiple
	}
	if in.Params.DiscountRate != nil {
		disc, discSrc = *in.Params.DiscountRate, "custom"
	}
	rd := &ReverseDCF{
		Basis: basis, TerminalMargin: *dp(margin, ratioPlaces), ExitMultiple: *dp(exit, multiplePlaces), DiscountRate: *dp(disc, ratioPlaces),
		DiscountSource: discSrc, Status: "unavailable",
	}
	h.ReverseDCF = rd
	switch {
	case !havePrice || !haveShares:
		rd.Note = "needs a stored price and a share count"
	case !haveRev || !revenue.IsPositive():
		rd.Note = "needs positive annual revenue"
	default:
		res, err := engine.ReverseDCF(engine.ReverseDCFInput{
			Price: price, BaseRevenue: revenue, NetDebt: netDebt, Shares: shares, Basis: basis,
			TerminalMargin: margin, ExitMultiple: exit, DiscountRate: disc,
		})
		if err != nil {
			rd.Note = err.Error()
			break
		}
		rd.Status = res.Status
		rd.ImpliedGrowth = &money.Decimal{Decimal: res.ImpliedGrowth}
		switch res.Status {
		case engine.ReverseBelowRange:
			rd.Note = "the price is below what even a 50% yearly decline in revenue would explain with these inputs"
		case engine.ReverseAboveRange:
			rd.Note = "the price is above what 150% yearly growth would explain with these inputs"
		default:
			rd.Note = "constant yearly revenue growth over 5 years that justifies today's price; informational, not a fair value"
		}
	}
	return h
}

// sortPeriods orders periods ascending by end, then type.
func sortPeriods(p []period) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].End != p[j].End {
			return p[i].End < p[j].End
		}
		return p[i].Type < p[j].Type
	})
}

// daysBefore returns the date n days before the civil date s.
func daysBefore(s string, n int) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.AddDate(0, 0, -n).Format("2006-01-02")
}

// ParseParams reads helper inputs given as decimal strings (fractions for
// yield, margin and rate); an empty string keeps the default.
func ParseParams(bazinYield, terminalMargin, exitMultiple, discountRate string) (Params, error) {
	var p Params
	for _, f := range []struct {
		name string
		raw  string
		dst  **decimal.Decimal
	}{
		{"bazin_yield", bazinYield, &p.BazinYield}, {"terminal_margin", terminalMargin, &p.TerminalMargin},
		{"exit_multiple", exitMultiple, &p.ExitMultiple}, {"discount_rate", discountRate, &p.DiscountRate},
	} {
		if f.raw == "" {
			continue
		}
		v, err := decimal.NewFromString(f.raw)
		if err != nil {
			return Params{}, validationf("%s must be a decimal number", f.name)
		}
		if !v.IsPositive() {
			return Params{}, validationf("%s must be positive", f.name)
		}
		*f.dst = &v
	}
	return p, nil
}
