// Package marketdata contains clients and parsers for external market data
// sources (brapi, B3 COTAHIST, BCB, Tesouro Transparente, B3 index matrix,
// FRED, Tiingo). Clients take an *http.Client and a base URL so tests can use
// httptest servers serving fixtures. Prices and rates are money.Decimal, never
// floats; dates are civil dates represented as time.Time at UTC midnight.
//
// Persistence and scheduling are out of scope here; every client returns plain
// value types that a caller can store.
package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
	"github.com/shopspring/decimal"
)

// DefaultTimeout is the overall timeout of the HTTP client used when a
// constructor receives a nil *http.Client.
const DefaultTimeout = 30 * time.Second

// Date returns the civil date y-m-d as time.Time at UTC midnight.
func Date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// truncateDate returns the UTC civil date of t.
func truncateDate(t time.Time) time.Time {
	t = t.UTC()
	return Date(t.Year(), t.Month(), t.Day())
}

// HTTPError is returned for non-2xx responses.
type HTTPError struct {
	Source string
	URL    string
	Status int
	Body   string // truncated response body
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("marketdata: %s: HTTP %d from %s: %s", e.Source, e.Status, e.URL, e.Body)
}

// IsNotFound reports whether err is an HTTP 404 (for example a COTAHIST daily
// file that has not been published yet).
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusNotFound
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: DefaultTimeout}
}

// get performs a GET and returns the response with a 2xx status. The caller
// closes the body.
func get(ctx context.Context, c *http.Client, source, url string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("marketdata: %s: building request: %w", source, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpClient(c).Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketdata: %s: %w", source, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, &HTTPError{Source: source, URL: url, Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return resp, nil
}

const maxBodyBytes = 64 << 20

// getBytes fetches a small/medium body.
func getBytes(ctx context.Context, c *http.Client, source, url string, hdr map[string]string) ([]byte, error) {
	resp, err := get(ctx, c, source, url, hdr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("marketdata: %s: reading body: %w", source, err)
	}
	return b, nil
}

// getJSON fetches url and decodes the JSON body into v. It rejects HTML
// responses (some providers answer 200 with an HTML block page).
func getJSON(ctx context.Context, c *http.Client, source, url string, hdr map[string]string, v any) error {
	resp, err := get(ctx, c, source, url, hdr)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); strings.Contains(strings.ToLower(ct), "text/html") {
		return fmt.Errorf("marketdata: %s: unexpected content type %q from %s", source, ct, url)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("marketdata: %s: decoding JSON from %s: %w", source, url, err)
	}
	return nil
}

// parseDecimal parses a plain decimal string ("5.7851").
func parseDecimal(s string) (money.Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return money.Decimal{}, fmt.Errorf("invalid decimal %q: %w", s, err)
	}
	return money.New(d), nil
}

// parsePtBRDecimal parses "1.234,56" style numbers (thousands dot, decimal
// comma).
func parsePtBRDecimal(s string) (money.Decimal, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	return parseDecimal(s)
}

// decodeText returns b as a string, interpreting it as UTF-8 when valid and as
// Latin-1 otherwise. A leading UTF-8 BOM is removed.
func decodeText(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b)
	}
	return latin1(b)
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// jsonNumber converts a decoded JSON number (json.Number, because decoders use
// UseNumber) or numeric string to a Decimal.
func jsonDecimal(n json.Number) (money.Decimal, error) {
	return parseDecimal(n.String())
}
