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

// DefaultBrapiBaseURL is the brapi.dev API root.
const DefaultBrapiBaseURL = "https://brapi.dev"

// Quote is the latest price of an instrument.
type Quote struct {
	Ticker          string // resolved symbol (brapi may rename a ticker)
	RequestedTicker string
	Price           money.Decimal
	Currency        string
	At              time.Time // instant of the quote (UTC)
	Date            time.Time // civil date of the quote in UTC
}

// QuoteSource returns the latest quote for one ticker.
type QuoteSource interface {
	LatestQuote(ctx context.Context, ticker string) (Quote, error)
}

// CashDividend is a dividend/JCP row. Rate is gross BRL per share on the
// split-adjusted scale (brapi retroactively adjusts it for later splits).
type CashDividend struct {
	Label                 string // DIVIDENDO, JCP, ...
	Rate                  money.Decimal
	ISIN                  string
	LastDatePrior         time.Time  // data-com
	ExDate                *time.Time // nullable
	PaymentDate           *time.Time // nullable; may be a deadline
	PaymentDateIsDeadline bool
	ApprovedOn            *time.Time
	Verified              bool
}

// StockDividend is a split/reverse split/bonus row. Factor is brapi's
// multiplier: 2 means "2 for 1", 0.01 "1 for 100". The BONIFICACAO label is
// observed in the fixture (factor 1.3333334).
type StockDividend struct {
	Label          string // DESDOBRAMENTO, GRUPAMENTO, BONIFICACAO
	Factor         money.Decimal
	CompleteFactor string
	ISIN           string
	LastDatePrior  time.Time
	ExDate         *time.Time // null in every observed row
	ApprovedOn     *time.Time
}

// Subscription is a rights issue row. The shape is UNVERIFIED (empty in every
// observed response); Raw keeps the original JSON.
type Subscription struct {
	Label         string
	ISIN          string
	LastDatePrior *time.Time
	ExDate        *time.Time
	ApprovedOn    *time.Time
	Raw           json.RawMessage
}

// CorporateActions groups everything the dividends endpoint returns.
type CorporateActions struct {
	Ticker         string
	CashDividends  []CashDividend
	StockDividends []StockDividend
	Subscriptions  []Subscription
}

// DividendSource returns dividends and corporate actions for one ticker.
// from/to are optional (zero means unbounded).
type DividendSource interface {
	CorporateActions(ctx context.Context, ticker string, from, to time.Time) (CorporateActions, error)
}

// Brapi is a client for brapi.dev. Free plan: one asset per request.
type Brapi struct {
	HTTP    *http.Client
	BaseURL string
	Token   string // empty works only for the sandbox tickers
}

var (
	_ QuoteSource    = (*Brapi)(nil)
	_ DividendSource = (*Brapi)(nil)
)

// NewBrapi builds a client; nil client uses a default with DefaultTimeout.
func NewBrapi(c *http.Client, baseURL, token string) *Brapi {
	if baseURL == "" {
		baseURL = DefaultBrapiBaseURL
	}
	return &Brapi{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/"), Token: token}
}

func (b *Brapi) headers() map[string]string {
	h := map[string]string{"Accept": "application/json"}
	if b.Token != "" {
		h["Authorization"] = "Bearer " + b.Token
	}
	return h
}

type brapiEnvelope struct {
	Results []struct {
		RequestedSymbol string          `json:"requestedSymbol"`
		Symbol          string          `json:"symbol"`
		Data            json.RawMessage `json:"data"`
	} `json:"results"`
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

func (b *Brapi) fetch(ctx context.Context, path string, q url.Values) (*brapiEnvelope, error) {
	var env brapiEnvelope
	if err := getJSON(ctx, b.HTTP, "brapi", b.BaseURL+path+"?"+q.Encode(), b.headers(), &env); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.Status == http.StatusForbidden && he.Code == "FEATURE_NOT_AVAILABLE" {
			return nil, fmt.Errorf("marketdata: brapi: %w (%s)", ErrPlanNotAvailable, truncateRunes(he.Message, 200))
		}
		return nil, err
	}
	if env.Error {
		return nil, fmt.Errorf("marketdata: brapi: %s", env.Message)
	}
	if len(env.Results) == 0 {
		return nil, fmt.Errorf("marketdata: brapi: empty results")
	}
	return &env, nil
}

// LatestQuote returns the latest B3 quote (free plan: 30 minute delay).
func (b *Brapi) LatestQuote(ctx context.Context, ticker string) (Quote, error) {
	env, err := b.fetch(ctx, "/api/v2/stocks/quote", url.Values{"symbols": {ticker}})
	if err != nil {
		return Quote{}, err
	}
	r := env.Results[0]
	var d struct {
		Currency string      `json:"currency"`
		Price    json.Number `json:"regularMarketPrice"`
		Time     string      `json:"regularMarketTime"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return Quote{}, fmt.Errorf("marketdata: brapi: decoding quote for %s: %w", ticker, err)
	}
	if d.Price == "" {
		return Quote{}, fmt.Errorf("marketdata: brapi: no regularMarketPrice for %s", ticker)
	}
	price, err := jsonDecimal(d.Price)
	if err != nil {
		return Quote{}, fmt.Errorf("marketdata: brapi: %s price: %w", ticker, err)
	}
	at, err := time.Parse(time.RFC3339, d.Time)
	if err != nil {
		return Quote{}, fmt.Errorf("marketdata: brapi: %s regularMarketTime %q: %w", ticker, d.Time, err)
	}
	at = at.UTC()
	return Quote{
		Ticker: r.Symbol, RequestedTicker: r.RequestedSymbol, Price: price,
		Currency: d.Currency, At: at, Date: truncateDate(at),
	}, nil
}

type brapiDate struct{ Time *time.Time }

func (d *brapiDate) UnmarshalJSON(b []byte) error {
	if string(b) == "null" || string(b) == `""` {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("invalid date %q: %w", s, err)
	}
	// brapi sends B3 civil dates as local midnight (03:00Z); the UTC date is
	// the civil date.
	v := truncateDate(t)
	d.Time = &v
	return nil
}

func (d brapiDate) ptr() *time.Time { return d.Time }

// CorporateActions fetches cash dividends, stock dividends and subscriptions.
// The Free plan documents dividends as "Limited" (unquantified).
func (b *Brapi) CorporateActions(ctx context.Context, ticker string, from, to time.Time) (CorporateActions, error) {
	q := url.Values{"symbols": {ticker}}
	if !from.IsZero() {
		q.Set("startDate", from.Format("2006-01-02"))
	}
	if !to.IsZero() {
		q.Set("endDate", to.Format("2006-01-02"))
	}
	env, err := b.fetch(ctx, "/api/v2/stocks/dividends", q)
	if err != nil {
		return CorporateActions{}, err
	}
	r := env.Results[0]
	var d struct {
		Cash []struct {
			Label    string      `json:"label"`
			Rate     json.Number `json:"rate"`
			ISIN     string      `json:"isinCode"`
			Last     brapiDate   `json:"lastDatePrior"`
			Ex       brapiDate   `json:"exDate"`
			Pay      brapiDate   `json:"paymentDate"`
			Deadline bool        `json:"paymentDateIsDeadline"`
			Approved brapiDate   `json:"approvedOn"`
			Verified bool        `json:"verified"`
		} `json:"cashDividends"`
		Stock []struct {
			Label    string      `json:"label"`
			Factor   json.Number `json:"factor"`
			Complete string      `json:"completeFactor"`
			ISIN     string      `json:"isinCode"`
			Last     brapiDate   `json:"lastDatePrior"`
			Ex       brapiDate   `json:"exDate"`
			Approved brapiDate   `json:"approvedOn"`
		} `json:"stockDividends"`
		Subs []json.RawMessage `json:"subscriptions"`
	}
	if err := json.Unmarshal(r.Data, &d); err != nil {
		return CorporateActions{}, fmt.Errorf("marketdata: brapi: decoding dividends for %s: %w", ticker, err)
	}
	out := CorporateActions{Ticker: r.Symbol}
	for i, c := range d.Cash {
		rate, err := jsonDecimal(c.Rate)
		if err != nil {
			return CorporateActions{}, fmt.Errorf("marketdata: brapi: cashDividends[%d].rate: %w", i, err)
		}
		cd := CashDividend{
			Label: c.Label, Rate: rate, ISIN: c.ISIN, ExDate: c.Ex.ptr(), PaymentDate: c.Pay.ptr(),
			PaymentDateIsDeadline: c.Deadline, ApprovedOn: c.Approved.ptr(), Verified: c.Verified,
		}
		if c.Last.Time != nil {
			cd.LastDatePrior = *c.Last.Time
		}
		out.CashDividends = append(out.CashDividends, cd)
	}
	for i, s := range d.Stock {
		f, err := jsonDecimal(s.Factor)
		if err != nil {
			return CorporateActions{}, fmt.Errorf("marketdata: brapi: stockDividends[%d].factor: %w", i, err)
		}
		sd := StockDividend{
			Label: s.Label, Factor: f, CompleteFactor: s.Complete, ISIN: s.ISIN,
			ExDate: s.Ex.ptr(), ApprovedOn: s.Approved.ptr(),
		}
		if s.Last.Time != nil {
			sd.LastDatePrior = *s.Last.Time
		}
		out.StockDividends = append(out.StockDividends, sd)
	}
	for i, raw := range d.Subs {
		var s struct {
			Label    string    `json:"label"`
			ISIN     string    `json:"isinCode"`
			Last     brapiDate `json:"lastDatePrior"`
			Ex       brapiDate `json:"exDate"`
			Approved brapiDate `json:"approvedOn"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			return CorporateActions{}, fmt.Errorf("marketdata: brapi: subscriptions[%d]: %w", i, err)
		}
		out.Subscriptions = append(out.Subscriptions, Subscription{
			Label: s.Label, ISIN: s.ISIN, LastDatePrior: s.Last.ptr(), ExDate: s.Ex.ptr(),
			ApprovedOn: s.Approved.ptr(), Raw: raw,
		})
	}
	return out, nil
}
