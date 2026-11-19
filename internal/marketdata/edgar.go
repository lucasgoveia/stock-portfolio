package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// SEC EDGAR endpoints.
const (
	DefaultEDGARDataURL = "https://data.sec.gov"
	DefaultEDGARWWWURL  = "https://www.sec.gov"
	// DefaultSECUserAgent is a placeholder: SEC requires a descriptive agent
	// with a contact, so operators should set sec_user_agent.
	DefaultSECUserAgent = "stock-portfolio admin@example.com"
)

// EDGAR reads XBRL company facts and the ticker to CIK map from the SEC.
// Verified live (docs/data-sources.md §7). A request without a User-Agent gets
// HTTP 200 with an HTML block page, which the shared JSON helper rejects by
// content type.
type EDGAR struct {
	HTTP      *http.Client
	DataURL   string
	WWWURL    string
	UserAgent string
}

// NewEDGAR builds a client; empty arguments use the defaults.
func NewEDGAR(c *http.Client, dataURL, wwwURL, userAgent string) *EDGAR {
	if dataURL == "" {
		dataURL = DefaultEDGARDataURL
	}
	if wwwURL == "" {
		wwwURL = DefaultEDGARWWWURL
	}
	if userAgent == "" {
		userAgent = DefaultSECUserAgent
	}
	return &EDGAR{
		HTTP: httpClient(c), DataURL: strings.TrimRight(dataURL, "/"), WWWURL: strings.TrimRight(wwwURL, "/"),
		UserAgent: userAgent,
	}
}

func (e *EDGAR) headers() map[string]string {
	return map[string]string{"User-Agent": e.UserAgent, "Accept": "application/json"}
}

// FormatCIK returns s as a zero-padded 10-digit CIK, or "" when invalid.
func FormatCIK(s string) string {
	d := digitsOnly(s)
	if d == "" || len(d) > 10 {
		return ""
	}
	n, err := strconv.ParseInt(d, 10, 64)
	if err != nil || n == 0 {
		return ""
	}
	return fmt.Sprintf("%010d", n)
}

// CIKForTicker resolves a ticker through company_tickers.json. Class
// separators differ (BRK.B here, BRK-B at the SEC), so both are tried.
func (e *EDGAR) CIKForTicker(ctx context.Context, ticker string) (string, error) {
	var m map[string]struct {
		CIK    json.Number `json:"cik_str"`
		Ticker string      `json:"ticker"`
	}
	if err := getJSON(ctx, e.HTTP, "sec", e.WWWURL+"/files/company_tickers.json", e.headers(), &m); err != nil {
		return "", err
	}
	want := strings.ToUpper(strings.TrimSpace(ticker))
	alt := strings.ReplaceAll(want, ".", "-")
	for _, row := range m {
		if t := strings.ToUpper(row.Ticker); t == want || t == alt {
			if cik := FormatCIK(row.CIK.String()); cik != "" {
				return cik, nil
			}
		}
	}
	return "", fmt.Errorf("marketdata: sec: ticker %q not found in company_tickers.json", ticker)
}

type edgarFact struct {
	Start string      `json:"start"`
	End   string      `json:"end"`
	Val   json.Number `json:"val"`
	Form  string      `json:"form"`
	Filed string      `json:"filed"`
	FP    string      `json:"fp"` // fiscal period of the filing: Q1, Q2, Q3 or FY
}

type edgarConcept struct {
	Units map[string][]edgarFact `json:"units"`
}

type edgarDoc struct {
	EntityName string                             `json:"entityName"`
	Facts      map[string]map[string]edgarConcept `json:"facts"`
}

// edgarPoint is one deduplicated fact value.
type edgarPoint struct {
	Val    money.Decimal
	Filed  string
	Annual bool // from a 10-K (instants only)
}

// isAnnualForm reports whether form is an annual report: 10-K for domestic
// filers, 20-F or 40-F for foreign private issuers (amendments such as 10-K/A
// match by prefix).
func isAnnualForm(form string) bool {
	return strings.HasPrefix(form, "10-K") || strings.HasPrefix(form, "20-F") || strings.HasPrefix(form, "40-F")
}

// series returns the facts of one concept keyed by period end. Flow concepts
// keep only full-year durations from annual filings; instant concepts keep
// annual and 10-Q values. The latest filing wins per end date (restatements).
func (d *edgarDoc) series(ns, name, unit string, flow bool) map[string]edgarPoint {
	out := map[string]edgarPoint{}
	for _, f := range d.Facts[ns][name].Units[unit] {
		if f.End == "" {
			continue
		}
		annualForm := isAnnualForm(f.Form)
		if flow {
			if f.Start == "" || !annualForm {
				continue
			}
			s, err1 := time.Parse("2006-01-02", f.Start)
			en, err2 := time.Parse("2006-01-02", f.End)
			if err1 != nil || err2 != nil {
				continue
			}
			if days := int(en.Sub(s).Hours() / 24); days < 340 || days > 380 {
				continue
			}
		} else if !annualForm && !strings.HasPrefix(f.Form, "10-Q") {
			continue
		}
		v, err := jsonDecimal(f.Val)
		if err != nil {
			continue
		}
		if cur, ok := out[f.End]; ok && cur.Filed > f.Filed {
			continue
		}
		out[f.End] = edgarPoint{Val: v, Filed: f.Filed, Annual: annualForm}
	}
	return out
}

// firstOf merges several concepts of one namespace into one series; earlier
// names win per end.
func (d *edgarDoc) firstOf(ns, unit string, flow bool, names ...string) map[string]edgarPoint {
	out := map[string]edgarPoint{}
	for _, n := range names {
		for end, p := range d.series(ns, n, unit, flow) {
			if _, ok := out[end]; !ok {
				out[end] = p
			}
		}
	}
	return out
}

const (
	nsUSGAAP = "us-gaap"
	nsIFRS   = "ifrs-full"
)

// annualFactCount counts the annual-form facts of a namespace.
func (d *edgarDoc) annualFactCount(ns string) int {
	n := 0
	for _, c := range d.Facts[ns] {
		for _, fs := range c.Units {
			for _, f := range fs {
				if isAnnualForm(f.Form) {
					n++
				}
			}
		}
	}
	return n
}

// foreignFiler reports whether ns has facts from a 20-F or 40-F filing.
func (d *edgarDoc) foreignFiler(ns string) bool {
	for _, c := range d.Facts[ns] {
		for _, fs := range c.Units {
			for _, f := range fs {
				if strings.HasPrefix(f.Form, "20-F") || strings.HasPrefix(f.Form, "40-F") {
					return true
				}
			}
		}
	}
	return false
}

// pickNamespace chooses the taxonomy with more annual facts; us-gaap wins
// ties and is the default. Foreign private issuers mostly use ifrs-full, a
// 40-F filer may use either.
func (d *edgarDoc) pickNamespace() string {
	if d.annualFactCount(nsIFRS) > d.annualFactCount(nsUSGAAP) {
		return nsIFRS
	}
	return nsUSGAAP
}

// pickCurrency returns the monetary unit (three upper-case letters) with the
// most annual-form facts in ns, or "USD" when there is none. Ties go to USD,
// then alphabetical order.
func (d *edgarDoc) pickCurrency(ns string) string {
	count := map[string]int{}
	for _, c := range d.Facts[ns] {
		for unit, fs := range c.Units {
			if len(unit) != 3 || strings.ToUpper(unit) != unit {
				continue
			}
			for _, f := range fs {
				if isAnnualForm(f.Form) {
					count[unit]++
				}
			}
		}
	}
	units := make([]string, 0, len(count))
	for u := range count {
		units = append(units, u)
	}
	sort.Strings(units)
	best := "USD"
	for _, u := range units {
		if count[u] > count[best] {
			best = u
		}
	}
	return best
}

// edgarConcepts lists the ordered concept fallbacks of one taxonomy.
type edgarConcepts struct {
	revenue, ebit, netIncome, eps, sharesDil, ocf, capex, da, dps []string
	equity, shares, cash, invest                                  []string
	noncurrentDebt, totalDebt, currentDebt, shortDebt             []string
	// debtFallback is used for period ends without any debt concept above
	// (IFRS: Borrowings, else the sum of the short-term and other borrowings).
	debtSum []string
}

var usGAAPConcepts = edgarConcepts{
	revenue: []string{"RevenueFromContractWithCustomerExcludingAssessedTax", "Revenues", "SalesRevenueNet",
		"RevenueFromContractWithCustomerIncludingAssessedTax", "SalesRevenueGoodsNet"},
	ebit:      []string{"OperatingIncomeLoss"},
	netIncome: []string{"NetIncomeLoss", "ProfitLoss"},
	eps:       []string{"EarningsPerShareDiluted"},
	sharesDil: []string{"WeightedAverageNumberOfDilutedSharesOutstanding"},
	ocf:       []string{"NetCashProvidedByUsedInOperatingActivities", "NetCashProvidedByUsedInOperatingActivitiesContinuingOperations"},
	capex:     []string{"PaymentsToAcquirePropertyPlantAndEquipment", "PaymentsToAcquireProductiveAssets", "PaymentsForCapitalImprovements"},
	da: []string{"DepreciationDepletionAndAmortization", "DepreciationAndAmortization", "DepreciationAmortizationAndAccretionNet",
		"Depreciation"},
	dps:            []string{"CommonStockDividendsPerShareDeclared", "CommonStockDividendsPerShareCashPaid"},
	equity:         []string{"StockholdersEquity", "StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest"},
	shares:         []string{"CommonStockSharesOutstanding"},
	cash:           []string{"CashAndCashEquivalentsAtCarryingValue"},
	invest:         []string{"MarketableSecuritiesCurrent", "ShortTermInvestments", "AvailableForSaleSecuritiesDebtSecuritiesCurrent"},
	noncurrentDebt: []string{"LongTermDebtNoncurrent"},
	totalDebt:      []string{"LongTermDebt"},
	currentDebt:    []string{"LongTermDebtCurrent"},
	shortDebt:      []string{"CommercialPaper", "ShortTermBorrowings"},
}

var ifrsConcepts = edgarConcepts{
	revenue:   []string{"Revenue", "RevenueFromContractsWithCustomers"},
	ebit:      []string{"ProfitLossFromOperatingActivities"},
	netIncome: []string{"ProfitLossAttributableToOwnersOfParent", "ProfitLoss"},
	eps:       []string{"DilutedEarningsLossPerShare"},
	sharesDil: []string{"AdjustedWeightedAverageShares", "WeightedAverageShares"},
	ocf:       []string{"CashFlowsFromUsedInOperatingActivities"},
	capex: []string{"PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities",
		"PurchaseOfPropertyPlantAndEquipment"},
	da: []string{"AdjustmentsForDepreciationAndAmortisationExpense", "DepreciationAndAmortisationExpense", "DepreciationExpense"},
	dps: []string{"DividendsPaidOrdinarySharesPerShare", "DividendsRecognisedAsDistributionsToOwnersPerShare",
		"DividendsProposedOrDeclaredBeforeFinancialStatementsAuthorisedForIssueButNotRecognisedAsDistributionToOwnersPerShare"},
	equity:  []string{"EquityAttributableToOwnersOfParent", "Equity"},
	shares:  []string{"NumberOfSharesOutstanding"},
	cash:    []string{"CashAndCashEquivalents"},
	debtSum: []string{"ShorttermBorrowings", "OtherBorrowings"},
	// Borrowings is the total, so it is tried first and the parts are only
	// added when it is missing for that period end.
	totalDebt: []string{"Borrowings"},
}

// Financials builds annual (and latest quarterly balance sheet) facts from
// companyfacts. Concept names vary per issuer, so each canonical concept has
// an ordered fallback list.
func (e *EDGAR) Financials(ctx context.Context, cik string) (FinancialFacts, error) {
	id := FormatCIK(cik)
	if id == "" {
		return FinancialFacts{}, fmt.Errorf("marketdata: sec: invalid CIK %q", cik)
	}
	var doc edgarDoc
	if err := getJSON(ctx, e.HTTP, "sec", fmt.Sprintf("%s/api/xbrl/companyfacts/CIK%s.json", e.DataURL, id), e.headers(), &doc); err != nil {
		return FinancialFacts{}, err
	}
	return doc.financials(), nil
}

type periodKey struct {
	end    string
	annual bool
}

func (d *edgarDoc) financials() FinancialFacts {
	ns := d.pickNamespace()
	cur := d.pickCurrency(ns)
	cc := usGAAPConcepts
	if ns == nsIFRS {
		cc = ifrsConcepts
	}
	perShare := cur + "/shares"
	out := FinancialFacts{Source: "sec", Entity: d.EntityName, Currency: cur}
	if cur != "USD" {
		out.Notes = append(out.Notes, "figures are in "+cur+", the filer's reporting currency, not USD; convert before comparing with prices quoted in USD")
	}
	periods := map[periodKey]*FinancialPeriod{}
	add := func(k periodKey, filed, concept string, v money.Decimal) {
		p, ok := periods[k]
		if !ok {
			end, err := time.Parse("2006-01-02", k.end)
			if err != nil {
				return
			}
			typ := PeriodQuarterly
			if k.annual {
				typ = PeriodAnnual
			}
			p = &FinancialPeriod{End: end.UTC(), Type: typ, Filed: filed, Values: map[string]money.Decimal{}}
			periods[k] = p
		}
		if filed > p.Filed {
			p.Filed = filed
		}
		p.Values[concept] = v
	}
	flow := func(concept, unit string, names []string) {
		for end, p := range d.firstOf(ns, unit, true, names...) {
			add(periodKey{end, true}, p.Filed, concept, p.Val)
		}
	}
	instant := func(concept, unit string, names []string) {
		for end, p := range d.firstOf(ns, unit, false, names...) {
			add(periodKey{end, p.Annual}, p.Filed, concept, p.Val)
		}
	}

	flow(ConceptRevenue, cur, cc.revenue)
	flow(ConceptEBIT, cur, cc.ebit)
	flow(ConceptNetIncome, cur, cc.netIncome)
	flow(ConceptEPSDiluted, perShare, cc.eps)
	flow(ConceptSharesDilAvg, "shares", cc.sharesDil)
	flow(ConceptOCF, cur, cc.ocf)
	flow(ConceptCapex, cur, cc.capex)
	flow(ConceptDA, cur, cc.da)
	flow(ConceptDPS, perShare, cc.dps)
	instant(ConceptEquity, cur, cc.equity)
	instant(ConceptShares, "shares", cc.shares)

	// Cash plus short-term investments, per period end.
	cash := d.firstOf(ns, cur, false, cc.cash...)
	invest := d.firstOf(ns, cur, false, cc.invest...)
	for end, p := range cash {
		v := p.Val
		if inv, ok := invest[end]; ok {
			v = money.New(v.Add(inv.Val.Decimal))
		}
		add(periodKey{end, p.Annual}, p.Filed, ConceptCash, v)
	}

	// Financial debt: long-term (non-current plus current) and short-term
	// borrowings. Operating and finance leases are not included.
	noncurrent := d.firstOf(ns, cur, false, cc.noncurrentDebt...)
	total := d.firstOf(ns, cur, false, cc.totalDebt...)
	current := d.firstOf(ns, cur, false, cc.currentDebt...)
	short := d.firstOf(ns, cur, false, cc.shortDebt...)
	ends := map[string]edgarPoint{}
	for end, p := range noncurrent {
		ends[end] = p
	}
	for end, p := range total {
		if _, ok := ends[end]; !ok {
			ends[end] = p
		}
	}
	for end, base := range ends {
		sum := base.Val.Decimal
		if _, ok := noncurrent[end]; ok {
			if c, ok := current[end]; ok {
				sum = sum.Add(c.Val.Decimal)
			}
		}
		if s, ok := short[end]; ok {
			sum = sum.Add(s.Val.Decimal)
		}
		add(periodKey{end, base.Annual}, base.Filed, ConceptDebt, money.New(sum))
	}
	// Period ends with no total: add the parts (they are disjoint line items).
	parts := map[string]edgarPoint{}
	for _, n := range cc.debtSum {
		for end, p := range d.series(ns, n, cur, false) {
			if _, ok := ends[end]; ok {
				continue
			}
			if acc, ok := parts[end]; ok {
				acc.Val = money.New(acc.Val.Add(p.Val.Decimal))
				parts[end] = acc
			} else {
				parts[end] = p
			}
		}
	}
	for end, p := range parts {
		add(periodKey{end, p.Annual}, p.Filed, ConceptDebt, p.Val)
	}

	// Latest shares outstanding on a filing cover (dei).
	covers := map[string]edgarPoint{}
	for _, f := range d.Facts["dei"]["EntityCommonStockSharesOutstanding"].Units["shares"] {
		v, err := jsonDecimal(f.Val)
		if err != nil || f.End == "" {
			continue
		}
		if cur, ok := covers[f.End]; !ok || f.Filed >= cur.Filed {
			covers[f.End] = edgarPoint{Val: v, Filed: f.Filed}
		}
	}
	latestCover := ""
	for end := range covers {
		if end > latestCover {
			latestCover = end
		}
	}
	if latestCover != "" {
		add(periodKey{latestCover, false}, covers[latestCover].Filed, ConceptSharesCover, covers[latestCover].Val)
	}

	// Keep the 8 latest annual periods and quarterly ones after the last year.
	var annual, quarterly []FinancialPeriod
	for _, p := range periods {
		if p.Type == PeriodAnnual {
			annual = append(annual, *p)
		} else {
			quarterly = append(quarterly, *p)
		}
	}
	sortPeriods(annual)
	if len(annual) > 8 {
		annual = annual[len(annual)-8:]
	}
	lastAnnual := time.Time{}
	if len(annual) > 0 {
		lastAnnual = annual[len(annual)-1].End
	}
	sort.Slice(quarterly, func(i, j int) bool { return quarterly[i].End.Before(quarterly[j].End) })
	for _, q := range quarterly {
		if q.End.After(lastAnnual) {
			out.Periods = append(out.Periods, q)
		}
	}
	out.Periods = append(annual, out.Periods...)
	sortPeriods(out.Periods)
	if ns == nsUSGAAP { // 10-Q facts; foreign filers have none
		out.Quarters = d.quarters()
	}
	if len(annual) == 0 {
		out.Notes = append(out.Notes, "no annual (10-K, 20-F or 40-F) figures found in the us-gaap or ifrs-full taxonomy")
	} else if len(out.Quarters) == 0 && d.foreignFiler(ns) {
		out.Notes = append(out.Notes, "quarterly figures unavailable: foreign private issuer (6-K interim reports are not XBRL-tagged)")
	}
	return out
}
