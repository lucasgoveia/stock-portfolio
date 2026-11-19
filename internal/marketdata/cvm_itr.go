package marketdata

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/shopspring/decimal"
)

// QuarterlyFinancials returns the Q1 to Q3 income statement figures of the
// company from the ITR zips of the given years (a year without a published
// file is skipped). The Q4 is not in any ITR: derive it with
// DeriveFourthQuarters from the annual DFP figures.
//
// Verified live against the 2025 and 2026 ITR zips (docs/data-sources.md §6):
// the DRE rows of a Q2 or Q3 ITR carry two ÚLTIMO columns, the 3-month period
// and the year to date (DT_INI_EXERC differs); a Q1 carries only the 3-month
// one. Earnings per share is net income over the period-end share count, so
// it has the same definition as the expected EPS (spec §12.4).
func (c *CVM) QuarterlyFinancials(ctx context.Context, cnpj string, years []int) ([]QuarterFact, []string, error) {
	formatted := FormatCNPJ(cnpj)
	if formatted == "" {
		return nil, nil, fmt.Errorf("marketdata: cvm: invalid CNPJ %q", cnpj)
	}
	var (
		out   []QuarterFact
		notes []string
	)
	for _, y := range years {
		var (
			qs     []QuarterFact
			qnotes []string
			parsed error
		)
		err := c.withZip(ctx, fmt.Sprintf("%s/DOC/ITR/DADOS/itr_cia_aberta_%d.zip", c.BaseURL, y), func(zr *zip.Reader) {
			qs, qnotes, parsed = parseITR(zr, formatted, y)
		})
		if IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if parsed != nil {
			return nil, nil, parsed
		}
		out = append(out, qs...)
		notes = append(notes, qnotes...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].End.Before(out[j].End) })
	return out, notes, nil
}

// parseITR extracts the company's quarters from an open ITR zip.
func parseITR(zr *zip.Reader, cnpj string, year int) ([]QuarterFact, []string, error) {
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	var notes []string
	scope := "con"
	dre, err := readStatement(files[fmt.Sprintf("itr_cia_aberta_DRE_%s_%d.csv", scope, year)], cnpj)
	if err != nil {
		return nil, nil, err
	}
	if len(dre) == 0 {
		scope = "ind"
		if dre, err = readStatement(files[fmt.Sprintf("itr_cia_aberta_DRE_%s_%d.csv", scope, year)], cnpj); err != nil {
			return nil, nil, err
		}
		if len(dre) > 0 {
			notes = append(notes, fmt.Sprintf("%d ITR: only individual (not consolidated) statements available", year))
		}
	}
	if len(dre) == 0 {
		return nil, notes, nil
	}
	caps, err := readCapital(files[fmt.Sprintf("itr_cia_aberta_composicao_capital_%d.csv", year)], cnpj)
	if err != nil {
		return nil, nil, err
	}
	capByRefer := latestCapital(caps)
	filed, err := readITRIndex(files[fmt.Sprintf("itr_cia_aberta_%d.csv", year)], cnpj)
	if err != nil {
		return nil, nil, err
	}

	financial := isFinancialLayout(dre)
	if financial {
		notes = append(notes, fmt.Sprintf("%d ITR: financial-institution chart of accounts: revenue is gross financial-intermediation income", year))
	}
	byRefer := latestVersion(dre)
	hints := map[string]int{}
	for refer, rows := range byRefer {
		if cr, ok := capByRefer[refer]; ok {
			hints[refer] = shareScaleHint(rows, cr.Total)
		}
	}
	fileHint := fallbackHint(hints)

	var out []QuarterFact
	for refer, rows := range byRefer {
		if len(rows) == 0 {
			continue
		}
		if m := rows[0].Moeda; m != "" && !strings.EqualFold(m, "REAL") {
			notes = append(notes, fmt.Sprintf("%s: reported in %s, skipped", refer, m))
			continue
		}
		end, perr := time.Parse("2006-01-02", refer)
		if perr != nil {
			continue
		}
		q, ok := splitQuarter(rows, end)
		if !ok {
			continue
		}
		q.Filed = "v" + strconv.Itoa(rows[0].Version)
		if d, ok := filed[refer+"|"+strconv.Itoa(rows[0].Version)]; ok {
			q.Filed = d
		}
		if cr, ok := capByRefer[refer]; ok {
			f := hints[refer]
			if f == 0 {
				f = fileHint
			}
			sh := cr.Total.Sub(cr.Treasury).Mul(decimal.NewFromInt(int64(f)))
			if sh.IsPositive() {
				q.Values[ConceptShares] = money.New(sh)
				for _, m := range []map[string]money.Decimal{q.Values, q.YTD} {
					if ni, ok := m[ConceptNetIncome]; ok {
						m[ConceptEPSDiluted] = money.New(ni.Div(sh).Round(6))
					}
				}
			}
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].End.Before(out[j].End) })
	return out, notes, nil
}

// splitQuarter separates the 3-month and year-to-date columns of one ITR.
// The year-to-date column starts at the fiscal year start (the earliest
// DT_INI_EXERC); the fiscal quarter follows from how many months it spans.
func splitQuarter(rows []cvmRow, end time.Time) (QuarterFact, bool) {
	byStart := map[string][]cvmRow{}
	for _, r := range rows {
		byStart[r.Start] = append(byStart[r.Start], r)
	}
	var starts []string
	for s := range byStart {
		starts = append(starts, s)
	}
	sort.Strings(starts)
	ytdStart, err := time.Parse("2006-01-02", starts[0])
	if err != nil {
		return QuarterFact{}, false
	}
	months := (end.Year()-ytdStart.Year())*12 + int(end.Month()) - int(ytdStart.Month()) + 1
	quarter := (months + 1) / 3
	if months%3 != 0 || quarter < 1 || quarter > 3 {
		return QuarterFact{}, false
	}
	var three []cvmRow
	for _, s := range starts {
		st, err := time.Parse("2006-01-02", s)
		if err != nil {
			continue
		}
		if durationClass(int(end.Sub(st).Hours()/24)) == durationQuarter {
			three = byStart[s]
		}
	}
	if three == nil {
		return QuarterFact{}, false
	}
	q := QuarterFact{End: end.UTC(), Quarter: quarter, Values: map[string]money.Decimal{}, YTD: map[string]money.Decimal{}}
	fill := func(dst map[string]money.Decimal, rs []cvmRow) {
		a := accounts(rs)
		if v := at(a, "3.01"); v != nil {
			dst[ConceptRevenue] = money.New(*v)
		}
		if v, ok := netIncome(rs); ok {
			dst[ConceptNetIncome] = money.New(v)
		}
	}
	fill(q.Values, three)
	fill(q.YTD, byStart[starts[0]])
	if _, ok := q.Values[ConceptRevenue]; !ok {
		return QuarterFact{}, false
	}
	return q, true
}

// readITRIndex maps "DT_REFER|VERSAO" to the receipt date of the filing.
func readITRIndex(f *zip.File, cnpj string) (map[string]string, error) {
	out := map[string]string{}
	if f == nil {
		return out, nil
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
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: cvm: %s: %w", f.Name, err)
		}
		if col(rec, idx, "CNPJ_CIA") != cnpj {
			continue
		}
		out[col(rec, idx, "DT_REFER")+"|"+col(rec, idx, "VERSAO")] = col(rec, idx, "DT_RECEB")
	}
}
