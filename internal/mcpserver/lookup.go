package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/assets"
	"github.com/lucasgoveia/stock-portfolio/internal/marketdata"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

const (
	tsLayout = "2006-01-02T15:04:05Z"
	// cacheTTL is how long unknown-ticker lookups and the CVM register are kept.
	cacheTTL = 24 * time.Hour
)

// cached returns the value stored under key if it is younger than ttl, else
// fetches it and stores it.
func cached[T any](ctx context.Context, s *Server, key string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	var out T
	row, err := s.q.GetLookupCache(ctx, key)
	switch {
	case err == nil:
		if at, perr := time.Parse(tsLayout, row.FetchedAt); perr == nil && s.now().Sub(at) < ttl {
			if json.Unmarshal([]byte(row.Payload), &out) == nil {
				return out, nil
			}
		}
	case !errors.Is(err, sql.ErrNoRows):
		return out, fmt.Errorf("lookup cache: %w", err)
	}
	out, err = fetch()
	if err != nil {
		return out, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return out, fmt.Errorf("lookup cache: %w", err)
	}
	if err := s.q.PutLookupCache(ctx, db.PutLookupCacheParams{Key: key, Payload: string(b), FetchedAt: s.now().UTC().Format(tsLayout)}); err != nil {
		return out, fmt.Errorf("lookup cache: %w", err)
	}
	return out, nil
}

// target is the company a data tool works on: an asset the app holds, or an
// unknown ticker that passed the identity checks and the daily cap.
type target struct {
	Known   bool
	Asset   db.Asset
	Ticker  string
	Market  string
	CNPJ    string
	CIK     string
	Name    string
	Entry   *marketdata.RegisterEntry // CVM register match (B3 unknown)
	Warning string
}

func (t target) assetID() int64 { return t.Asset.ID }

var b3Ticker = regexp.MustCompile(`^[A-Z]{4}[0-9]{1,2}$`)

func inferMarket(ticker string) string {
	if b3Ticker.MatchString(ticker) {
		return assets.MarketB3
	}
	return assets.MarketUS
}

// findAsset returns the held asset with ticker (and market, when given).
func (s *Server) findAsset(ctx context.Context, ticker, market string) (db.Asset, bool, error) {
	list, err := s.d.Assets.List(ctx, assets.Filter{Market: market})
	if err != nil {
		return db.Asset{}, false, err
	}
	var found []db.Asset
	for _, a := range list {
		if a.Ticker == ticker {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return db.Asset{}, false, nil
	case 1:
		return found[0], true, nil
	}
	return db.Asset{}, false, fmt.Errorf("%s exists in more than one market; pass market (B3 or US)", ticker)
}

// requireAsset resolves a ticker that must already be in the app.
func (s *Server) requireAsset(ctx context.Context, ticker, market string) (db.Asset, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return db.Asset{}, errors.New("ticker is required")
	}
	a, ok, err := s.findAsset(ctx, ticker, strings.ToUpper(strings.TrimSpace(market)))
	if err != nil {
		return db.Asset{}, err
	}
	if !ok {
		return db.Asset{}, fmt.Errorf("%s is not in the app. Use lookup_company, list_filings and get_financials with market (and cnpj for B3) to research a new ticker, then submit_candidate", ticker)
	}
	return a, nil
}

// admit counts a symbol that the app does not hold against today's cap
// (America/Sao_Paulo). A symbol already counted today is always admitted.
func (s *Server) admit(ctx context.Context, market, symbol string) error {
	day := s.now().In(s.loc).Format("2006-01-02")
	n, err := s.q.HasLookupSymbol(ctx, db.HasLookupSymbolParams{Day: day, Market: market, Symbol: symbol})
	if err != nil {
		return fmt.Errorf("lookup cap: %w", err)
	}
	if n > 0 {
		return nil
	}
	cfg, err := s.d.Settings.MCP(ctx)
	if err != nil {
		return err
	}
	used, err := s.q.CountLookupSymbolsOnDay(ctx, day)
	if err != nil {
		return fmt.Errorf("lookup cap: %w", err)
	}
	if int(used) >= cfg.DailyNewSymbols {
		return fmt.Errorf("daily limit reached: %d of %d new symbols (not held in the app) were already looked up today (%s). %s was not looked up. Raise the limit in Settings > MCP, or try again tomorrow; symbols already looked up today still work", used, cfg.DailyNewSymbols, day, symbol)
	}
	return s.q.AddLookupSymbol(ctx, db.AddLookupSymbolParams{Day: day, Market: market, Symbol: symbol})
}

// register returns the CVM register of listed companies, cached for a day.
func (s *Server) register(ctx context.Context) ([]marketdata.RegisterEntry, error) {
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	if s.d.BR == nil {
		return nil, errors.New("the CVM client is not configured")
	}
	return cached(ctx, s, "cvm_register", cacheTTL, func() ([]marketdata.RegisterEntry, error) {
		return s.d.BR.Register(ctx)
	})
}

// VerifyCNPJ checks the check digits of a CNPJ and matches it against the CVM
// register of listed companies. It returns the register entry.
func (s *Server) verifyCNPJ(ctx context.Context, cnpj string) (marketdata.RegisterEntry, error) {
	if !marketdata.ValidCNPJ(cnpj) {
		return marketdata.RegisterEntry{}, fmt.Errorf("%q is not a valid CNPJ: the check digits do not match. Find the company's CNPJ on its investor relations page or on CVM and try again", cnpj)
	}
	formatted := marketdata.FormatCNPJ(cnpj)
	reg, err := s.register(ctx)
	if err != nil {
		return marketdata.RegisterEntry{}, fmt.Errorf("could not load the CVM register (cad_cia_aberta): %w", err)
	}
	for _, e := range reg {
		if e.CNPJ == formatted {
			return e, nil
		}
	}
	return marketdata.RegisterEntry{}, fmt.Errorf("CNPJ %s has valid check digits but is not in the CVM register of listed companies (cad_cia_aberta); it may be a fund, an unlisted company or a wrong number", formatted)
}

// resolve finds the company a data tool is about. A ticker the app holds is
// used as is. Otherwise it is an unknown ticker: B3 needs a CNPJ that passes
// the check digits and is found in the CVM register, US resolves its CIK, and
// the symbol counts against the daily cap. Unknown tickers never create assets.
func (s *Server) resolve(ctx context.Context, ticker, market, cnpj, cik string) (target, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	market = strings.ToUpper(strings.TrimSpace(market))
	if ticker == "" {
		return target{}, errors.New("ticker is required")
	}
	if market != "" && market != assets.MarketB3 && market != assets.MarketUS {
		return target{}, errors.New("market must be B3 or US")
	}
	a, ok, err := s.findAsset(ctx, ticker, market)
	if err != nil {
		return target{}, err
	}
	if ok {
		t := target{Known: true, Asset: a, Ticker: a.Ticker, Market: a.Market, CNPJ: a.Cnpj, CIK: a.Cik, Name: a.Name}
		if a.Market == assets.MarketB3 && t.CNPJ == "" && strings.TrimSpace(cnpj) != "" {
			if e, err := s.verifyCNPJ(ctx, cnpj); err == nil {
				t.CNPJ, t.Entry = e.CNPJ, &e
				t.Warning = "the asset has no CNPJ stored; using the one you passed (verified against CVM). Add it to the asset in the app to keep fundamentals working"
			}
		}
		return t, nil
	}
	if market == "" {
		market = inferMarket(ticker)
	}
	t := target{Ticker: ticker, Market: market}
	switch market {
	case assets.MarketB3:
		if strings.TrimSpace(cnpj) == "" {
			return target{}, fmt.Errorf("%s is not in the app, so its CNPJ is needed: find it on the web (the company's investor relations page or CVM) and pass it as cnpj. The app checks the digits and matches it against CVM's register of listed companies", ticker)
		}
		e, err := s.verifyCNPJ(ctx, cnpj)
		if err != nil {
			return target{}, err
		}
		if err := s.admit(ctx, market, ticker); err != nil {
			return target{}, err
		}
		t.CNPJ, t.Name, t.Entry = e.CNPJ, e.Name, &e
		if !strings.EqualFold(e.Situation, "ATIVO") {
			t.Warning = fmt.Sprintf("CVM register situation: %s %s", e.Situation, e.IssuerSituation)
		}
	default:
		if err := s.admit(ctx, market, ticker); err != nil {
			return target{}, err
		}
		if strings.TrimSpace(cik) != "" {
			if t.CIK = marketdata.FormatCIK(cik); t.CIK == "" {
				return target{}, errors.New("cik must be 1 to 10 digits")
			}
			return t, nil
		}
		if s.d.US == nil {
			return target{}, errors.New("the SEC client is not configured")
		}
		t.CIK, err = cached(ctx, s, "cik:"+ticker, cacheTTL, func() (string, error) {
			return s.d.US.CIKForTicker(ctx, ticker)
		})
		if err != nil {
			return target{}, fmt.Errorf("could not resolve a SEC CIK for %s (company_tickers.json): %w", ticker, err)
		}
	}
	return t, nil
}
