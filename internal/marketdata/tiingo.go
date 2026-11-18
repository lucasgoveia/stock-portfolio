package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// DefaultTiingoBaseURL is the Tiingo API root.
const DefaultTiingoBaseURL = "https://api.tiingo.com"

// DailyBar is one end-of-day row with raw and adjusted prices.
type DailyBar struct {
	Date        time.Time
	Open        money.Decimal
	High        money.Decimal
	Low         money.Decimal
	Close       money.Decimal // raw
	Volume      money.Decimal
	AdjClose    money.Decimal
	DivCash     money.Decimal // cash dividend with ex-date on this day, 0 normally
	SplitFactor money.Decimal // 1 normally; 2 for a 2:1 split
}

// USPriceSource returns daily US prices for a ticker.
type USPriceSource interface {
	DailyPrices(ctx context.Context, ticker string, from, to time.Time) ([]DailyBar, error)
}

// Tiingo is a client for Tiingo's end-of-day API.
//
// UNVERIFIED against the live API: no key existed when this was written. The
// request and response shapes come from Tiingo's documentation
// (https://www.tiingo.com/documentation/end-of-day) and the test fixture is
// hand-written to match. The Authorization header format, the exact date
// format and the quota-exceeded response (expected HTTP 429) are unconfirmed.
type Tiingo struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

var _ USPriceSource = (*Tiingo)(nil)

// NewTiingo builds a client; token is required.
func NewTiingo(c *http.Client, baseURL, token string) *Tiingo {
	if baseURL == "" {
		baseURL = DefaultTiingoBaseURL
	}
	return &Tiingo{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/"), Token: token}
}

// DailyPrices returns bars between from and to inclusive (zero values leave a
// bound open; with both zero Tiingo returns only the latest bar).
func (t *Tiingo) DailyPrices(ctx context.Context, ticker string, from, to time.Time) ([]DailyBar, error) {
	if t.Token == "" {
		return nil, errors.New("marketdata: tiingo: API token not configured")
	}
	q := url.Values{"format": {"json"}, "resampleFreq": {"daily"}}
	if !from.IsZero() {
		q.Set("startDate", from.Format("2006-01-02"))
	}
	if !to.IsZero() {
		q.Set("endDate", to.Format("2006-01-02"))
	}
	u := fmt.Sprintf("%s/tiingo/daily/%s/prices?%s", t.BaseURL, url.PathEscape(strings.ToLower(ticker)), q.Encode())
	var rows []struct {
		Date        string      `json:"date"`
		Open        json.Number `json:"open"`
		High        json.Number `json:"high"`
		Low         json.Number `json:"low"`
		Close       json.Number `json:"close"`
		Volume      json.Number `json:"volume"`
		AdjClose    json.Number `json:"adjClose"`
		DivCash     json.Number `json:"divCash"`
		SplitFactor json.Number `json:"splitFactor"`
	}
	hdr := map[string]string{"Authorization": "Token " + t.Token, "Accept": "application/json"}
	if err := getJSON(ctx, t.HTTP, "tiingo", u, hdr, &rows); err != nil {
		return nil, err
	}
	out := make([]DailyBar, 0, len(rows))
	for i, r := range rows {
		if len(r.Date) < 10 {
			return nil, fmt.Errorf("marketdata: tiingo: row %d: bad date %q", i, r.Date)
		}
		d, err := time.Parse("2006-01-02", r.Date[:10])
		if err != nil {
			return nil, fmt.Errorf("marketdata: tiingo: row %d: %w", i, err)
		}
		bar := DailyBar{Date: d.UTC()}
		fields := []struct {
			dst  *money.Decimal
			n    json.Number
			name string
		}{
			{&bar.Open, r.Open, "open"}, {&bar.High, r.High, "high"}, {&bar.Low, r.Low, "low"},
			{&bar.Close, r.Close, "close"}, {&bar.Volume, r.Volume, "volume"},
			{&bar.AdjClose, r.AdjClose, "adjClose"}, {&bar.DivCash, r.DivCash, "divCash"},
			{&bar.SplitFactor, r.SplitFactor, "splitFactor"},
		}
		for _, f := range fields {
			if *f.dst, err = jsonDecimal(f.n); err != nil {
				return nil, fmt.Errorf("marketdata: tiingo: row %d %s: %w", i, f.name, err)
			}
		}
		out = append(out, bar)
	}
	return out, nil
}
