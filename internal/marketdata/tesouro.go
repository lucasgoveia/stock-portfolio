package marketdata

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// DefaultTesouroCSVURL is the Tesouro Transparente "Taxas dos Titulos
// Ofertados" CSV (about 14 MB, all history since 2002).
const DefaultTesouroCSVURL = "https://www.tesourotransparente.gov.br/ckan/dataset/df56aa42-484a-4a59-8184-7676580c81e3/resource/796d2059-14e9-44e3-80c9-2d9e30b405c1/download/precotaxatesourodireto.csv"

// TesouroTitleSelic is the "Tipo Titulo" value of Tesouro Selic rows.
const TesouroTitleSelic = "Tesouro Selic"

// TesouroRow is one row of the CSV (morning indicative values of BaseDate).
type TesouroRow struct {
	Title    string
	Maturity time.Time
	BaseDate time.Time
	BuyRate  money.Decimal // Taxa Compra Manha
	SellRate money.Decimal // Taxa Venda Manha
	BuyPU    money.Decimal // PU Compra Manha
	SellPU   money.Decimal // PU Venda Manha (redemption price)
	BasePU   money.Decimal // PU Base Manha
}

// TesouroFilter selects rows. Zero fields match everything.
type TesouroFilter struct {
	Title      string    // exact, case-insensitive; typically TesouroTitleSelic
	Maturity   time.Time // zero = any maturity
	OnOrAfter  time.Time // keep rows with BaseDate >= this
	LatestOnly bool      // keep only rows of the latest BaseDate found
}

// TesouroResult is the outcome of parsing the CSV.
type TesouroResult struct {
	Rows []TesouroRow
	// Reference is the newest BaseDate among rows matching Title and Maturity
	// (the "as of" date; the file lags one business day, so D-1). Zero when
	// nothing matched.
	Reference time.Time
}

// TesouroSource fetches Tesouro prices.
type TesouroSource interface {
	Prices(ctx context.Context, f TesouroFilter) (TesouroResult, error)
}

// ParseTesouroCSV streams the ";"-separated, decimal-comma CSV and keeps the
// rows accepted by f. Columns are located by header name.
func ParseTesouroCSV(ctx context.Context, r io.Reader, f TesouroFilter) (TesouroResult, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	hdr, err := cr.Read()
	if err != nil {
		return TesouroResult{}, fmt.Errorf("marketdata: tesouro: reading header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range hdr {
		idx[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	cols := []string{"Tipo Titulo", "Data Vencimento", "Data Base", "Taxa Compra Manha", "Taxa Venda Manha", "PU Compra Manha", "PU Venda Manha", "PU Base Manha"}
	col := make([]int, len(cols))
	for i, c := range cols {
		j, ok := idx[c]
		if !ok {
			return TesouroResult{}, fmt.Errorf("marketdata: tesouro: missing column %q", c)
		}
		col[i] = j
	}
	var res TesouroResult
	for n := 2; ; n++ {
		if n%8192 == 0 {
			if err := ctx.Err(); err != nil {
				return TesouroResult{}, err
			}
		}
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return TesouroResult{}, fmt.Errorf("marketdata: tesouro: line %d: %w", n, err)
		}
		if len(rec) <= col[len(col)-1] {
			continue
		}
		title := strings.TrimSpace(rec[col[0]])
		if f.Title != "" && !strings.EqualFold(title, f.Title) {
			continue
		}
		mat, err := time.Parse("02/01/2006", strings.TrimSpace(rec[col[1]]))
		if err != nil {
			return TesouroResult{}, fmt.Errorf("marketdata: tesouro: line %d maturity: %w", n, err)
		}
		if !f.Maturity.IsZero() && !mat.Equal(truncateDate(f.Maturity)) {
			continue
		}
		base, err := time.Parse("02/01/2006", strings.TrimSpace(rec[col[2]]))
		if err != nil {
			return TesouroResult{}, fmt.Errorf("marketdata: tesouro: line %d base date: %w", n, err)
		}
		if base.After(res.Reference) {
			res.Reference = base
		}
		if !f.OnOrAfter.IsZero() && base.Before(truncateDate(f.OnOrAfter)) {
			continue
		}
		row := TesouroRow{Title: title, Maturity: mat, BaseDate: base}
		dst := []*money.Decimal{&row.BuyRate, &row.SellRate, &row.BuyPU, &row.SellPU, &row.BasePU}
		for k, d := range dst {
			if *d, err = parsePtBRDecimal(rec[col[3+k]]); err != nil {
				return TesouroResult{}, fmt.Errorf("marketdata: tesouro: line %d %s: %w", n, cols[3+k], err)
			}
		}
		res.Rows = append(res.Rows, row)
	}
	if f.LatestOnly {
		kept := res.Rows[:0]
		for _, r := range res.Rows {
			if r.BaseDate.Equal(res.Reference) {
				kept = append(kept, r)
			}
		}
		res.Rows = kept
	}
	return res, nil
}

// Tesouro downloads and parses the Tesouro Transparente CSV.
type Tesouro struct {
	HTTP *http.Client
	URL  string
}

var _ TesouroSource = (*Tesouro)(nil)

// NewTesouro builds a client; empty url uses DefaultTesouroCSVURL. A nil client
// gets one without an overall timeout (14 MB file); bound with ctx.
func NewTesouro(c *http.Client, url string) *Tesouro {
	if c == nil {
		c = &http.Client{}
	}
	if url == "" {
		url = DefaultTesouroCSVURL
	}
	return &Tesouro{HTTP: c, URL: url}
}

// Prices streams the CSV and filters it. The file is refreshed around 07:20
// BRT with the previous business day as base date.
func (t *Tesouro) Prices(ctx context.Context, f TesouroFilter) (TesouroResult, error) {
	resp, err := get(ctx, t.HTTP, "tesouro", t.URL, nil)
	if err != nil {
		return TesouroResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); strings.Contains(ct, "text/html") {
		return TesouroResult{}, fmt.Errorf("marketdata: tesouro: unexpected content type %q", ct)
	}
	return ParseTesouroCSV(ctx, resp.Body, f)
}
