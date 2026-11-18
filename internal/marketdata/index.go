package marketdata

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// Default index sources.
const (
	DefaultB3IndexBaseURL = "https://sistemaswebb3-listados.b3.com.br"
	DefaultFREDBaseURL    = "https://fred.stlouisfed.org"
)

// IndexPoint is a daily index close (price index, no dividends).
type IndexPoint struct {
	Date  time.Time
	Close money.Decimal
}

// IndexSource returns daily closes of an index from a date on.
type IndexSource interface {
	Closes(ctx context.Context, from time.Time) ([]IndexPoint, error)
}

var ibovTitle = regexp.MustCompile(`(?i)^\s*([A-Z0-9^]+)\s*-\s*(\d{4})\s*$`)

var ptMonths = map[string]time.Month{
	"jan": time.January, "fev": time.February, "mar": time.March, "abr": time.April,
	"mai": time.May, "jun": time.June, "jul": time.July, "ago": time.August,
	"set": time.September, "out": time.October, "nov": time.November, "dez": time.December,
}

// ParseB3IndexMatrix parses B3's yearly index close matrix: a title line
// ("IBOV - 2026"), a header "Dia;Jan;...;Dez", then one row per day with
// pt-BR numbers and blanks on non-trading days, followed by summary rows
// (MINIMO/MAXIMO) that are ignored. Latin-1 or UTF-8 input. Points are sorted
// by date.
func ParseB3IndexMatrix(b []byte) ([]IndexPoint, error) {
	text := strings.ReplaceAll(decodeText(b), "\r\n", "\n")
	sc := bufio.NewScanner(strings.NewReader(text))
	var year int
	var monthCols map[int]time.Month
	var out []IndexPoint
	for ln := 1; sc.Scan(); ln++ {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		if year == 0 {
			m := ibovTitle.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("marketdata: b3 index: line %d: expected title like \"IBOV - 2026\", got %q", ln, l)
			}
			year, _ = strconv.Atoi(m[2])
			continue
		}
		fields := strings.Split(sc.Text(), ";")
		if monthCols == nil {
			if !strings.EqualFold(strings.TrimSpace(fields[0]), "Dia") {
				return nil, fmt.Errorf("marketdata: b3 index: line %d: expected header starting with Dia", ln)
			}
			monthCols = map[int]time.Month{}
			for i, f := range fields[1:] {
				m, ok := ptMonths[strings.ToLower(strings.TrimSpace(f))]
				if !ok {
					return nil, fmt.Errorf("marketdata: b3 index: line %d: unknown month %q", ln, f)
				}
				monthCols[i+1] = m
			}
			continue
		}
		day, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			break // summary rows start here
		}
		for i, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			month, ok := monthCols[i+1]
			if !ok {
				continue
			}
			d := Date(year, month, day)
			if d.Month() != month {
				return nil, fmt.Errorf("marketdata: b3 index: line %d: invalid date %d-%s-%d", ln, year, month, day)
			}
			v, err := parsePtBRDecimal(f)
			if err != nil {
				return nil, fmt.Errorf("marketdata: b3 index: line %d: %w", ln, err)
			}
			out = append(out, IndexPoint{Date: d, Close: v})
		}
	}
	if year == 0 || monthCols == nil {
		return nil, errors.New("marketdata: b3 index: empty or malformed matrix")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out, nil
}

// B3Index is a client for B3's (undocumented) index statistics endpoint.
type B3Index struct {
	HTTP    *http.Client
	BaseURL string
	Index   string // "IBOV"
}

// NewB3Index builds a client for the given index code.
func NewB3Index(c *http.Client, baseURL, index string) *B3Index {
	if baseURL == "" {
		baseURL = DefaultB3IndexBaseURL
	}
	return &B3Index{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/"), Index: index}
}

// Year fetches the close matrix of one calendar year. The endpoint takes a
// base64 JSON parameter in the path and returns the CSV base64-encoded (a raw
// CSV body is accepted too).
func (b *B3Index) Year(ctx context.Context, year int) ([]IndexPoint, error) {
	param := fmt.Sprintf(`{"index":%q,"language":"pt-br","year":"%d"}`, b.Index, year)
	u := b.BaseURL + "/indexStatisticsProxy/IndexCall/GetDownloadPortfolioDay/" + base64.StdEncoding.EncodeToString([]byte(param))
	body, err := getBytes(ctx, b.HTTP, "b3-index", u, nil)
	if err != nil {
		return nil, err
	}
	if dec, derr := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(body))); derr == nil {
		body = dec
	}
	pts, err := ParseB3IndexMatrix(body)
	if err != nil {
		return nil, fmt.Errorf("%w (from %s)", err, u)
	}
	return pts, nil
}

// Closes fetches every year from from.Year() to the current year (UTC) and
// returns the points on or after from.
func (b *B3Index) Closes(ctx context.Context, from time.Time) ([]IndexPoint, error) {
	var out []IndexPoint
	for y := from.Year(); y <= time.Now().UTC().Year(); y++ {
		pts, err := b.Year(ctx, y)
		if err != nil {
			return nil, err
		}
		for _, p := range pts {
			if !p.Date.Before(truncateDate(from)) {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

var _ IndexSource = (*B3Index)(nil)

// ParseFREDCSV parses a FRED fredgraph.csv (observation_date,<SERIES>).
// Blank or "." observations (holidays) are skipped.
func ParseFREDCSV(r io.Reader) ([]IndexPoint, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	hdr, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("marketdata: fred: reading header: %w", err)
	}
	if len(hdr) < 2 || !strings.EqualFold(strings.TrimSpace(hdr[0]), "observation_date") {
		return nil, fmt.Errorf("marketdata: fred: unexpected header %v", hdr)
	}
	var out []IndexPoint
	for n := 2; ; n++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("marketdata: fred: line %d: %w", n, err)
		}
		if len(rec) < 2 {
			continue
		}
		v := strings.TrimSpace(rec[1])
		if v == "" || v == "." {
			continue
		}
		d, err := time.Parse("2006-01-02", strings.TrimSpace(rec[0]))
		if err != nil {
			return nil, fmt.Errorf("marketdata: fred: line %d: %w", n, err)
		}
		dv, err := parseDecimal(v)
		if err != nil {
			return nil, fmt.Errorf("marketdata: fred: line %d: %w", n, err)
		}
		out = append(out, IndexPoint{Date: d.UTC(), Close: dv})
	}
}

// FRED is a client for FRED's key-less CSV download (S&P 500 price index).
// Check FRED's licence note on SP500 history before relying on it.
type FRED struct {
	HTTP    *http.Client
	BaseURL string
	Series  string // "SP500"
}

// NewFRED builds a client for a FRED series id.
func NewFRED(c *http.Client, baseURL, series string) *FRED {
	if baseURL == "" {
		baseURL = DefaultFREDBaseURL
	}
	return &FRED{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/"), Series: series}
}

// Closes returns observations on or after from. FRED lags by about a day.
func (f *FRED) Closes(ctx context.Context, from time.Time) ([]IndexPoint, error) {
	u := fmt.Sprintf("%s/graph/fredgraph.csv?id=%s&cosd=%s", f.BaseURL, f.Series, from.Format("2006-01-02"))
	resp, err := get(ctx, f.HTTP, "fred", u, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); strings.Contains(ct, "text/html") {
		return nil, fmt.Errorf("marketdata: fred: unexpected content type %q", ct)
	}
	return ParseFREDCSV(io.LimitReader(resp.Body, maxBodyBytes))
}

var _ IndexSource = (*FRED)(nil)
