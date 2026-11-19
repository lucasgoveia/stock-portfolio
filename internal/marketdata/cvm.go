package marketdata

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/shopspring/decimal"
)

// DefaultCVMBaseURL is the CVM open data root for listed companies.
const DefaultCVMBaseURL = "https://dados.cvm.gov.br/dados/CIA_ABERTA"

// cvmTimeout bounds one DFP zip download (13 MB or more).
const cvmTimeout = 10 * time.Minute

// CVM reads annual statements (DFP) from CVM Dados Abertos. Verified live
// against the 2025 DFP zip (see docs/data-sources.md §6).
//
// The zips are 13 to 27 MB and expand to about 300 MB, so each one is
// downloaded to a temp file, only the needed members are opened, and rows are
// filtered by CNPJ while streaming: no CSV is ever held in memory.
type CVM struct {
	HTTP    *http.Client
	BaseURL string
}

// NewCVM builds a client; a nil client gets a long timeout suited to the zips.
func NewCVM(c *http.Client, baseURL string) *CVM {
	if c == nil {
		c = &http.Client{Timeout: cvmTimeout}
	}
	if baseURL == "" {
		baseURL = DefaultCVMBaseURL
	}
	return &CVM{HTTP: c, BaseURL: strings.TrimRight(baseURL, "/")}
}

// FormatCNPJ returns the 14 digits of s as XX.XXX.XXX/XXXX-XX, or "" when s
// does not hold exactly 14 digits.
func FormatCNPJ(s string) string {
	d := digitsOnly(s)
	if len(d) != 14 {
		return ""
	}
	return d[0:2] + "." + d[2:5] + "." + d[5:8] + "/" + d[8:12] + "-" + d[12:14]
}

// ValidCNPJ reports whether s is a CNPJ with correct check digits.
func ValidCNPJ(s string) bool {
	d := digitsOnly(s)
	if len(d) != 14 || strings.Count(d, d[:1]) == 14 {
		return false
	}
	check := func(n int) byte {
		w := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}[13-n:]
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(d[i]-'0') * w[i]
		}
		r := sum % 11
		if r < 2 {
			return '0'
		}
		return byte('0' + 11 - r)
	}
	return d[12] == check(12) && d[13] == check(13)
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AnnualFinancials returns the annual figures of the company for each year
// whose DFP zip exists (years without a published file are skipped with a
// note). For a restated filing the highest VERSAO wins. Consolidated
// statements are preferred over individual ones.
func (c *CVM) AnnualFinancials(ctx context.Context, cnpj string, years []int) (FinancialFacts, error) {
	formatted := FormatCNPJ(cnpj)
	if formatted == "" {
		return FinancialFacts{}, fmt.Errorf("marketdata: cvm: invalid CNPJ %q", cnpj)
	}
	out := FinancialFacts{Source: "cvm", Currency: "BRL"}
	byEnd := map[string]*FinancialPeriod{}
	for _, y := range years {
		ps, entity, notes, err := c.year(ctx, formatted, y)
		if IsNotFound(err) {
			out.Notes = append(out.Notes, fmt.Sprintf("no DFP file published for %d", y))
			continue
		}
		if err != nil {
			return FinancialFacts{}, err
		}
		if entity != "" {
			out.Entity = entity
		}
		out.Notes = append(out.Notes, notes...)
		for i := range ps {
			p := ps[i]
			byEnd[p.End.Format("2006-01-02")] = &p
		}
	}
	for _, p := range byEnd {
		out.Periods = append(out.Periods, *p)
	}
	sortPeriods(out.Periods)
	return out, nil
}

// year downloads and parses one DFP zip.
func (c *CVM) year(ctx context.Context, cnpj string, year int) ([]FinancialPeriod, string, []string, error) {
	var (
		ps     []FinancialPeriod
		entity string
		notes  []string
		parsed error
	)
	err := c.withZip(ctx, fmt.Sprintf("%s/DOC/DFP/DADOS/dfp_cia_aberta_%d.zip", c.BaseURL, year), func(zr *zip.Reader) {
		ps, entity, notes, parsed = parseDFP(zr, cnpj, year)
	})
	if err != nil {
		return nil, "", nil, err
	}
	return ps, entity, notes, parsed
}

// withZip downloads url to a temp file and hands the opened zip to fn.
func (c *CVM) withZip(ctx context.Context, url string, fn func(zr *zip.Reader)) error {
	resp, err := get(ctx, c.HTTP, "cvm", url, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	tmp, err := os.CreateTemp("", "cvm-*.zip")
	if err != nil {
		return fmt.Errorf("marketdata: cvm: %w", err)
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("marketdata: cvm: downloading %s: %w", url, err)
	}
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return fmt.Errorf("marketdata: cvm: opening %s: %w", url, err)
	}
	defer func() { _ = zr.Close() }()
	fn(&zr.Reader)
	return nil
}

// cvmRow is one account line of a statement file.
type cvmRow struct {
	Refer   string // DT_REFER
	Version int
	Entity  string
	Moeda   string
	Scale   decimal.Decimal
	Ordem   string
	Start   string // DT_INI_EXERC
	End     string // DT_FIM_EXERC
	Code    string
	Desc    string
	Value   decimal.Decimal
}

// parseDFP extracts the company's periods from an open DFP zip.
func parseDFP(zr *zip.Reader, cnpj string, year int) ([]FinancialPeriod, string, []string, error) {
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	name := func(kind string) string { return fmt.Sprintf("dfp_cia_aberta_%s_%d.csv", kind, year) }
	stmt := func(kind, scope string) ([]cvmRow, error) {
		return readStatement(files[name(kind+"_"+scope)], cnpj)
	}
	var notes []string
	scope := "con"
	dre, err := stmt("DRE", scope)
	if err != nil {
		return nil, "", nil, err
	}
	if len(dre) == 0 {
		scope = "ind"
		if dre, err = stmt("DRE", scope); err != nil {
			return nil, "", nil, err
		}
		if len(dre) > 0 {
			notes = append(notes, fmt.Sprintf("%d: only individual (not consolidated) statements available", year))
		}
	}
	if len(dre) == 0 {
		return nil, "", append(notes, fmt.Sprintf("%d: no DFP filing for this company yet", year)), nil
	}
	bpa, err := stmt("BPA", scope)
	if err != nil {
		return nil, "", nil, err
	}
	bpp, err := stmt("BPP", scope)
	if err != nil {
		return nil, "", nil, err
	}
	dfc, err := stmt("DFC_MI", scope) // indirect method carries D&A
	if err != nil {
		return nil, "", nil, err
	}
	if len(dfc) == 0 {
		if dfc, err = stmt("DFC_MD", scope); err != nil {
			return nil, "", nil, err
		}
	}
	caps, err := readCapital(files[name("composicao_capital")], cnpj)
	if err != nil {
		return nil, "", nil, err
	}

	periods := map[string]*FinancialPeriod{}
	entity := ""
	period := func(refer string, version int) *FinancialPeriod {
		p, ok := periods[refer]
		if !ok {
			end, perr := time.Parse("2006-01-02", refer)
			if perr != nil {
				return nil
			}
			p = &FinancialPeriod{End: end.UTC(), Type: PeriodAnnual, Filed: "v" + strconv.Itoa(version), Values: map[string]money.Decimal{}}
			periods[refer] = p
		}
		return p
	}
	apply := func(rows []cvmRow, f func(p *FinancialPeriod, rows []cvmRow)) {
		for refer, rs := range latestVersion(rows) {
			if len(rs) == 0 {
				continue
			}
			if rs[0].Moeda != "" && !strings.EqualFold(rs[0].Moeda, "REAL") {
				notes = append(notes, fmt.Sprintf("%s: reported in %s, skipped", refer, rs[0].Moeda))
				continue
			}
			if entity == "" {
				entity = rs[0].Entity
			}
			if p := period(refer, rs[0].Version); p != nil {
				f(p, rs)
			}
		}
	}
	financial := isFinancialLayout(dre)
	if financial {
		notes = append(notes, fmt.Sprintf("%d: financial-institution chart of accounts: revenue is gross financial-intermediation income and net income and equity are the controlling shareholders' share; EBIT, debt, capex and operating cash flow are omitted (not comparable with industrial companies)", year))
	}
	capByRefer := latestCapital(caps)
	hints := map[string]int{}
	apply(dre, func(p *FinancialPeriod, rs []cvmRow) {
		a := accounts(rs)
		set(p, ConceptRevenue, at(a, "3.01"))
		if !financial {
			set(p, ConceptEBIT, at(a, "3.05"))
		}
		if ni, ok := netIncome(rs); ok {
			set(p, ConceptNetIncome, &ni)
		}
		if rev, ok := a["3.01"]; ok && rev.IsZero() {
			if ni, ok := p.Values[ConceptNetIncome]; ok && !ni.IsZero() {
				notes = append(notes, fmt.Sprintf("%s: revenue is zero (typically a holding company whose income comes from equity-method results); revenue-based metrics are not meaningful", p.End.Format("2006-01-02")))
			}
		}
		if cr, ok := capByRefer[rs[0].Refer]; ok {
			hints[rs[0].Refer] = shareScaleHint(rs, cr.Total)
		}
	})
	if !financial {
		apply(bpa, func(p *FinancialPeriod, rs []cvmRow) {
			a := accounts(rs)
			if c, ok := a["1.01.01"]; ok {
				if f, ok := a["1.01.02"]; ok {
					c = c.Add(f)
				}
				set(p, ConceptCash, &c)
			}
		})
	}
	apply(bpp, func(p *FinancialPeriod, rs []cvmRow) {
		a := accounts(rs)
		if financial {
			if eq, ok := controllerEquity(rs); ok {
				set(p, ConceptEquity, &eq)
			}
			return
		}
		debt, found := decimal.Zero, false
		for _, code := range []string{"2.01.04", "2.02.01"} {
			if v, ok := a[code]; ok {
				debt, found = debt.Add(v), true
			}
		}
		if found {
			set(p, ConceptDebt, &debt)
		}
		if eq, ok := a["2.03"]; ok {
			if nci, ok := a["2.03.09"]; ok {
				eq = eq.Sub(nci)
			}
			set(p, ConceptEquity, &eq)
		}
	})
	if !financial {
		apply(dfc, func(p *FinancialPeriod, rs []cvmRow) {
			a := accounts(rs)
			set(p, ConceptOCF, at(a, "6.01"))
			if v, ok := capex(rs); ok {
				set(p, ConceptCapex, &v)
			}
			if v, ok := depreciation(rs); ok {
				set(p, ConceptDA, &v)
			}
		})
	}
	fileHint := fallbackHint(hints)
	for refer, cr := range capByRefer {
		if p := period(refer, cr.Version); p != nil {
			f := hints[refer]
			if f == 0 {
				f = fileHint
			}
			sh := cr.Total.Sub(cr.Treasury).Mul(decimal.NewFromInt(int64(f)))
			set(p, ConceptShares, &sh)
		}
	}

	out := make([]FinancialPeriod, 0, len(periods))
	for _, p := range periods {
		out = append(out, *p)
	}
	sortPeriods(out)
	return out, entity, notes, nil
}

var (
	attributableCode = regexp.MustCompile(`^3\.\d\d\.01$`)
	topEquityCode    = regexp.MustCompile(`^2\.\d\d$`)
)

// isFinancialLayout reports whether the DRE rows follow the bank or insurer
// chart of accounts (3.01 is intermediation income or insurance revenue,
// not sales of goods and services).
func isFinancialLayout(rows []cvmRow) bool {
	for _, r := range rows {
		if r.Code != "3.01" {
			continue
		}
		d := fold(r.Desc)
		return strings.Contains(d, "intermediacao financeira") || strings.Contains(d, "seguradora")
	}
	return false
}

// netIncome returns the net income attributable to the controlling
// shareholders. The attributable line sits under the consolidated result
// (3.11.01 in the industrial layout, 3.09.01 or 3.11.01 for banks, 3.13.01 for
// insurers), so the highest 3.NN.01 line described as attributable to the
// controlling shareholders is used. Some filers leave it at zero while
// reporting the whole result in the parent line (Klabin, Taesa), so a zero
// falls back to the consolidated result less the non-controlling share.
func netIncome(rs []cvmRow) (decimal.Decimal, bool) {
	a := accounts(rs)
	best := ""
	for _, r := range rs {
		d := fold(r.Desc)
		if attributableCode.MatchString(r.Code) && strings.Contains(d, "controlad") && !strings.Contains(d, "nao controlad") && r.Code > best {
			best = r.Code
		}
	}
	if best == "" {
		v, ok := a["3.11"]
		return v, ok
	}
	v := a[best]
	if !v.IsZero() {
		return v, true
	}
	if total, ok := a[best[:4]]; ok {
		return total.Sub(a[best[:4]+".02"]), true
	}
	return v, true
}

// controllerEquity returns the shareholders' equity attributable to the
// controlling shareholders in a bank or insurer balance sheet: the explicit
// controller line when present, otherwise the equity total (the first 2.NN
// line named "Patrimônio Líquido") less the non-controlling interest.
func controllerEquity(rs []cvmRow) (decimal.Decimal, bool) {
	total := ""
	for _, r := range rs {
		if topEquityCode.MatchString(r.Code) && strings.HasPrefix(fold(r.Desc), "patrimonio liquido") {
			total = r.Code
			break
		}
	}
	if total == "" {
		return decimal.Zero, false
	}
	a := accounts(rs)
	var nci *decimal.Decimal
	for _, r := range rs {
		if len(r.Code) != len(total)+3 || !strings.HasPrefix(r.Code, total+".") {
			continue
		}
		d := fold(r.Desc)
		switch {
		case strings.Contains(d, "nao controlador"):
			v := r.Value
			nci = &v
		case strings.Contains(d, "controlador"):
			return r.Value, true
		}
	}
	eq := a[total]
	if nci != nil {
		eq = eq.Sub(*nci)
	}
	return eq, true
}

// epsOf picks the reported earnings per share of one DRE column: the first
// non-zero class line (3.99.01.xx basic, then 3.99.02.xx diluted), then the
// 3.99 total. Class lines come first because some filers (Itaú) report the
// 3.99 total on a different basis. EPS is a per-share amount, so the monetary
// ESCALA_MOEDA that readStatement applied to every value is undone.
func epsOf(rs []cvmRow) decimal.Decimal {
	unscaled := func(r cvmRow) decimal.Decimal {
		if r.Scale.IsPositive() {
			return r.Value.Div(r.Scale)
		}
		return r.Value
	}
	for _, r := range rs {
		if strings.HasPrefix(r.Code, "3.99.") && len(r.Code) == 10 && !r.Value.IsZero() {
			return unscaled(r)
		}
	}
	for _, r := range rs {
		if r.Code == "3.99" {
			return unscaled(r)
		}
	}
	return decimal.Zero
}

// shareScaleHint infers whether the composicao_capital quantity qty is in
// thousands. CVM has no scale field for it and some issuers file thousands of
// shares (VALE, Taesa, Itaú), so the quantity is compared with the share count
// implied by the DRE: net income over reported EPS. It returns 1000 when qty
// times 1000 is closer to the implied count (in log distance), 1 when qty is
// closer, and 0 when there is no usable signal.
func shareScaleHint(rows []cvmRow, qty decimal.Decimal) int {
	if !qty.IsPositive() {
		return 0
	}
	byStart := map[string][]cvmRow{}
	var starts []string
	for _, r := range rows {
		if _, ok := byStart[r.Start]; !ok {
			starts = append(starts, r.Start)
		}
		byStart[r.Start] = append(byStart[r.Start], r)
	}
	sort.Strings(starts)
	q, _ := qty.Float64()
	for _, s := range starts {
		rs := byStart[s]
		ni, ok := netIncome(rs)
		eps := epsOf(rs)
		if !ok || ni.IsZero() || eps.IsZero() {
			continue
		}
		n, _ := ni.Abs().Float64()
		e, _ := eps.Abs().Float64()
		implied := n / e
		if implied <= 0 {
			continue
		}
		if math.Abs(math.Log(q*1000/implied)) < math.Abs(math.Log(q/implied)) {
			return 1000
		}
		return 1
	}
	return 0
}

// fallbackHint is the scale for periods without an EPS signal: the filer
// uses one convention across periods, so the hint of the latest period that
// has one applies. Without any hint quantities are taken as units.
func fallbackHint(hints map[string]int) int {
	latest, f := "", 1
	for refer, h := range hints {
		if h != 0 && refer > latest {
			latest, f = refer, h
		}
	}
	return f
}

func at(a map[string]decimal.Decimal, code string) *decimal.Decimal {
	if v, ok := a[code]; ok {
		return &v
	}
	return nil
}

func set(p *FinancialPeriod, concept string, v *decimal.Decimal) {
	if v != nil {
		p.Values[concept] = money.New(*v)
	}
}

// accounts maps account code to value, keeping the first row per code.
func accounts(rows []cvmRow) map[string]decimal.Decimal {
	m := make(map[string]decimal.Decimal, len(rows))
	for _, r := range rows {
		if _, ok := m[r.Code]; !ok {
			m[r.Code] = r.Value
		}
	}
	return m
}

// latestVersion keeps, per DT_REFER, the rows of the highest VERSAO of the
// current-exercise column (ORDEM_EXERC = ÚLTIMO).
func latestVersion(rows []cvmRow) map[string][]cvmRow {
	best := map[string]int{}
	for _, r := range rows {
		if r.Version > best[r.Refer] {
			best[r.Refer] = r.Version
		}
	}
	out := map[string][]cvmRow{}
	for _, r := range rows {
		if r.Version == best[r.Refer] && r.Ordem == "ÚLTIMO" {
			out[r.Refer] = append(out[r.Refer], r)
		}
	}
	for _, rs := range out {
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].Code < rs[j].Code })
	}
	return out
}

var (
	investingChild = regexp.MustCompile(`^6\.02\.\d\d$`)
	opActivityLine = regexp.MustCompile(`^6\.01\.0[12]\.\d\d$`)
	foldReplacer   = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c")
)

func fold(s string) string { return foldReplacer.Replace(strings.ToLower(s)) }

// capex sums the investing lines that buy fixed or intangible assets: direct
// children of 6.02 with a negative value whose description mentions
// imobilizado, intangível or propriedade. This is an approximation: CVM has no
// fixed capex account code.
func capex(rows []cvmRow) (decimal.Decimal, bool) {
	sum, found := decimal.Zero, false
	for _, r := range rows {
		if !investingChild.MatchString(r.Code) || !r.Value.IsNegative() {
			continue
		}
		d := fold(r.Desc)
		if strings.Contains(d, "imobilizad") || strings.Contains(d, "intangiv") || strings.Contains(d, "propriedade") {
			sum, found = sum.Add(r.Value.Abs()), true
		}
	}
	return sum, found
}

// depreciation picks D&A from the indirect cash flow adjustments: a line that
// names both depreciation and amortization, otherwise the sum of lines naming
// either.
func depreciation(rows []cvmRow) (decimal.Decimal, bool) {
	var both, either []decimal.Decimal
	for _, r := range rows {
		if !opActivityLine.MatchString(r.Code) {
			continue
		}
		d := fold(r.Desc)
		dep := strings.Contains(d, "deprecia")
		amo := strings.Contains(d, "amortiza")
		if strings.Contains(d, "ajuste") || strings.Contains(d, "impairment") || strings.Contains(d, "recuperavel") {
			continue
		}
		switch {
		case dep && amo:
			both = append(both, r.Value)
		case dep || amo:
			either = append(either, r.Value)
		}
	}
	if len(both) > 0 {
		return both[0], true
	}
	if len(either) > 0 {
		sum := decimal.Zero
		for _, v := range either {
			sum = sum.Add(v)
		}
		return sum, true
	}
	return decimal.Zero, false
}

// capitalRow is one composicao_capital line.
type capitalRow struct {
	Refer    string
	Version  int
	Total    decimal.Decimal
	Treasury decimal.Decimal
}

func latestCapital(rows []capitalRow) map[string]capitalRow {
	out := map[string]capitalRow{}
	for _, r := range rows {
		if cur, ok := out[r.Refer]; !ok || r.Version > cur.Version {
			out[r.Refer] = r
		}
	}
	return out
}

// openCSV streams f as a semicolon CSV decoded from Latin-1.
func openCSV(f *zip.File) (*csv.Reader, io.Closer, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("marketdata: cvm: opening %s: %w", f.Name, err)
	}
	r := csv.NewReader(&latin1Reader{r: rc})
	r.Comma = ';'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	return r, rc, nil
}

func headerIndex(h []string) map[string]int {
	m := make(map[string]int, len(h))
	for i, c := range h {
		m[strings.TrimPrefix(strings.TrimSpace(c), "\ufeff")] = i
	}
	return m
}

func col(rec []string, idx map[string]int, name string) string {
	if i, ok := idx[name]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}

func readStatement(f *zip.File, cnpj string) ([]cvmRow, error) {
	if f == nil {
		return nil, nil
	}
	r, closer, err := openCSV(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: %s: reading header: %w", f.Name, err)
	}
	idx := headerIndex(head)
	var rows []cvmRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: %s: %w", f.Name, err)
		}
		if col(rec, idx, "CNPJ_CIA") != cnpj {
			continue
		}
		ver, _ := strconv.Atoi(col(rec, idx, "VERSAO"))
		val, err := decimal.NewFromString(strings.TrimSpace(col(rec, idx, "VL_CONTA")))
		if err != nil {
			continue
		}
		rows = append(rows, cvmRow{
			Refer: col(rec, idx, "DT_REFER"), Version: ver, Entity: col(rec, idx, "DENOM_CIA"),
			Moeda: col(rec, idx, "MOEDA"), Scale: cvmScale(col(rec, idx, "ESCALA_MOEDA")),
			Ordem: col(rec, idx, "ORDEM_EXERC"), Start: col(rec, idx, "DT_INI_EXERC"), End: col(rec, idx, "DT_FIM_EXERC"),
			Code: col(rec, idx, "CD_CONTA"), Desc: col(rec, idx, "DS_CONTA"),
			Value: val.Mul(cvmScale(col(rec, idx, "ESCALA_MOEDA"))),
		})
	}
}

func cvmScale(s string) decimal.Decimal {
	switch fold(strings.TrimSpace(s)) {
	case "mil":
		return decimal.NewFromInt(1000)
	case "milhao", "milhoes":
		return decimal.NewFromInt(1000000)
	}
	return decimal.NewFromInt(1)
}

func readCapital(f *zip.File, cnpj string) ([]capitalRow, error) {
	if f == nil {
		return nil, nil
	}
	r, closer, err := openCSV(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: cvm: %s: reading header: %w", f.Name, err)
	}
	idx := headerIndex(head)
	var rows []capitalRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: %s: %w", f.Name, err)
		}
		if col(rec, idx, "CNPJ_CIA") != cnpj {
			continue
		}
		ver, _ := strconv.Atoi(col(rec, idx, "VERSAO"))
		total, err := decimal.NewFromString(strings.TrimSpace(col(rec, idx, "QT_ACAO_TOTAL_CAP_INTEGR")))
		if err != nil {
			continue
		}
		tre, err := decimal.NewFromString(strings.TrimSpace(col(rec, idx, "QT_ACAO_TOTAL_TESOURO")))
		if err != nil {
			tre = decimal.Zero
		}
		rows = append(rows, capitalRow{Refer: col(rec, idx, "DT_REFER"), Version: ver, Total: total, Treasury: tre})
	}
}

// latin1Reader converts a Latin-1 byte stream to UTF-8 on the fly. It buffers
// converted bytes, so it works with callers that read into a buffer of any
// size, even a single byte (bufio does when its buffer is nearly full).
type latin1Reader struct {
	r   io.Reader
	in  [4096]byte
	out []byte
	err error
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(l.out) == 0 && l.err == nil {
		var n int
		n, l.err = l.r.Read(l.in[:])
		for _, b := range l.in[:n] {
			if b < 0x80 {
				l.out = append(l.out, b)
			} else {
				l.out = append(l.out, 0xC0|b>>6, 0x80|b&0x3F)
			}
		}
	}
	if len(l.out) == 0 {
		return 0, l.err
	}
	n := copy(p, l.out)
	l.out = l.out[n:]
	return n, nil
}
