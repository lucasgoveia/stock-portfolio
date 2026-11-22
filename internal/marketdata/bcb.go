package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/money"
)

// Default BCB base URLs.
const (
	DefaultOlindaBaseURL = "https://olinda.bcb.gov.br"
	DefaultSGSBaseURL    = "https://api.bcb.gov.br"
)

// SGS series codes.
const (
	SGSSeriesUSDSell = 1  // USD sell, equals PTAX sell
	SGSSeriesCDI     = 12 // CDI daily rate, percent per business day
	// SGSSeriesSelicTarget is the Selic target (Meta Selic), percent per year.
	SGSSeriesSelicTarget = 432
)

// sgsYears is the longest window SGS accepts for daily series; wider
// requests return HTTP 406. Chunks are 10 years minus a day.
const sgsYears = 10

// FXRate is one PTAX bulletin (closing window of a business day).
type FXRate struct {
	Date time.Time // civil date of the bulletin
	At   time.Time // bulletin timestamp as published (BRT wall time, labelled UTC)
	Buy  money.Decimal
	Sell money.Decimal // use as the USDBRL rate by convention
}

// FXSource returns USDBRL rates for a date range (inclusive).
type FXSource interface {
	USDBRL(ctx context.Context, from, to time.Time) ([]FXRate, error)
}

// RatePoint is a dated value of a BCB SGS series.
type RatePoint struct {
	Date  time.Time
	Value money.Decimal
}

// CDISource returns daily CDI points. Values are percent per business day
// (0.050788 means 0.050788 %).
type CDISource interface {
	CDI(ctx context.Context, from, to time.Time) ([]RatePoint, error)
}

// SelicTargetSource returns Selic target points between from and to, percent
// per year (15.00 means 15 %).
type SelicTargetSource interface {
	SelicTarget(ctx context.Context, from, to time.Time) ([]RatePoint, error)
}

// PTAX is a client for the BCB Olinda PTAX service.
type PTAX struct {
	HTTP    *http.Client
	BaseURL string
}

var _ FXSource = (*PTAX)(nil)

// NewPTAX builds a client.
func NewPTAX(c *http.Client, baseURL string) *PTAX {
	if baseURL == "" {
		baseURL = DefaultOlindaBaseURL
	}
	return &PTAX{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/")}
}

// USDBRL returns the PTAX bulletins between from and to inclusive. Olinda uses
// US-style MM-DD-YYYY dates. The BCB publishes one closing bulletin per
// business day (about 13:00 BRT).
func (p *PTAX) USDBRL(ctx context.Context, from, to time.Time) ([]FXRate, error) {
	u := fmt.Sprintf("%s/olinda/servico/PTAX/versao/v1/odata/CotacaoDolarPeriodo(dataInicial=@dataInicial,dataFinalCotacao=@dataFinalCotacao)?@dataInicial='%s'&@dataFinalCotacao='%s'&$top=10000&$format=json",
		p.BaseURL, from.Format("01-02-2006"), to.Format("01-02-2006"))
	var body struct {
		Value []struct {
			Buy  json.Number `json:"cotacaoCompra"`
			Sell json.Number `json:"cotacaoVenda"`
			At   string      `json:"dataHoraCotacao"`
		} `json:"value"`
	}
	if err := getJSON(ctx, p.HTTP, "bcb-ptax", u, nil, &body); err != nil {
		return nil, err
	}
	out := make([]FXRate, 0, len(body.Value))
	for i, v := range body.Value {
		if len(v.At) < 10 {
			return nil, fmt.Errorf("marketdata: bcb-ptax: value[%d]: bad dataHoraCotacao %q", i, v.At)
		}
		d, err := time.Parse("2006-01-02", v.At[:10])
		if err != nil {
			return nil, fmt.Errorf("marketdata: bcb-ptax: value[%d]: %w", i, err)
		}
		buy, err := jsonDecimal(v.Buy)
		if err != nil {
			return nil, fmt.Errorf("marketdata: bcb-ptax: value[%d] buy: %w", i, err)
		}
		sell, err := jsonDecimal(v.Sell)
		if err != nil {
			return nil, fmt.Errorf("marketdata: bcb-ptax: value[%d] sell: %w", i, err)
		}
		at, _ := time.Parse("2006-01-02 15:04:05.999999", v.At) // wall time; best effort
		out = append(out, FXRate{Date: d.UTC(), At: at, Buy: buy, Sell: sell})
	}
	return out, nil
}

// SGS is a client for the BCB SGS time-series API.
type SGS struct {
	HTTP    *http.Client
	BaseURL string
}

var (
	_ CDISource         = (*SGS)(nil)
	_ SelicTargetSource = (*SGS)(nil)
)

// NewSGS builds a client.
func NewSGS(c *http.Client, baseURL string) *SGS {
	if baseURL == "" {
		baseURL = DefaultSGSBaseURL
	}
	return &SGS{HTTP: httpClient(c), BaseURL: strings.TrimRight(baseURL, "/")}
}

func (s *SGS) fetch(ctx context.Context, u string) ([]RatePoint, error) {
	var rows []struct {
		Data  string `json:"data"`
		Valor string `json:"valor"`
	}
	if err := getJSON(ctx, s.HTTP, "bcb-sgs", u, nil, &rows); err != nil {
		return nil, err
	}
	out := make([]RatePoint, 0, len(rows))
	for i, r := range rows {
		d, err := time.Parse("02/01/2006", r.Data)
		if err != nil {
			return nil, fmt.Errorf("marketdata: bcb-sgs: row %d date %q: %w", i, r.Data, err)
		}
		v, err := parseDecimal(r.Valor)
		if err != nil {
			return nil, fmt.Errorf("marketdata: bcb-sgs: row %d value: %w", i, err)
		}
		out = append(out, RatePoint{Date: d.UTC(), Value: v})
	}
	return out, nil
}

// Series returns the points of series code between from and to inclusive.
// dataInicial is required by the API and windows over 10 years return HTTP
// 406, so longer ranges are split into consecutive chunks.
func (s *SGS) Series(ctx context.Context, code int, from, to time.Time) ([]RatePoint, error) {
	if to.Before(from) {
		return nil, fmt.Errorf("marketdata: bcb-sgs: to %s before from %s", to.Format("2006-01-02"), from.Format("2006-01-02"))
	}
	var out []RatePoint
	for start := truncateDate(from); !start.After(to); {
		end := start.AddDate(sgsYears, 0, -1)
		if end.After(to) {
			end = truncateDate(to)
		}
		u := fmt.Sprintf("%s/dados/serie/bcdata.sgs.%d/dados?formato=json&dataInicial=%s&dataFinal=%s",
			s.BaseURL, code, start.Format("02/01/2006"), end.Format("02/01/2006"))
		pts, err := s.fetch(ctx, u)
		if err != nil {
			return nil, err
		}
		out = append(out, pts...)
		start = end.AddDate(0, 0, 1)
	}
	return out, nil
}

// Latest returns the last n points of series code (/ultimos/N), for the daily
// job: the newest point is typically D-1.
func (s *SGS) Latest(ctx context.Context, code, n int) ([]RatePoint, error) {
	return s.fetch(ctx, fmt.Sprintf("%s/dados/serie/bcdata.sgs.%d/dados/ultimos/%d?formato=json", s.BaseURL, code, n))
}

// CDI returns the daily CDI series (SGS 12), percent per business day.
func (s *SGS) CDI(ctx context.Context, from, to time.Time) ([]RatePoint, error) {
	return s.Series(ctx, SGSSeriesCDI, from, to)
}

// SelicTarget returns the Selic target (SGS 432), percent per year, between
// from and to. The series is daily and runs ahead to the next Copom meeting,
// so a /ultimos read returns future dates; callers pass to = today.
func (s *SGS) SelicTarget(ctx context.Context, from, to time.Time) ([]RatePoint, error) {
	return s.Series(ctx, SGSSeriesSelicTarget, from, to)
}

// USDBRL returns the SGS series 1 (USD sell), the documented fallback to PTAX.
// Buy is left zero.
func (s *SGS) USDBRL(ctx context.Context, from, to time.Time) ([]FXRate, error) {
	pts, err := s.Series(ctx, SGSSeriesUSDSell, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]FXRate, len(pts))
	for i, p := range pts {
		out[i] = FXRate{Date: p.Date, Sell: p.Value}
	}
	return out, nil
}

var _ FXSource = (*SGS)(nil)
