package marketdata

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/shopspring/decimal"
)

// DefaultCotahistBaseURL is where B3 publishes the COTAHIST zip files.
const DefaultCotahistBaseURL = "https://bvmf.bmfbovespa.com.br/InstDados/SerHist/"

// COTAHIST market types (TPMERC).
const (
	MarketLot        = "010" // mercado a vista
	MarketFractional = "020" // mercado fracionario (ticker + "F")
)

const cotahistRecordLen = 245

// CotahistRecord is one type-01 record (a security's trading day).
// Prices are raw (unadjusted) trade prices.
type CotahistRecord struct {
	Date      time.Time // DATA
	BDI       string    // CODBDI
	Ticker    string    // CODNEG, trimmed
	Market    string    // TPMERC
	ShortName string    // NOMRES
	Spec      string    // ESPECI
	Open      money.Decimal
	High      money.Decimal
	Low       money.Decimal
	Avg       money.Decimal
	Close     money.Decimal // PREULT
	Bid       money.Decimal
	Ask       money.Decimal
	Trades    int64
	Quantity  int64
	Volume    money.Decimal // financial volume
	QuoteFact int64         // FATCOT
	ISIN      string        // CODISI, used to join renamed tickers
	Distrib   string        // DISMES
}

// CotahistFilter selects records before they are fully parsed. Empty fields
// match everything.
type CotahistFilter struct {
	Tickers []string // exact CODNEG matches (e.g. "PETR4", "PETR4F")
	Markets []string // TPMERC values (e.g. MarketLot)
}

func (f CotahistFilter) matches(ticker, market string) bool {
	if len(f.Markets) > 0 && !contains(f.Markets, market) {
		return false
	}
	return len(f.Tickers) == 0 || contains(f.Tickers, ticker)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// FractionalTicker returns the fractional-market ticker of a lot ticker.
func FractionalTicker(lot string) string { return lot + "F" }

// LotFilter selects the lot market (010) rows of the given tickers.
func LotFilter(tickers ...string) CotahistFilter {
	return CotahistFilter{Tickers: tickers, Markets: []string{MarketLot}}
}

// FractionalFilter selects the fractional market (020) rows of the lot tickers
// (it appends the "F" suffix).
func FractionalFilter(lotTickers ...string) CotahistFilter {
	t := make([]string, len(lotTickers))
	for i, l := range lotTickers {
		t[i] = FractionalTicker(l)
	}
	return CotahistFilter{Tickers: t, Markets: []string{MarketFractional}}
}

// CotahistFileKind selects the file granularity.
type CotahistFileKind int

// File granularities.
const (
	CotahistYearly CotahistFileKind = iota
	CotahistMonthly
	CotahistDaily
)

// CotahistFileName returns the zip name for t (for example COTAHIST_A2026.ZIP,
// COTAHIST_M102026.ZIP, COTAHIST_D17112026.ZIP).
func CotahistFileName(kind CotahistFileKind, t time.Time) string {
	switch kind {
	case CotahistMonthly:
		return fmt.Sprintf("COTAHIST_M%02d%04d.ZIP", int(t.Month()), t.Year())
	case CotahistDaily:
		return fmt.Sprintf("COTAHIST_D%02d%02d%04d.ZIP", t.Day(), int(t.Month()), t.Year())
	default:
		return fmt.Sprintf("COTAHIST_A%04d.ZIP", t.Year())
	}
}

// CotahistURL returns the download URL (baseURL may be empty for the default).
func CotahistURL(baseURL string, kind CotahistFileKind, t time.Time) string {
	if baseURL == "" {
		baseURL = DefaultCotahistBaseURL
	}
	return strings.TrimRight(baseURL, "/") + "/" + CotahistFileName(kind, t)
}

// line gives character-based slicing of a record, tolerating the rare
// non-ASCII (UTF-8 or Latin-1) record whose byte offsets would otherwise shift.
type line struct {
	ascii string
	runes []rune
}

func newLine(b []byte) line {
	for _, c := range b {
		if c >= utf8.RuneSelf {
			if utf8.Valid(b) {
				return line{runes: []rune(string(b))}
			}
			return line{runes: []rune(latin1(b))}
		}
	}
	return line{ascii: string(b)}
}

func (l line) len() int {
	if l.runes != nil {
		return len(l.runes)
	}
	return len(l.ascii)
}

// sub returns characters pos1..pos2 (1-based, inclusive).
func (l line) sub(pos1, pos2 int) string {
	if l.runes != nil {
		return string(l.runes[pos1-1 : pos2])
	}
	return l.ascii[pos1-1 : pos2]
}

// price parses an N(width)V99 field.
func (l line) scaled(pos1, pos2 int) (money.Decimal, error) {
	n, err := strconv.ParseInt(l.sub(pos1, pos2), 10, 64)
	if err != nil {
		return money.Decimal{}, fmt.Errorf("positions %d-%d: %w", pos1, pos2, err)
	}
	return money.New(decimal.New(n, -2)), nil
}

func (l line) integer(pos1, pos2 int) (int64, error) {
	n, err := strconv.ParseInt(l.sub(pos1, pos2), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("positions %d-%d: %w", pos1, pos2, err)
	}
	return n, nil
}

func parseCotahistLine(l line) (CotahistRecord, error) {
	var r CotahistRecord
	var err error
	date, err := time.Parse("20060102", l.sub(3, 10))
	if err != nil {
		return r, fmt.Errorf("DATA: %w", err)
	}
	r.Date = date.UTC()
	r.BDI = l.sub(11, 12)
	r.Ticker = strings.TrimSpace(l.sub(13, 24))
	r.Market = l.sub(25, 27)
	r.ShortName = strings.TrimSpace(l.sub(28, 39))
	r.Spec = strings.TrimSpace(l.sub(40, 49))
	prices := []struct {
		dst      *money.Decimal
		from, to int
	}{
		{&r.Open, 57, 69}, {&r.High, 70, 82}, {&r.Low, 83, 95}, {&r.Avg, 96, 108},
		{&r.Close, 109, 121}, {&r.Bid, 122, 134}, {&r.Ask, 135, 147}, {&r.Volume, 171, 188},
	}
	for _, p := range prices {
		if *p.dst, err = l.scaled(p.from, p.to); err != nil {
			return r, err
		}
	}
	if r.Trades, err = l.integer(148, 152); err != nil {
		return r, err
	}
	if r.Quantity, err = l.integer(153, 170); err != nil {
		return r, err
	}
	if r.QuoteFact, err = l.integer(211, 217); err != nil {
		return r, err
	}
	r.ISIN = strings.TrimSpace(l.sub(231, 242))
	r.Distrib = strings.TrimSpace(l.sub(243, 245))
	return r, nil
}

// ScanCotahist streams a COTAHIST text file (records of 245 characters plus
// CRLF), calling fn for each type-01 record accepted by filter. Header (00)
// and trailer (99) records are skipped. Lines are decoded as UTF-8 with
// Latin-1 fallback and sliced by character. ctx is checked periodically.
func ScanCotahist(ctx context.Context, r io.Reader, filter CotahistFilter, fn func(CotahistRecord) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	for n := 1; ; n++ {
		if n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		raw, err := br.ReadBytes('\n')
		if len(raw) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("marketdata: cotahist: reading line %d: %w", n, err)
		}
		for len(raw) > 0 && (raw[len(raw)-1] == '\n' || raw[len(raw)-1] == '\r') {
			raw = raw[:len(raw)-1]
		}
		if len(raw) < 2 || raw[0] != '0' || raw[1] != '1' {
			continue // header, trailer or blank
		}
		l := newLine(raw)
		if l.len() < cotahistRecordLen {
			return fmt.Errorf("marketdata: cotahist: line %d has %d characters, want %d", n, l.len(), cotahistRecordLen)
		}
		if !filter.matches(strings.TrimSpace(l.sub(13, 24)), l.sub(25, 27)) {
			continue
		}
		rec, perr := parseCotahistLine(l)
		if perr != nil {
			return fmt.Errorf("marketdata: cotahist: line %d: %w", n, perr)
		}
		if ferr := fn(rec); ferr != nil {
			return ferr
		}
	}
}

// ParseCotahist collects the records accepted by filter. For a given
// (date, ticker, market) only the first record is kept.
func ParseCotahist(ctx context.Context, r io.Reader, filter CotahistFilter) ([]CotahistRecord, error) {
	type key struct {
		d      time.Time
		t, mkt string
	}
	seen := map[key]bool{}
	var out []CotahistRecord
	err := ScanCotahist(ctx, r, filter, func(rec CotahistRecord) error {
		k := key{rec.Date, rec.Ticker, rec.Market}
		if !seen[k] {
			seen[k] = true
			out = append(out, rec)
		}
		return nil
	})
	return out, err
}

// ScanCotahistZip scans the first .TXT entry of a zip archive.
func ScanCotahistZip(ctx context.Context, zr *zip.Reader, filter CotahistFilter, fn func(CotahistRecord) error) error {
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToUpper(f.Name), ".TXT") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("marketdata: cotahist: opening %s: %w", f.Name, err)
		}
		defer func() { _ = rc.Close() }()
		return ScanCotahist(ctx, rc, filter, fn)
	}
	return errors.New("marketdata: cotahist: no .TXT entry in zip")
}

// Cotahist downloads and scans COTAHIST zip files.
type Cotahist struct {
	HTTP    *http.Client // should have no overall Timeout for yearly files; use ctx
	BaseURL string
}

// NewCotahist builds a client. A nil client gets one without an overall
// timeout (yearly files are ~95 MB); bound the call with ctx.
func NewCotahist(c *http.Client, baseURL string) *Cotahist {
	if c == nil {
		c = &http.Client{}
	}
	if baseURL == "" {
		baseURL = DefaultCotahistBaseURL
	}
	return &Cotahist{HTTP: c, BaseURL: baseURL}
}

// Scan downloads the file for (kind, t) to a temporary file and streams its
// records through fn. A missing daily file (not yet published, about 21:00
// BRT) yields an error for which IsNotFound is true.
func (c *Cotahist) Scan(ctx context.Context, kind CotahistFileKind, t time.Time, filter CotahistFilter, fn func(CotahistRecord) error) error {
	url := CotahistURL(c.BaseURL, kind, t)
	resp, err := get(ctx, c.HTTP, "cotahist", url, map[string]string{"User-Agent": "Mozilla/5.0 (stock-portfolio)"})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	tmp, err := os.CreateTemp("", "cotahist-*.zip")
	if err != nil {
		return fmt.Errorf("marketdata: cotahist: temp file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("marketdata: cotahist: downloading %s: %w", url, err)
	}
	st, err := tmp.Stat()
	if err != nil {
		return fmt.Errorf("marketdata: cotahist: %w", err)
	}
	zr, err := zip.NewReader(tmp, st.Size())
	if err != nil {
		return fmt.Errorf("marketdata: cotahist: %s is not a zip: %w", url, err)
	}
	return ScanCotahistZip(ctx, zr, filter, fn)
}
