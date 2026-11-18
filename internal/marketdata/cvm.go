package marketdata

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
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
	url := fmt.Sprintf("%s/DOC/DFP/DADOS/dfp_cia_aberta_%d.zip", c.BaseURL, year)
	resp, err := get(ctx, c.HTTP, "cvm", url, nil)
	if err != nil {
		return nil, "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	tmp, err := os.CreateTemp("", "cvm-dfp-*.zip")
	if err != nil {
		return nil, "", nil, fmt.Errorf("marketdata: cvm: %w", err)
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return nil, "", nil, fmt.Errorf("marketdata: cvm: downloading %s: %w", url, err)
	}
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return nil, "", nil, fmt.Errorf("marketdata: cvm: opening %s: %w", url, err)
	}
	defer func() { _ = zr.Close() }()
	return parseDFP(&zr.Reader, cnpj, year)
}

// cvmRow is one account line of a statement file.
type cvmRow struct {
	Refer   string // DT_REFER
	Version int
	Entity  string
	Moeda   string
	Scale   decimal.Decimal
	Ordem   string
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
	apply(dre, func(p *FinancialPeriod, rs []cvmRow) {
		a := accounts(rs)
		set(p, ConceptRevenue, at(a, "3.01"))
		set(p, ConceptEBIT, at(a, "3.05"))
		if v, ok := a["3.11.01"]; ok {
			set(p, ConceptNetIncome, &v)
		} else {
			set(p, ConceptNetIncome, at(a, "3.11"))
		}
	})
	apply(bpa, func(p *FinancialPeriod, rs []cvmRow) {
		a := accounts(rs)
		if c, ok := a["1.01.01"]; ok {
			if f, ok := a["1.01.02"]; ok {
				c = c.Add(f)
			}
			set(p, ConceptCash, &c)
		}
	})
	apply(bpp, func(p *FinancialPeriod, rs []cvmRow) {
		a := accounts(rs)
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
	for refer, cr := range latestCapital(caps) {
		if p := period(refer, cr.Version); p != nil {
			sh := cr.Total.Sub(cr.Treasury)
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
			Ordem: col(rec, idx, "ORDEM_EXERC"), Code: col(rec, idx, "CD_CONTA"), Desc: col(rec, idx, "DS_CONTA"),
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

// latin1Reader converts a Latin-1 byte stream to UTF-8 on the fly.
type latin1Reader struct {
	r   io.Reader
	buf []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(p) < 2 {
		return 0, io.ErrShortBuffer
	}
	if cap(l.buf) < len(p)/2 {
		l.buf = make([]byte, len(p)/2)
	}
	n, err := l.r.Read(l.buf[:len(p)/2])
	w := 0
	for _, b := range l.buf[:n] {
		if b < 0x80 {
			p[w] = b
			w++
			continue
		}
		p[w] = 0xC0 | b>>6
		p[w+1] = 0x80 | b&0x3F
		w += 2
	}
	return w, err
}
