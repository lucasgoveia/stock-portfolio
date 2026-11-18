# Data sources (spikes for issues #8 and #37)

Research done on 2026-11-18 with live calls from a residential Brazilian IP (no paid signups, no tokens). Spec reference: `docs/spec.md` §11, §14, §15, §16, §17. Fixtures live in `internal/marketdata/testdata/<source>/`.

Legend: **VERIFIED** = observed in a live call today. **DOCS** = taken from the provider's own documentation, not exercised. **UNVERIFIED** = could not be confirmed.

## 0. Headline findings

1. **brapi.dev needs a token for everything except four sandbox tickers** (PETR4, VALE3, ITUB4, MGLU3). Any other ticker, and every index (`^BVSP`), returns 401 `MISSING_TOKEN`. The sandbox is rate limited (20 req/min, concurrency 1). A free token (R$ 0) exists but is capped at 1 asset per request, 15,000 req/month, 3 months of history and 30-minute data freshness. Dividends are "Limited" on Free (not quantified in the docs).
2. **Corporate actions in brapi come from the dividends endpoint**, not a separate `stockDividends` endpoint: `GET /api/v2/stocks/dividends` returns `cashDividends`, `stockDividends` (desdobramento/grupamento/bonificação) and `subscriptions`. The legacy `GET /api/quote/{t}?dividends=true` returns the same under `dividendsData`. Spec §14 wording ("brapi `stockDividends`") is correct but should name the endpoint.
3. **Yahoo Finance returned HTTP 429 for every request from this machine** (chart, quote, with and without cookie, any User-Agent, both query1 and query2 hosts). It could not be verified live. Treat it as unreliable and plan for a fallback from day one (see 2).
4. **Tesouro Selic price lags one business day.** The Tesouro Transparente CSV is refreshed each morning (~07:20 BRT) with the previous business day's base date. At the 19:00 job the latest PU is from D-1. Spec §16.1 step 1 must tolerate "latest available date".
5. **CDI (SGS 12) is also published with a lag**: at 12:00 BRT on 2026-11-18 the latest point was 2026-11-17. The 19:00 job should ask for the latest available point, not "today".
6. **BCB SGS API caps daily series queries to a 10-year window** and requires `dataInicial` when not using `/ultimos/N`; wider ranges return HTTP 406. Chunk bootstrap loads.
7. **The old Tesouro Direto JSON endpoint (`tesourodireto.com.br/json/.../treasurybondsinfo.json`) now returns 410 Gone.** Do not use it.
8. **IBOV and S&P 500 have no free official-and-documented daily API.** Verified workable sources: B3's own yearly IBOV close table (undocumented but official site) and FRED CSV for S&P 500 (price index, no dividends, no key). Yahoo (`^BVSP`, `^GSPC`) unverified. brapi `^BVSP` needs a token (plan level unverified).
9. **Tax facts changed since the spec was written** (details in section 8): JCP withholding is now **17.5%** (LC 224/2025); dividends paid to residents are subject to **10% withholding above R$ 50,000/month per payer** (Lei 15.270/2025); the swing-trade R$ 20,000 exemption and 15%/20% rates remain.

## 1. brapi.dev (B3 prices, dividends, corporate actions, fundamentals)

### Auth and limits
- Base: `https://brapi.dev`. Token header: `Authorization: Bearer <token>` (docs: https://brapi.dev/docs/authentication.mdx; also accepted as `?token=` per the Excel example, DOCS).
- Without token: only PETR4, VALE3, ITUB4, MGLU3 work. **VERIFIED**: all four return 200 on `/api/quote/{t}`; WEGE3 and `^BVSP` return 401 `{"error":true,"message":"Token de autenticação não fornecido","code":"MISSING_TOKEN"}`.
- Sandbox limits **VERIFIED**: response headers `ratelimit-limit: 20`, `x-brapi-concurrency-limit: 1`; exceeding gives `{"code":"RATE_LIMITED","message":"Limite do sandbox excedido..."}`. Sandbox tickers returned full data in testing (5 years of history, full dividends, balance sheet), so **the sandbox does not reflect plan gating**; paywalls below are DOCS only.
- Status codes (DOCS): 401 token missing/invalid; 403 plan does not include the resource (body names the plan); 429 quota or concurrency exhausted (`Retry-After`); 422 bad params. Rate-limit headers on every response: `RateLimit-Limit/Remaining/Reset`, `Retry-After`, `X-Plan-Data-Freshness`.
- v1 routes (`/api/quote/...`) send a deprecation `Link` header (VERIFIED); new integrations should use `/api/v2/...` with `symbols=` (llms.txt).

### Plans (source: https://brapi.dev/pricing.md, "current as of 2026-08-21")
| Plan | Price | Requests/month | Assets/request | History | Freshness |
|---|---|---|---|---|---|
| Free | R$ 0 | 15,000 | 1 | up to 3 months | 30 min |
| Startup | R$ 119.99/month or R$ 1,199.90/year | 150,000 | 10 | up to 1 year | 15 min |
| Pro | R$ 139.99/month or R$ 1,399.90/year | 500,000 | 20 | 10+ years | 5 min |

What each plan includes (DOCS, pricing.md feature table plus per-endpoint OpenAPI text from https://brapi.dev/openapi.json):
- Quotes (B3 stocks, BDRs, ETFs, FIIs): all plans, with a token (sandbox tickers without).
- History: range/interval and window size limited by plan (Free 3 mo, Startup 1 yr, Pro 10+ yr). Over the limit returns 400. `includeRaw=true` (unadjusted OHLC) is Pro only.
- Dividends (`/api/v2/stocks/dividends`): Free "Limited" (undefined), Startup up to 1 year, Pro 10+ years. `includeRaw=true` is Pro only.
- Fundamentals: Free none; Startup annual statements, recent years; Pro quarterly and annual since 2009.
- Macro (`/api/v2/macro`, includes Selic and CDI daily): Startup and Pro. Currency (`/api/v2/currency`, PTAX based): Startup and Pro. Currency history: Pro.
- Tesouro Direto (`/api/v2/treasury/*`): Pro. Without token only three bonds are open (not identified in the docs fetched).
- Insider transactions (CVM): Pro. Earnings calendar: Pro. Indexes composition (`/api/v2/indexes/*`): Pro.
- Index quotes/history (`^BVSP`): the quote endpoint documents index support; plan level for indices is **UNVERIFIED** (no token available).

Implication: for a personal tracker the **Free token probably covers prices** (e.g. 40 tickers x 1 req/day ~ 1,200/month, well under 15,000) but not enough dividend history, fundamentals or deep history. **Dividends/corporate actions and deep history need Startup (R$ 119.99/month) or Pro**. Confirming what Free "Limited" dividends means requires creating a free account (not done).

### Endpoints and samples
| Need | Endpoint | Fixture |
|---|---|---|
| Quote | `GET /api/v2/stocks/quote?symbols=PETR4,VALE3` (v1: `GET /api/quote/PETR4,VALE3`) | `testdata/brapi/v2_stocks_quote_PETR4.json` |
| History (relative) | `GET /api/quote/PETR4?range=1mo&interval=1d` or `GET /api/v2/stocks/historical?symbols=PETR4&range=3mo&interval=1d` | `testdata/brapi/quote_PETR4_range1mo.json` (trimmed to 5 points) |
| History (dates) | `GET /api/v2/stocks/historical?symbols=PETR4&startDate=2020-01-27&endDate=2020-02-03` | `testdata/brapi/v2_stocks_historical_PETR4_2020.json` |
| Dividends, JCP, splits, bonificações | `GET /api/v2/stocks/dividends?symbols=PETR4[&startDate&endDate]` (v1: `?dividends=true`) | `testdata/brapi/v2_stocks_dividends_PETR4.json` (cash array trimmed to 8 of 178) |
| Fundamentals | `GET /api/v2/stocks/{balance-sheet,income-statement,cash-flow,financial-data,statistics}?symbols=...&period=annual\|quarterly` (v1: `modules=`) | not saved |
| Errors | no token | `testdata/brapi/quote_nonsandbox_401.json`, `quote_BVSP_no_token_401.json` |

Shape notes (VERIFIED on PETR4):
- v1 quote: `{"results":[{symbol, regularMarketPrice, regularMarketTime (ISO UTC), historicalDataPrice:[{date (epoch seconds), open, high, low, close, volume, adjustedClose}]}], requestedAt, took}`. v2 wraps each result as `{requestedSymbol, symbol, changed, data:{...}}`; `changed:true` means an old ticker was resolved to the current one.
- Dividends: `cashDividends[]` with `label` (`JCP`, `DIVIDENDO`, ...), `rate` (BRL per share, **gross**, expressed on the split-adjusted scale), `lastDatePrior` (data-com), `exDate` (nullable), `paymentDate` (nullable; may be only a deadline, see `paymentDateIsDeadline`), `approvedOn`, `isinCode`, `verified`. `stockDividends[]` with `label` (`DESDOBRAMENTO`, `GRUPAMENTO` observed; BONIFICACAO label UNVERIFIED), `factor` (2 = "2 para 1"; 0.01 = "1 para 100"), `completeFactor`, `lastDatePrior`, `exDate` (null in all observed rows), `approvedOn`.
- Prices are **adjusted**: on 2020-01-31 `close` was 28.45 but `adjustedClose` 7.5578; docs state daily OHLC may already be adjusted for splits/dividends and `raw*` fields (Pro) carry the original. **Consequence for spec §14**: the price-ratio cross-check around ex-date cannot use brapi history if it is retroactively split-adjusted; store our own raw daily closes at fetch time, or use `includeRaw` (Pro). Also `rate` is adjusted, so on a split the historical dividend per share changes retroactively.
- Ticker conversions (e.g. AXIA5/6 to AXIA3) are resolved by `changed`/`requestedSymbol`; also `GET /api/v2/tickers/renames`.

Fallback for B3: Yahoo `PETR4.SA` (same unofficial API as section 2, same fragility); for dividends/events CVM IPE (comunicados, avisos aos acionistas) is a slow, manual-grade source. No free, stable programmatic B3 dividend source was found. The pragmatic fallback is **manual entry in the confirmation queue** (spec §14 already has one).

## 2. Yahoo Finance (US quotes, history, splits, dividends)

Unofficial, undocumented, ToS-grey. **Not verified live: HTTP 429 "Too Many Requests" from this IP on all variants tried** (chart on query1 and query2, v7 quote, with/without cookie from `fc.yahoo.com`, browser-like and curl User-Agents, spaced 8 to 30 seconds apart). No Yahoo fixture is saved because none could be captured. The Go implementation must save one as soon as a call succeeds from a working network.

Known shape (DOCS-equivalent from community knowledge, UNVERIFIED today):
- `GET https://query1.finance.yahoo.com/v8/finance/chart/AAPL?range=5y&interval=1d&events=div%2Csplits` (also `period1`/`period2` as epoch seconds). Response `chart.result[0]` has `meta` (currency, regularMarketPrice, exchangeTimezoneName), `timestamp[]`, `indicators.quote[0]` (open/high/low/close/volume), `indicators.adjclose[0].adjclose[]`, and `events.dividends{ts:{amount,date}}`, `events.splits{ts:{date,numerator,denominator,splitRatio}}`.
- Indices: `^GSPC`, `^BVSP` as path symbol (URL-encode `^` as `%5E`). B3 stocks as `PETR4.SA`.
- Needs a browser-like `User-Agent`; `/v7/finance/quote` and `quoteSummary` additionally need a cookie plus `crumb` (cookie from `https://fc.yahoo.com/`, crumb from `/v1/test/getcrumb`; the cookie request returned 404 with a Set-Cookie `A3` header, the crumb call was 429 here). The v8 chart endpoint has historically worked without crumb.
- Rate limits: undocumented; aggressive per-IP throttling (we were throttled on the first request, so this IP may already be flagged). Cache aggressively, one request per ticker per day, retry with backoff, never parallelise.
- Fragility: has changed or broken several times; no SLA; data licence is personal use only.

Candidate fallbacks for US (none exercised; all need a free key, so not verified):
- **Alpha Vantage**: free key, 25 requests/day (VERIFIED text on https://www.alphavantage.co/support/). Enough for roughly 25 tickers per day only if each ticker needs one call; too tight for quotes plus splits plus dividends.
- **Tiingo** (EOD daily prices include `divCash` and `splitFactor`): free tier exists, limits UNVERIFIED (pricing page did not render without JavaScript). `api.tiingo.com` answered 403 "Please supply a token", confirming key auth.
- **Polygon.io**: free tier with 5 req/min and splits/dividends endpoints (`/v3/reference/splits`, `/v3/reference/dividends`), UNVERIFIED limits; `api.polygon.io` answered "API Key was not provided".
- **Stooq**: now serves a JavaScript bot challenge instead of CSV (VERIFIED), not usable.
Recommendation: implement behind an interface; primary Yahoo chart endpoint; fallback Tiingo or Polygon with a free key. Decision on which fallback should wait until one account is created (user action).

## 3. BCB: USDBRL PTAX and CDI

### PTAX (Olinda) VERIFIED
- Day: `https://olinda.bcb.gov.br/olinda/servico/PTAX/versao/v1/odata/CotacaoDolarDia(dataCotacao=@dataCotacao)?@dataCotacao='MM-DD-YYYY'&$format=json`
- Period: `https://olinda.bcb.gov.br/olinda/servico/PTAX/versao/v1/odata/CotacaoDolarPeriodo(dataInicial=@dataInicial,dataFinalCotacao=@dataFinalCotacao)?@dataInicial='11-09-2026'&@dataFinalCotacao='11-18-2026'&$top=100&$format=json` (note US-style `MM-DD-YYYY` dates).
- Response: `{"value":[{"cotacaoCompra":5.7845,"cotacaoVenda":5.7851,"dataHoraCotacao":"2026-11-17 13:07:26.01516"}]}`. One bulletin per business day, around 13:00 BRT (the closing PTAX is the last of the day's four windows). Use `cotacaoVenda` as the USDBRL rate per the usual convention (a choice for the implementation, record it in settings). No auth, no documented rate limit. Fixture: `testdata/bcb/olinda_ptax_usd_period.json`.
- Alternative: SGS series 1 (USD sell, `https://api.bcb.gov.br/dados/serie/bcdata.sgs.1/dados?formato=json&dataInicial=09/11/2026&dataFinal=18/11/2026`) returned values equal to the PTAX sell on the same dates (5.7324 on 16/11). Fixture: `testdata/bcb/sgs1_usd_venda.json`. Use as fallback.
- PTAX is not an intraday market rate. The job at 19:00 gets that day's PTAX. For trade-date BRL cost basis (spec §11.1) PTAX of the trade date is the natural choice, but Receita rules for foreign assets should be checked (section 8).

### CDI daily (SGS 12) VERIFIED
- `https://api.bcb.gov.br/dados/serie/bcdata.sgs.12/dados?formato=json&dataInicial=03/11/2026&dataFinal=18/11/2026` or last N: `.../dados/ultimos/5?formato=json`.
- Response: `[{"data":"17/11/2026","valor":"0.050788"}]`. **`valor` is a string, the daily rate in percent** (0.050788 % per business day, about 13.65 % per year; matches SGS 4389 = 13.65 annualised, 252 basis). Dates `DD/MM/YYYY`. Fixture: `testdata/bcb/sgs12_cdi_daily.json`; also `sgs432_selic_meta.json` (Selic target).
- Window rule **VERIFIED**: a request over more than 10 years, or without `dataInicial`, returns HTTP 406 with a message saying to use at most 10 years. `/ultimos/N` is fine.
- Freshness: latest point was D-1 at 12:00 BRT on 2026-11-18. Unverified whether D is available by 19:00; job must take the latest available point.
- No auth, no documented limit; be polite (cache; one call per day).
- Fallback: brapi `/api/v2/macro?symbols=cdi` (Startup+, DOCS).

## 4. Tesouro Selic price

- Source: Tesouro Transparente CKAN dataset "Taxas dos Títulos Ofertados pelo Tesouro Direto" (dataset id `df56aa42-484a-4a59-8184-7676580c81e3`). Update frequency per metadata: "Diariamente". Data from 2002.
- CSV (VERIFIED, 14 MB, ASCII, `;` separated, decimal comma): `https://www.tesourotransparente.gov.br/ckan/dataset/df56aa42-484a-4a59-8184-7676580c81e3/resource/796d2059-14e9-44e3-80c9-2d9e30b405c1/download/precotaxatesourodireto.csv`
- Columns: `Tipo Titulo;Data Vencimento;Data Base;Taxa Compra Manha;Taxa Venda Manha;PU Compra Manha;PU Venda Manha;PU Base Manha`. Tesouro Selic rows exist per maturity (e.g. `Tesouro Selic;01/03/2029;17/11/2026;0,05;0,06;20300,05;20285,04;20285,04`). Rows are sorted newest first; the file carries all history (~178,700 rows), so the daily job must download the whole file (or use `If-None-Match`/ETag, VERIFIED present) and filter.
- Freshness VERIFIED: `Last-Modified: Wed, 18 Nov 2026 10:23:16 GMT` (07:23 BRT) with max `Data Base` 2026-11-17. So at 19:00 BRT the newest PU is from the previous business day; the "PU" is the morning indicative price of that base date (column names say "Manha"). Which price to use for valuation (`PU Venda Manha` is the redemption price) is a design decision; `PU Base Manha` equals `PU Venda Manha` in the sample.
- Fixture: `testdata/tesouro/precotaxatesourodireto_sample.csv` (last 3 dates, Selic plus 2 IPCA+ rows).
- Fallbacks: brapi `/api/v2/treasury/indicators` (Pro only, DOCS). The old tesourodireto.com.br JSON is 410 Gone (VERIFIED). Alternative: derive from Selic accrual (`VNA` style) since Tesouro Selic PU follows the Selic with a tiny spread; a computed estimate for the same-day value. Not implemented or verified.
- If the user holds Tesouro Selic of one maturity, store the maturity as part of the instrument.

## 5. Indices (IBOV, S&P 500)

| Index | Primary | Fallback | Status |
|---|---|---|---|
| IBOV | B3 yearly close matrix (`https://sistemaswebb3-listados.b3.com.br/indexStatisticsProxy/IndexCall/GetDownloadPortfolioDay/<base64 {"index":"IBOV","language":"pt-br","year":"2026"}>`), returns base64 of a CSV with rows = day, columns = month, `;`-separated, decimal comma, in Latin-1 | brapi `^BVSP` (needs token; plan UNVERIFIED), Yahoo `^BVSP` (UNVERIFIED) | B3 VERIFIED, 2026-11-17 close 169,816.64 present, 2026-11-18 absent at midday. Undocumented endpoint on B3's own site; may change. Fixture `testdata/index/b3_ibov_2026_matrix.csv` |
| S&P 500 | FRED `https://fred.stlouisfed.org/graph/fredgraph.csv?id=SP500&cosd=2026-11-01` (no key, CSV `observation_date,SP500`, blanks on holidays) | Yahoo `^GSPC` (UNVERIFIED) | FRED VERIFIED. Price index only (no dividends), lags by a day (11-17 absent at midday). FRED documents a licence restriction on SP500 history (limited to about 10 years); check terms before relying on it. Fixture `testdata/index/fred_sp500.csv` |

Other notes: BCB SGS series 7 ("Bovespa - índice") exists but stops in 2019 (VERIFIED), unusable. Stooq now blocks with a JS challenge (VERIFIED). For a fair benchmark the S&P 500 in BRL multiplies by the PTAX USDBRL; spec §15 says benchmark vs IBOV and S&P in BRL, both price indices, whereas CDI is accrual; this is a total-return versus price-return mismatch worth a note in the UI. Total-return indices (IBOV total return, S&P 500 TR) were not found in any free source.

## 6. CVM Dados Abertos (BR filings)

- Base: `https://dados.cvm.gov.br/dados/CIA_ABERTA/`. No auth, no documented rate limit, plain HTTPS files. Update policy is per dataset page; the files observed were regenerated on 2026-11-15 (Sunday), so expect **weekly-ish updates**, not daily.
- Company register: `CAD/DADOS/cad_cia_aberta.csv` (1.5 MB, last modified 2026-11-18 04:16 GMT). Maps CNPJ and `CD_CVM`; mapping ticker to CNPJ is not provided by CVM (use brapi profile, B3, or a manual table).
- ITR (quarterly): `DOC/ITR/DADOS/itr_cia_aberta_2026.zip` (27 MB; 19 CSV files inside, about 760 MB uncompressed). Files: index `itr_cia_aberta_2026.csv` (one row per filing with `VERSAO`, `LINK_DOC`), statements `BPA/BPP/DRE/DFC_MD/DFC_MI/DMPL/DRA/DVA` each in `_con` and `_ind` variants, `composicao_capital` (share counts), `parecer`. Statement rows are long format: `CNPJ_CIA;DT_REFER;VERSAO;DENOM_CIA;CD_CVM;GRUPO_DFP;MOEDA;ESCALA_MOEDA;ORDEM_EXERC;DT_INI_EXERC;DT_FIM_EXERC;CD_CONTA;DS_CONTA;VL_CONTA;ST_CONTA_FIXA`, with `ESCALA_MOEDA` = MIL (values in thousands) and `ORDEM_EXERC` = ÚLTIMO/PENÚLTIMO. Restatements appear as higher `VERSAO`; take the highest per (CNPJ, DT_REFER).
- DFP (annual): `DOC/DFP/DADOS/dfp_cia_aberta_2025.zip` (12.9 MB), same layout.
- IPE (events and comunicados): `DOC/IPE/DADOS/ipe_cia_aberta_2026.zip` (1.9 MB; 17 MB CSV). Columns `CNPJ_Companhia;Nome_Companhia;Codigo_CVM;Data_Referencia;Categoria;Tipo;Especie;Assunto;Data_Entrega;Tipo_Apresentacao;Protocolo_Entrega;Versao;Link_Download`. `Categoria = "Fato Relevante"` (2,211 rows in 2026 so far), also "Comunicado ao Mercado", "Assembleia", "Aviso aos Acionistas" etc. `Link_Download` points to the RAD PDF/HTML. Files are Latin-1 encoded (accents show as mojibake if read as UTF-8).
- RAD (`https://www.rad.cvm.gov.br/`) is a web UI, not an API; use the IPE CSV instead.
- Fixtures (Petrobras, CNPJ 33.000.167/0001-01, re-encoded as UTF-8): `testdata/cvm/ipe_fato_relevante_petrobras_sample.csv`, `itr_index_petrobras_sample.csv`, `itr_dre_con_petrobras_sample.csv`, `itr_composicao_capital_petrobras_sample.csv`.
- Fundamentals (VERIFIED live 2026-11-18, `internal/marketdata/cvm.go`): the DFP 2025 zip (12.3 MB, 19 CSVs, about 286 MB uncompressed) is downloaded to a temp file and only `DRE`, `BPA`, `BPP`, `DFC_MI`/`DFC_MD` (consolidated `_con`, else `_ind`) and `composicao_capital` are opened, filtering rows by CNPJ while streaming (Latin-1 decoded on the fly). `DFP_2026` exists but holds no filings yet. Mapping, `ORDEM_EXERC` = ÚLTIMO, highest `VERSAO` per `DT_REFER`, values times `ESCALA_MOEDA`: revenue 3.01, EBIT 3.05, net income 3.11.01 (parent; falls back to 3.11), equity 2.03 minus 2.03.09 (non-controlling), cash 1.01.01 + 1.01.02, debt 2.01.04 + 2.02.01 (includes lease financing), operating cash flow 6.01, shares = QT_ACAO_TOTAL_CAP_INTEGR minus treasury. Approximations: capex is the sum of negative direct children of 6.02 whose description mentions imobilizado, intangível or propriedade (CVM has no capex code); D&A is the 6.01.01/6.01.02 adjustment line naming depreciação/amortização (absent in direct-method DFC, then EBITDA is not shown); banks and insurers use other account plans and are not handled. Petrobras 2025 matched the published figures (revenue 497.5 bn, EBIT 145.6 bn, parent net income 110.1 bn). Quarterly results (issue #34, VERIFIED live 2026-11-18 on the Petrobras ITR 2025 and 2026 zips, `internal/marketdata/cvm_itr.go`): the `DRE_con` file of an ITR holds, per `DT_REFER`, the `ÚLTIMO` columns of the current period and `PENÚLTIMO` of the prior year. A Q1 ITR has one column per year (`DT_INI_EXERC` = Jan 1, three months). A Q2 or Q3 ITR has two per year: the year to date (`DT_INI_EXERC` = Jan 1) and the 3-month quarter (`DT_INI_EXERC` = Apr 1 or Jul 1), so the quarter is the `ÚLTIMO` row whose duration is 80 to 100 days and the fiscal quarter is the number of months the earliest `DT_INI_EXERC` spans divided by 3. The same codes as the DFP apply (3.01 revenue, 3.11.01 parent net income, falling back to 3.11) and `VL_CONTA` times `ESCALA_MOEDA`; the highest `VERSAO` per `DT_REFER` wins (Petrobras Q1 2026 has a VERSAO 2). The CVM EPS lines (3.99.01.01 basic and 3.99.02.01 diluted, per share class) are not used: EPS is net income over `QT_ACAO_TOTAL_CAP_INTEGR` minus treasury from `composicao_capital` at the same `DT_REFER`, the same definition as the expected EPS. The filing date is `DT_RECEB` of the index file `itr_cia_aberta_YYYY.csv` for that (`DT_REFER`, `VERSAO`). There is no Q4 ITR: Q4 = DFP annual minus the Q3 year to date (Petrobras 2025: revenue 497.5 bn annual, 370.2 bn through Q3). Each ITR zip is 28 to 32 MB (the 2026 zip has no Q4 yet); a refresh reads the current and two previous years. Ticker to CNPJ is a per-asset field entered by the user.
- Fixtures: `testdata/cvm/dfp_2025_*_petrobras_latin1.csv` (Latin-1, with a synthetic VERSAO 2 restatement of revenue and a few rows of another company).
- Cost: free. Confidence in structure: high (VERIFIED); update cadence: medium.

## 7. SEC EDGAR (US filings, fundamentals)

- Auth: none, but **a descriptive User-Agent is mandatory**. VERIFIED: a request to `data.sec.gov` without a User-Agent returned HTTP 200 with an HTML "Your Request Originates from an Undeclared Automated Tool" page instead of JSON, so the client must check content type, not just status. Use a UA of the form `stock-portfolio contact@example.com` (placeholder; the user should configure a real contact in settings).
- Rate limit (SEC fair access policy, DOCS, not tested): max 10 requests/second per IP; exceeding it results in temporary blocks. The daily job needs only a handful of calls.
- Ticker to CIK: `https://www.sec.gov/files/company_tickers.json` (VERIFIED, object keyed "0".., fields `cik_str`, `ticker`, `title`). Fixture `testdata/edgar/company_tickers_head.json` (5 entries).
- Filings list: `https://data.sec.gov/submissions/CIK0000320193.json` (VERIFIED; CIK zero-padded to 10 digits). `filings.recent` holds parallel arrays (`accessionNumber`, `filingDate`, `reportDate`, `form`, `primaryDocument`, `isXBRL`, ...) for about the latest 1,000 filings; older ones in `filings.files[]`. Document URL: `https://www.sec.gov/Archives/edgar/data/320193/<accession without dashes>/<primaryDocument>`. Fixture `testdata/edgar/submissions_AAPL_trimmed.json`.
- XBRL facts: `https://data.sec.gov/api/xbrl/companyfacts/CIK0000320193.json` (VERIFIED, 3.7 MB for Apple; `facts.us-gaap.<Concept>.units.<unit>[]` with `start,end,val,accn,fy,fp,form,filed,frame`). Single concept: `https://data.sec.gov/api/xbrl/companyconcept/CIK0000320193/us-gaap/EarningsPerShareDiluted.json` (DOCS). Dividends per share (`CommonStockDividendsPerShareDeclared`) exist in facts for Apple. Concept names vary per issuer, so a fundamentals module needs a concept mapping with fallbacks. Fixture `testdata/edgar/companyfacts_AAPL_trimmed.json` (3 concepts, 4 points each).
- Fundamentals (VERIFIED live 2026-11-18, `internal/marketdata/edgar.go`): companyfacts for Apple answers `application/json` with a descriptive User-Agent (the placeholder `stock-portfolio admin@example.com` worked; configure `sec_user_agent`). Annual flows are 10-K facts with a 340 to 380 day duration, deduplicated per period end by latest `filed`; instants come from 10-K and 10-Q. Ordered fallbacks: revenue RevenueFromContractWithCustomerExcludingAssessedTax, Revenues, SalesRevenueNet, ...; EBIT OperatingIncomeLoss; net income NetIncomeLoss, ProfitLoss; EPS EarningsPerShareDiluted; equity StockholdersEquity; shares CommonStockSharesOutstanding, WeightedAverageNumberOfDilutedSharesOutstanding and dei EntityCommonStockSharesOutstanding (latest cover count); cash CashAndCashEquivalentsAtCarryingValue plus MarketableSecuritiesCurrent; debt LongTermDebtNoncurrent + LongTermDebtCurrent (or LongTermDebt) + CommercialPaper/ShortTermBorrowings (leases excluded); operating cash flow, capex PaymentsToAcquirePropertyPlantAndEquipment, D&A DepreciationDepletionAndAmortization, dividends per share CommonStockDividendsPerShareDeclared (annual; used for yield and Bazin). Quarterly (issue #34, VERIFIED live 2026-11-18 on Apple companyfacts, `internal/marketdata/edgar_quarters.go`): 10-Q flow facts carry `start`, `end`, `form`, `filed`, `fy`, `fp` and sometimes `frame`. `fp` is the fiscal period of the *filing* (Q1, Q2, Q3), so it also labels the prior-year comparatives inside that filing, which is what the fiscal quarter number is taken from. A Q1 10-Q has one fact (about 90 days); a Q2 or Q3 10-Q has two per concept: the 3-month quarter (80 to 100 days) and the year to date (6 or 9 months, 170 to 290 days). Q4 is never reported on its own: Q4 = 10-K FY minus the Q3 9-month year to date (Apple FY2026 revenue 478.8 bn minus 364.4 bn = 114.5 bn; EPS 8.98 minus 6.88 = 2.10). The `frame` field (`CY2026Q2`) marks calendar-aligned quarters but is absent on year-to-date facts and on most Q4 values, so it is not relied on. Ordered fallbacks as for the annual figures; EPS is `EarningsPerShareDiluted` as reported, shares are `WeightedAverageNumberOfDilutedSharesOutstanding`. Foreign filers (20-F, ifrs-full) are not supported. CIK is resolved from company_tickers.json (BRK.B tried as BRK-B) and cached on the asset. Fixture `testdata/edgar/companyfacts_AAPL_fundamentals.json`.
- Cost: free.

## 8. Tax facts to re-verify (spec §11)

The spec says all rates are settings. Findings below; I am not a tax adviser; have a contador confirm before release.

| Rule (spec ref) | Current finding | Source | Confidence |
|---|---|---|---|
| Swing trade stocks, monthly sales up to R$ 20,000 exempt (§11.2) | Confirmed. Lei 11.033/2004 art. 3 I: individual's net gain on spot stock sales is exempt when monthly sales are at or below R$ 20,000. Applies to stock sales on exchange in the spot market; not to FIIs (20%, no exemption; secondary sources). | https://www.planalto.gov.br/ccivil_03/_ato2004-2006/2004/lei/l11033.htm (read via fetch, paraphrased) | High. The page flags amendments from MP 1.303/2025 as "Vigência encerrada" (expired), meaning that MP's changes did not take effect |
| 15% swing, 20% day trade (§11.2) | Confirmed. Lei 11.033 art. 2: 20% day trade, 15% other cases. IRRF "dedo-duro" 0.005% on swing (and 1% on day trade per secondary sources). | same Planalto page | High for rates; IRRF 1% day-trade rate from secondary sources only |
| DARF code 6015, due last business day of the following month (§11.2) | Secondary sources agree on 6015 for variable income net gains, minimum R$ 10, payment by the last business day of the next month. One site lists 6047 for day trade; conflicting. | secondary: blogs listed in search; no Receita page could be fetched (gov.br URL returned 404) | Medium. Verify in Sicalc |
| Loss carry-forward without expiry, same nature only (swing vs day trade) (§11.2) | Secondary sources only. Spec says "losses carried forward" without distinguishing. | secondary | Medium. Model swing and day-trade loss pools separately |
| Losses from an exempt month (stock sales up to R$ 20,000) carry forward (§11.2) | Implemented as an assumption in `engine.TaxMonths`: a net stock loss in an exempt month joins the swing loss pool; exempt gains never offset anything. | secondary sources only | Low: **ask a contador** |
| Foreign assets 15% annual (Lei 14.754/2023) (§11.3) | Confirmed: art. 2 and 3 taxes income and gains from foreign financial applications at 15% in the annual adjustment, no deductions, realised-basis for gains, loss compensation within the foreign pool (art. 9), foreign tax credit only if a treaty or reciprocity exists (art. 4). Secondary: Cosit SC 130/2026 treats gains on US-listed stocks and ADRs as financial applications (15%); no R$ 20,000 style exemption applies to them. | https://www.planalto.gov.br/ccivil_03/_ato2023-2026/2023/lei/L14754.htm (fetched); Cosit SC 130 from a search summary, not opened | High for the law, medium for the Cosit item |
| US dividends (§11.3 "dividends with US withholding") | Same 15% annual rule applies to dividends from foreign financial applications. US withholding (typically 30% for Brazilian residents, no treaty) is creditable only under art. 4 conditions (reciprocity) and the credit cannot exceed the Brazilian tax increase. Not confirmed from a primary source for the reciprocity status. | Lei 14.754 art. 4 (fetched) | Low for creditability: **UNVERIFIED, ask a contador** |
| Cost basis of foreign assets in BRL (§11.1 "FX at each trade date") | Not verified. Which FX (PTAX buy or sell, which date) the Receita requires was not confirmed. | none | Unconfirmed |
| JCP withholding rate (§17 note) | **17.5%** from 2026-01-01 (was 15%), set in art. 9 par. 2 of Lei 9.249/1995 as amended by LC 224/2025. Secondary sources say 20% from 2028 (the Planalto text fetched does not show that, so UNVERIFIED). MP 1.303/2025 had proposed 20% but expired. | https://www.planalto.gov.br/ccivil_03/leis/l9249.htm (fetched) | High for 17.5% now; low for 2028 |
| Dividends to residents after the 2025 income tax reform (§17 note) | Lei 15.270/2025 art. 6-A (effective 2026-01-01): dividends paid by one legal entity to one resident individual above R$ 50,000 in a month suffer 10% IRRF on the **whole** amount (not only the excess); no deductions; recomputed on the month's combined payments from that payer. Excluded: dividends from results up to calendar 2025 and those approved by 2025-12-31. Dividends below the threshold remain exempt (secondary). Also a minimum-tax mechanism (IRPFM) for annual income above R$ 600k (rate rising to 10% at R$ 1.2M), secondary sources. Dividends abroad: 10% withholding on remittances to non-residents (n/a to this app). | https://www.planalto.gov.br/ccivil_03/_ato2023-2026/2025/lei/L15270.htm (fetched) | High for 10%/R$ 50k/month per payer. IRPFM details from secondary only |

Spec impact: §17 and §11 should record JCP at 17.5% and dividend withholding 10% above R$ 50k per payer-month as default settings (both with effective dates, because JCP changes in 2028 per secondary sources), and add a note that US dividends are taxed in the annual 15% bucket. A single-user investor unlikely hits R$ 50k from one payer per month, but the rule is cheap to model as a per-payer monthly sum.

## 9. Summary table

| Need | Source | Fallback | Cost | Confidence |
|---|---|---|---|---|
| B3 quotes | brapi `/api/v2/stocks/quote` (token) | Yahoo `.SA`, manual | Free token OK (15k req/month, 1 asset/req) | High |
| B3 daily history (unadjusted) | **B3 COTAHIST** yearly/daily zips (section 11) | brapi (3 months, adjusted) | Free | High (VERIFIED; daily file ~21:00 BRT; one UTF-8 record) |
| B3 dividends/JCP | brapi `/api/v2/stocks/dividends` | CVM IPE (manual), manual entry | Free "limited" (undefined); Startup 1 yr; Pro 10+ yr | Medium |
| B3 splits/bonificações | same endpoint (`stockDividends`) | Manual entry via confirmation queue | same | Medium (BONIFICACAO label unverified) |
| B3 fundamentals | brapi `/api/v2/stocks/*` statements, or CVM ITR/DFP | CVM Dados Abertos | Startup annual; Pro quarterly; CVM free | High for CVM, medium for brapi |
| US prices/splits/dividends | **Tiingo** free key (section 12) | Yahoo chart API, Polygon | Free (50/h, 1000/day, 500 symbols/mo, personal use) | Medium (DOCS only, no key) |
| USDBRL | BCB Olinda PTAX | SGS series 1 | Free | High |
| CDI | BCB SGS 12 (daily %, string) | brapi macro (Startup+) | Free | High |
| Tesouro Selic price | Tesouro Transparente CSV | brapi treasury (Pro), Selic accrual estimate | Free | High (1-day lag) |
| IBOV | B3 yearly close matrix (2024, 2025, 2026 tested; not in COTAHIST) | BOVA11 from COTAHIST, brapi `^BVSP`, Yahoo | Free | Medium (undocumented endpoint) |
| S&P 500 | FRED SP500 CSV | Yahoo `^GSPC` | Free | Medium (price index, licence note) |
| BR filings | CVM Dados Abertos (IPE, ITR, DFP) | RAD web UI | Free | High |
| US filings, XBRL | SEC EDGAR submissions and companyfacts | none needed | Free (descriptive User-Agent) | High |
| Tax rules | Planalto (Lei 11.033, 14.754, 15.270, 9.249/LC 224) | contador | n/a | High for rates, low for details flagged above |

## 11. B3 COTAHIST (issue #37)

Official B3 "Séries Históricas" files. Live calls on 2026-11-18 from a Brazilian IP, no auth, no cookies, plain `curl` with a browser User-Agent. Layout doc: https://www.b3.com.br/data/files/33/67/B9/50/D84057102C784E47AC094EA8/SeriesHistoricas_Layout.pdf (rev. 2020, 10 pages; read, **VERIFIED** 200). Fixture: `testdata/cotahist/COTAHIST_sample.TXT` (17 real records plus header and trailer, 4.4 KB; the trailer record count does not match because the file is trimmed).

### Download URLs (all VERIFIED, HTTP 200, `application/x-zip-compressed`)
Base `https://bvmf.bmfbovespa.com.br/InstDados/SerHist/`
| Granularity | File | Size observed | Last-Modified observed |
|---|---|---|---|
| Year | `COTAHIST_A2026.ZIP` (YYYY) | 95 MB zip, 833 MB txt, 3.5M records | 2026-11-18 00:08 GMT |
| Year | `COTAHIST_A2025.ZIP` | 89 MB zip, 748 MB txt, 3.17M records | 2025-12-31 |
| Month | `COTAHIST_M102026.ZIP` (MMYYYY) | 10 MB | 2026-10-30 |
| Day | `COTAHIST_D17112026.ZIP` (DDMMYYYY) | 606 KB zip, 3.9 MB txt, 16,440 records | 2026-11-18 00:02 GMT |
- Entry inside the zip is `COTAHIST_<same suffix>.TXT`. Parse the daily file for the incremental job; use yearly files only for backfill (two files for 2 years, ~185 MB download; stream, do not load into memory).
- Freshness: the file for D is published about **21:00 BRT on D** (Last-Modified 00:02 GMT on D+1). `COTAHIST_D18112026.ZIP` returned 404 at 13:00 BRT the same day. So the 19:00 job (spec §16) may not find today's file: ask for the latest available daily file (try D, then walk back), or schedule the COTAHIST step after 21:30.
- The yearly file of the current year is regenerated nightly and contains every day to date; the 2025 file ends at 2025-12-30 (last trading day).
- Terms: B3 publishes the files for free on its site; redistribution terms were not checked (personal use only here). UNVERIFIED.

### Format
Text, **245 characters per record + CRLF**, fixed width, positions 1-based inclusive (layout PDF, all offsets below checked against real rows).
- Record `00` header (`COTAHIST.YYYYBOVESPA YYYYMMDD` generation date) and `99` trailer (record count). `01` = one row per security per day. Skip `00`/`99`.
- Prices are `N(11)V99`: 13 digits, last two are cents (`0000000004398` = 43.98). Volume `N(16)V99`, 18 digits. No decimal separator.

| Field | Pos | Notes (PETR4, 2026-11-17: `020 ... 02PETR4`) |
|---|---|---|
| TIPREG | 1-2 | `01` |
| DATA | 3-10 | `20261117` |
| CODBDI | 11-12 | `02` |
| CODNEG | 13-24 | ticker, right-padded: `PETR4`, fractional `PETR4F` |
| TPMERC | 25-27 | `010` |
| NOMRES | 28-39 | `PETROBRAS` |
| ESPECI | 40-49 | `PN      N2` |
| PREABE / PREMAX / PREMIN / PREMED | 57-69 / 70-82 / 83-95 / 96-108 | 44.30 / 44.75 / 43.90 / 44.27 |
| PREULT (close) | 109-121 | 43.98 |
| PREOFC / PREOFV | 122-134 / 135-147 | best bid / ask |
| TOTNEG | 148-152 | number of trades, 35371 |
| QUATOT | 153-170 | quantity (shares), integer |
| VOLTOT | 171-188 | financial volume, `V99` |
| PREEXE / INDOPC / DATVEN | 189-201 / 202 / 203-210 | options strike, indicator, expiry (`99991231` if none) |
| FATCOT | 211-217 | quotation factor, `0000001` for every equity/BDR/FII observed |
| PTOEXE | 218-230 | |
| CODISI | 231-242 | ISIN `BRPETRACNPR6` |
| DISMES | 243-245 | distribution number |

### Encoding gotcha (VERIFIED)
The 2025 file is pure ASCII (all records 245 bytes). The **2026 file contains one record in UTF-8** (`FROTA OCEÂNICA`, 246 bytes, `012026102252FR0T5L`), so byte offsets shift for that line. The layout says nothing about encoding; older years were Latin-1 historically (not checked). Parser rule: decode each line as UTF-8, fall back to Latin-1, then slice by characters (never by bytes). Accents only occur in NOMRES/ESPECI, so a tolerant parser could also ignore bad lines for non-held tickers. Fixture keeps this line.

### Filtering
Layout tables (PDF): TPMERC `010` vista, `012`/`013` option exercise, `017` leilão, `020` fracionário, `030` termo, `050`/`060` futuro, `070` call, `080` put. CODBDI `02` lote padrão, `96` mercado fracionário, `12` FII, `78`/`82` options, `74`/`75` index options, `38`/`42` option exercise, `62` termo, `10` direitos e recibos, `08` recuperação judicial. **BDR/ETF codes are not in the 2020 layout but observed in 2025-26 data**: BDI `34` (ESPECI `DRN`, e.g. MACY34), `36` (`DRE`), `35` (`DR1`), `14` (`CI`: ETFs such as BOVA11 and some funds), `13` (`CI`, FIDC/FIAGRO-like), `22` (bônus).
- Lot-market stocks, units and BDRs: `TPMERC = 010` and CODBDI in {`02`, `34`, `35`, `36`, `14` for ETFs, `12` for FIIs}. Simplest robust rule: key on the ticker the user holds (exact `CODNEG` match with `TPMERC=010`), not on BDI whitelists; use BDI only to classify unknown tickers.
- Fractional: `TPMERC = 020` and `CODBDI = 96`, ticker = lot ticker + `F` (`PETR4F`). Prices differ slightly from the lot ticker and volume is small. For beta/split checks use the lot ticker only.
- Exclude options (`070`/`080`, BDI `78`/`82`), exercise, termo (`030`), auctions (`017`, BDI `5x`), and stray rows.
- Same ticker can appear with several `010` rows? Not observed for a given day and BDI; still key by (date, CODNEG, TPMERC=010) and keep the first.

### Unadjusted and split detection (VERIFIED)
Prices are raw trade prices. Scanning 2025-01 to 2026-11 lot tickers for day-to-day close ratios outside 0.55-1.8 found clear events with `FATCOT` still `0000001`: ENJU3 0.80 to 8.24 on 2026-10-09 (consistent with a 1:10 grupamento), GFSA3 1.24 to 22.48 on 2025-06-16, DIRR3 40.99 to 13.63 on 2025-08-11 (ratio 0.333, consistent with a 3:1 split/bonificação; fixture rows). FATCOT does not carry split information. Noise to filter: penny stocks (PDGR3, IFCM3 at R$0.01-0.05), tickers in recuperação judicial and tick-size jumps; require price above some floor and a matching brapi/CVM event or send to the confirmation queue. Example of an unexplained jump: MGLU3 12.69 to 5.30 on 2026-10-26 (ratio 0.418, not a clean split ratio) while the brapi sandbox dividends endpoint listed no stock event since 2026-09-01 (so either a real price move or a missing/late event; exactly the spec §14 alert case; not investigated further).
Ticker changes (e.g. `AXIA3`) appear as new `CODNEG` with no link to the old one; use ISIN (`CODISI`) to join history across renames.

### IBOV in COTAHIST
**No index series.** The file has only securities. Observed IBOV-related rows are the ETF `IBOV11` (BDI `02`, `IBO/`, an index-linked security, not the index) and options on index (BDI `74`/`75`, e.g. `IBOVK187`). Use the B3 index endpoint already found in #8 (section 5): year matrix for `IBOV`, re-tested for `year` 2024 and 2025 (both HTTP 200), so a 2+ year history is a handful of calls. Fixture: `testdata/index/ibov_2025_b3_matrix.csv` (full 2025 matrix, 3.3 KB; Latin-1/ASCII, rows = day, columns = month, `;`, decimal comma, thousands dot). The matrix gives the close only: fine for weekly returns. Undocumented endpoint; keep a manual CSV import as fallback. The ETF BOVA11 (COTAHIST BDI 14) is a usable proxy if the endpoint disappears, with the caveat of ETF dividends (unadjusted close drops on ex-date).

### Use for beta
Weekly (Friday close, or last trading day of the week) returns of the ticker from COTAHIST raw closes versus IBOV from the matrix. Raw closes must be split-adjusted before returns: apply confirmed events (spec §14) or drop weeks that straddle an unexplained jump. Dividends are not in the file, so raw returns understate total return by the yield (immaterial for beta).

## 12. Tiingo (US EOD, splits, dividends) (issue #37)

**No API key was created**, so nothing below was called with a token. `https://api.tiingo.com/tiingo/daily/aapl/prices` answered HTTP 403 `{"detail":"Please supply a token"}` (VERIFIED, confirms key auth). Everything else is from Tiingo's own pages fetched today (pricing page and https://www.tiingo.com/documentation/end-of-day), marked DOCS. No Tiingo fixture could be saved.

- Free plan "Starter" (individual, $0), DOCS: **50 requests/hour, 1,000 requests/day, 500 unique symbols/month, 1 GB bandwidth/month**; price history listed as 30+ years; license "personal use only, may not display or share data with another organization" (pricing page). Search summaries agree on the limits and note that fundamentals and News are not included. Paid: Power $30/month (UNVERIFIED, search results only).
- Endpoints (DOCS): `GET https://api.tiingo.com/tiingo/daily/<ticker>/prices` (latest EOD), same with `?startDate=YYYY-M-D&endDate=...&resampleFreq=daily|weekly|monthly&format=json|csv` for history; meta `GET /tiingo/daily/<ticker>` returns `ticker,name,exchangeCode,description,startDate,endDate`.
- Fields (DOCS): `date, open, high, low, close, volume` (raw), `adjOpen, adjHigh, adjLow, adjClose, adjVolume`, `divCash` (cash dividend on its ex-date), `splitFactor` (1.0 normally; 2.0 for a 2:1 split). Raw vs adjusted are both present, which fits the spec §14 ratio check.
- Auth (DOCS, UNVERIFIED here): `Authorization: Token <key>` header or `?token=`; the docs page text did not show the header format, it is Tiingo's documented convention.
- Not verified: timestamps/freshness (EOD usually posted evening US time), the exact ISO format of `date`, whether `^GSPC`-style indices exist (use FRED for S&P, see 5), ToS beyond the personal-use line, and any quota-exceeded response shape (expect 429).
- Fit: daily job needs 1 request per US ticker per day (batch not needed), well under 50/hour and 1,000/day for a personal portfolio; symbols/month of 500 is no problem. History backfill per ticker is one request. Corporate events come from the same call by asking for a date range and reading `divCash`/`splitFactor` != 0/1.
- Account needed: free signup by the user. Until then Yahoo (section 2) remains unverified.

## 13. CVM IPE as fallback (issue #37)
Already described in section 6 (IPE CSV, `Categoria`: Fato Relevante, Comunicado ao Mercado, Aviso aos Acionistas). Not re-tested. It carries announcements as text/links, not structured split ratios or dividend amounts, so it is a manual-queue trigger only.

## 14. Recommendation after #37
- **B3 prices**: brapi Free for the quote (30 min delayed); **COTAHIST daily file as the official EOD close** (unadjusted, same-day after ~21:00 BRT, D-1 at the 19:00 job) and as the history/backfill source. It removes the need to "store each raw close" from brapi for history and gives raw ratios for split checks from 2 years back on day one.
- **IBOV**: B3 index year matrix (close only); BOVA11 from COTAHIST as proxy fallback.
- **B3 dividends/events**: still brapi Free "limited" plus manual queue, CVM IPE as trigger; nothing new found. COTAHIST detects splits (raw jump) but not dividends.
- **US**: Tiingo free key (50/h, 1000/day, 500 symbols/month, personal use), pending live verification once the user creates a key; Yahoo optional.

## 15. Not verified

- Yahoo Finance live behaviour (429 from this IP); cookie/crumb flow; exact rate limits.
- brapi plan gating on a real token (sandbox ignores it); what Free "Limited" dividends means; whether `^BVSP` works on Free; which three Tesouro bonds are open without token.
- brapi BONIFICACAO label and shape; `exDate` for stock events (null for all observed).
- CDI availability on the same day by 19:00 BRT; PTAX timing relative to the job (should be fine).
- Polygon free-tier limits. Tiingo: all of section 12 is DOCS only (no key created, no JSON captured, no fixture); `Authorization: Token` header format, date format, ToS beyond personal use.
- COTAHIST: redistribution terms; Latin-1 encoding of older years; behaviour on holidays/half days; whether the UTF-8 record is a one-off; BDI list for BDRs/ETFs beyond observed codes (layout PDF dates from 2020); how the 2025 yearly file looks after the final trading day.
- DIRR3/GFSA3/ENJU3/MGLU3 jumps were not matched to official corporate-action notices.
- DARF 6047 vs 6015 for day trade; Cosit SC 130/2026 text; JCP 20% from 2028; FX convention for foreign cost basis; US withholding creditability; whether Receita's own pages (gov.br) state the R$ 20k rule (URL returned 404 when fetched).
