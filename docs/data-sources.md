# Data sources (spike for issue #8)

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
- Cost: free. Confidence in structure: high (VERIFIED); update cadence: medium.

## 7. SEC EDGAR (US filings, fundamentals)

- Auth: none, but **a descriptive User-Agent is mandatory**. VERIFIED: a request to `data.sec.gov` without a User-Agent returned HTTP 200 with an HTML "Your Request Originates from an Undeclared Automated Tool" page instead of JSON, so the client must check content type, not just status. Use a UA of the form `stock-portfolio contact@example.com` (placeholder; the user should configure a real contact in settings).
- Rate limit (SEC fair access policy, DOCS, not tested): max 10 requests/second per IP; exceeding it results in temporary blocks. The daily job needs only a handful of calls.
- Ticker to CIK: `https://www.sec.gov/files/company_tickers.json` (VERIFIED, object keyed "0".., fields `cik_str`, `ticker`, `title`). Fixture `testdata/edgar/company_tickers_head.json` (5 entries).
- Filings list: `https://data.sec.gov/submissions/CIK0000320193.json` (VERIFIED; CIK zero-padded to 10 digits). `filings.recent` holds parallel arrays (`accessionNumber`, `filingDate`, `reportDate`, `form`, `primaryDocument`, `isXBRL`, ...) for about the latest 1,000 filings; older ones in `filings.files[]`. Document URL: `https://www.sec.gov/Archives/edgar/data/320193/<accession without dashes>/<primaryDocument>`. Fixture `testdata/edgar/submissions_AAPL_trimmed.json`.
- XBRL facts: `https://data.sec.gov/api/xbrl/companyfacts/CIK0000320193.json` (VERIFIED, 3.7 MB for Apple; `facts.us-gaap.<Concept>.units.<unit>[]` with `start,end,val,accn,fy,fp,form,filed,frame`). Single concept: `https://data.sec.gov/api/xbrl/companyconcept/CIK0000320193/us-gaap/EarningsPerShareDiluted.json` (DOCS). Dividends per share (`CommonStockDividendsPerShareDeclared`) exist in facts for Apple. Concept names vary per issuer, so a fundamentals module needs a concept mapping with fallbacks. Fixture `testdata/edgar/companyfacts_AAPL_trimmed.json` (3 concepts, 4 points each).
- Cost: free.

## 8. Tax facts to re-verify (spec §11)

The spec says all rates are settings. Findings below; I am not a tax adviser; have a contador confirm before release.

| Rule (spec ref) | Current finding | Source | Confidence |
|---|---|---|---|
| Swing trade stocks, monthly sales up to R$ 20,000 exempt (§11.2) | Confirmed. Lei 11.033/2004 art. 3 I: individual's net gain on spot stock sales is exempt when monthly sales are at or below R$ 20,000. Applies to stock sales on exchange in the spot market; not to FIIs (20%, no exemption; secondary sources). | https://www.planalto.gov.br/ccivil_03/_ato2004-2006/2004/lei/l11033.htm (read via fetch, paraphrased) | High. The page flags amendments from MP 1.303/2025 as "Vigência encerrada" (expired), meaning that MP's changes did not take effect |
| 15% swing, 20% day trade (§11.2) | Confirmed. Lei 11.033 art. 2: 20% day trade, 15% other cases. IRRF "dedo-duro" 0.005% on swing (and 1% on day trade per secondary sources). | same Planalto page | High for rates; IRRF 1% day-trade rate from secondary sources only |
| DARF code 6015, due last business day of the following month (§11.2) | Secondary sources agree on 6015 for variable income net gains, minimum R$ 10, payment by the last business day of the next month. One site lists 6047 for day trade; conflicting. | secondary: blogs listed in search; no Receita page could be fetched (gov.br URL returned 404) | Medium. Verify in Sicalc |
| Loss carry-forward without expiry, same nature only (swing vs day trade) (§11.2) | Secondary sources only. Spec says "losses carried forward" without distinguishing. | secondary | Medium. Model swing and day-trade loss pools separately |
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
| B3 daily history | brapi `/api/v2/stocks/historical` | Own stored closes; Yahoo `.SA` | Free: 3 months; Startup R$ 119.99/mo: 1 yr; Pro R$ 139.99/mo: 10+ yr | High (prices adjusted, see 1) |
| B3 dividends/JCP | brapi `/api/v2/stocks/dividends` | CVM IPE (manual), manual entry | Free "limited" (undefined); Startup 1 yr; Pro 10+ yr | Medium |
| B3 splits/bonificações | same endpoint (`stockDividends`) | Manual entry via confirmation queue | same | Medium (BONIFICACAO label unverified) |
| B3 fundamentals | brapi `/api/v2/stocks/*` statements, or CVM ITR/DFP | CVM Dados Abertos | Startup annual; Pro quarterly; CVM free | High for CVM, medium for brapi |
| US quotes/history/splits/dividends | Yahoo chart API `events=div,splits` | Tiingo or Polygon (free key) | Free | **Low** (blocked here, unofficial) |
| USDBRL | BCB Olinda PTAX | SGS series 1 | Free | High |
| CDI | BCB SGS 12 (daily %, string) | brapi macro (Startup+) | Free | High |
| Tesouro Selic price | Tesouro Transparente CSV | brapi treasury (Pro), Selic accrual estimate | Free | High (1-day lag) |
| IBOV | B3 yearly close matrix | brapi `^BVSP` (token), Yahoo `^BVSP` | Free | Medium (undocumented endpoint) |
| S&P 500 | FRED SP500 CSV | Yahoo `^GSPC` | Free | Medium (price index, licence note) |
| BR filings | CVM Dados Abertos (IPE, ITR, DFP) | RAD web UI | Free | High |
| US filings, XBRL | SEC EDGAR submissions and companyfacts | none needed | Free (descriptive User-Agent) | High |
| Tax rules | Planalto (Lei 11.033, 14.754, 15.270, 9.249/LC 224) | contador | n/a | High for rates, low for details flagged above |

## 10. Not verified

- Yahoo Finance live behaviour (429 from this IP); cookie/crumb flow; exact rate limits.
- brapi plan gating on a real token (sandbox ignores it); what Free "Limited" dividends means; whether `^BVSP` works on Free; which three Tesouro bonds are open without token.
- brapi BONIFICACAO label and shape; `exDate` for stock events (null for all observed).
- CDI availability on the same day by 19:00 BRT; PTAX timing relative to the job (should be fine).
- Tiingo and Polygon free-tier limits.
- DARF 6047 vs 6015 for day trade; Cosit SC 130/2026 text; JCP 20% from 2028; FX convention for foreign cost basis; US withholding creditability; whether Receita's own pages (gov.br) state the R$ 20k rule (URL returned 404 when fetched).
