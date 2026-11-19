# Stock Portfolio — Specification v0.2

Status: approved design (grilling session 2026-11-18). v0.2 folds in the data-source spike findings (`docs/data-sources.md`, issue #8). Spec before code.

## 1. Purpose

A single-user decision-support system that enforces a small set of "boring" portfolio rules:

- Each stock holds 2–10% of the portfolio, sized by a 5-factor conviction score.
- Positions are trimmed slowly instead of sold out, except when the thesis breaks.
- 4–20% of the portfolio sits in liquid cash (Tesouro Selic, CDI, USD cash).
- New money (monthly contributions) is the main rebalancing tool.

The system **suggests**; the user **executes** trades manually at the broker and records them. No broker integration, no credentials stored.

### Non-goals

- Placing orders or connecting to broker APIs.
- Multi-user, auth, hosting (local only for v1; hosting may come later).
- Options, futures, crypto, FIIs (may be added later as asset kinds).
- Day trading support (day trades are flagged, not supported).

## 2. Scope of assets

| Kind | Market | Currency | Examples |
|---|---|---|---|
| Stock | B3 | BRL | WEGE3, ITUB4 |
| BDR | B3 | BRL | GOGL34 |
| Stock / ETF | US (direct account) | USD | GOOGL, MSFT |
| Tesouro Selic | Tesouro Direto | BRL | Tesouro Selic 2031 |
| CDI instrument | Bank | BRL | CDB 100% CDI, liquidity daily |
| USD cash | US broker | USD | idle USD balance |

Base currency: **BRL**. USD values converted with the daily PTAX/market USDBRL rate.

The user starts with **zero capital**; all money enters through contributions.

## 3. Architecture

- **Backend:** Go, single binary. `net/http` + chi, SQLite via `modernc.org/sqlite` (pure Go), sqlc for queries, Atlas versioned migrations (`YYYYMMDDHHmmSS_desc.sql`, generated from the declarative schema in `internal/store/schema` with `atlas migrate diff`, checksum-verified via `atlas.sum`, applied by the binary on startup). Monetary and quantity values use `shopspring/decimal`, stored as TEXT.
- **Frontend:** React + TypeScript (Vite), shadcn-ui, TanStack Router, TanStack Query. Built assets embedded into the Go binary with `embed`.
- **API:** JSON REST under `/api`. OpenAPI document maintained alongside handlers; TS client types generated from it.
- **Scheduler:** in-process daily job at 19:00 America/Sao_Paulo (after B3 close), plus a "run now" button.
- **Engine:** pure, deterministic Go package (`internal/engine`) holding all rule math (scores, targets, bands, cash target, contribution plan, trim plan, tax). No I/O. Table-driven tests are mandatory for every rule.
- **Config:** `config.toml` + settings table; secrets (Anthropic key, brapi token) via env vars or local config file excluded from git.

```
cmd/stockportfolio/       main, wires server + scheduler
internal/engine/          pure rule math
internal/store/           sqlc queries, migrations
internal/marketdata/      brapi, Yahoo, BCB, Tesouro clients
internal/jobs/            daily pipeline
internal/analyst/         AI analyst (Anthropic API)
internal/api/             HTTP handlers
web/                      React app
```

## 4. Domain model

### 4.1 Asset lifecycle

`watchlist → holding → exited` (an exited asset can return to watchlist). Each asset has a **sleeve**: `core` or `tactical`.

### 4.2 Main entities

- **Asset:** ticker, market, currency, kind, sleeve, status, benchmark index (IBOV or S&P 500).
- **Trade:** date, asset, side (buy/sell), quantity (decimal, fractional allowed), price, fees, currency, FX rate at trade date.
- **Position (derived):** quantity, average cost (Receita rules, §11), market value BRL, weight.
- **CashInstrument:** Tesouro Selic title, CDI instrument, USD cash. Balance tracked by transactions (deposit/withdraw/yield) and valued daily.
- **Rating:** 5 factor scores, effective date, reason. Append-only history; latest wins.
- **Thesis:** text, break conditions (list), next review date, status (`active`, `under_review`, `broken`).
- **FairValue:** versioned. Method (`scenario_dcf`, `bazin`, `graham`, `manual`, `target_price`), value per share, currency, date, inputs JSON, sources.
- **ResearchNote:** text, sources, optional catalyst flag (`opportunity`) with expiry date, author (`user` or `ai_approved`).
- **TacticalSetup:** catalyst note ref, entry price, target price, stop price, expiry date.
- **Contribution:** date, amount BRL, generated plan, executed status.
- **Dividend:** asset, type (dividend/JCP), ex-date, pay date, gross, withholding, net.
- **CorporateAction:** asset, type (split, reverse split, bonificação, subscription), ratio, cost basis per new share, ex-date, status (`detected`, `confirmed`, `rejected`, `applied`).
- **Alert:** type, asset (optional), payload, state (`open`, `snoozed`, `acknowledged`, `resolved`), dedupe key.
- **Snapshot:** daily portfolio value, per-position values, external flows (for TWR).
- **AnalystRun:** asset, model, inputs, output JSON, tokens, cost USD, status (`draft`, `approved`, `discarded`).
- **Settings:** factor weights, thresholds (all numbers in this spec are defaults, editable).

## 5. Scoring

### 5.1 Factors (1–5)

| Factor | Meaning | Default weight |
|---|---|---|
| Volatility | Low beta scores high | 1.0 |
| Understanding | How well the user understands the business | 1.0 |
| Moat | How hard it is to take market share away | 1.5 |
| Predictability | Recurring vs commodity/external-driven revenue | 1.5 |
| Horizon | How much it makes sense to hold long term | 1.0 |

Weighted score: `S = Σ wᵢ·rᵢ / Σ wᵢ`, range 1–5.

### 5.2 Volatility from beta

Beta computed daily from 2 years of weekly log returns (B3 history from B3 COTAHIST files, US history from the US price source; see §17) against IBOV (B3 assets) or S&P 500 (US assets), FX-neutral (US assets measured in USD vs S&P 500 in USD).

| Beta | Score |
|---|---|
| < 0.7 | 5 |
| 0.7–0.9 | 4 |
| 0.9–1.1 | 3 |
| 1.1–1.4 | 2 |
| > 1.4 | 1 |

User can override; the override is stored as a rating change with reason. Assets with less than 1 year of history require a manual rating.

### 5.3 Rating history

Every change appends a row with date and reason. Targets recompute on change.

## 6. Targets and bands

### 6.1 Budgets

- `cash_target` from §7.
- `tactical_weight` = sum of **actual** weights of tactical positions.
- `core_budget = 100% − cash_target − tactical_weight`.

### 6.2 Core targets

1. Raw weight: `rawᵢ = 2 + (Sᵢ − 1) × 2` (score 1 → 2%, score 5 → 10%).
2. Scale: `tᵢ = rawᵢ × core_budget / Σ raw`.
3. Clamp to [2%, 10%]. Freeze clamped positions; redistribute the remainder across unclamped positions proportionally to raw. Repeat until stable.
4. Infeasible cases:
   - `n × 10% < core_budget` (too few stocks): all positions at 10%, the remainder is reported as **undeployed**; an alert suggests adding watchlist names; the undeployed share is routed to cash in contribution planning.
   - `n × 2% > core_budget` (too many stocks): all at 2%; alert "too many positions".

Only core **holdings** (and watchlist names currently being bought, see §8.3) get targets.

### 6.3 Bands

- Lower band = `t × 0.75`; upper band = `min(t × 1.25, 12%)`.
- Hard floor 1.5%: below it, alert "top up or exit".
- Hard cap 12%: above it, trim alert regardless of target.

### 6.4 Bootstrap phase

While total portfolio value < **R$10,000** (configurable):

- 2–10% clamps, bands, hard floor/cap and cash band alerts are **off**.
- Core targets are pure score-proportional: `tᵢ = rawᵢ / Σ raw × core_budget`.
- Contribution planning still uses these targets, so concentration is limited by relative score.

Crossing the threshold switches to full rules automatically (alert notifies the switch).

## 7. Cash

- Cash = Tesouro Selic + CDI instruments + USD cash (converted). Dashboard shows BRL vs USD split.
- **Dynamic target:** `cash_target = 4% + 16% × overvalued_share`, where `overvalued_share` = (sum of core weights with price > fair value × 1.3) / (sum of core weights). Holdings without a fair value count as not overvalued and raise a "missing fair value" alert.
- Band: 4%–20%.
  - Actual cash < 4% → alert "raise cash" + trim plan (§9.3).
  - Actual cash > 20% → alert "deploy cash" + suggest running the contribution planner with cash as source.

## 8. Buying

### 8.1 Buy gate

A stock may receive a buy suggestion only if:

- Weighted score ≥ 2.5.
- Thesis written with at least one break condition.
- Fair value set (not older than 12 months).
- Price ≤ fair value.

Tactical entries have their own gate (§10.1).

### 8.2 Opportunity

An eligible stock (holding or watchlist, passing the buy gate) is an **opportunity** when either:

- Price ≤ fair value × 0.8, or
- It has an active research note flagged `opportunity` (non-expired).

### 8.3 Ramp-in

New core positions are built in thirds: tranche targets `t/3`, `2t/3`, `t`. Each contribution may advance a position by at most one tranche. Ramp state is stored per position.

### 8.4 Contribution planner

Input: contribution amount + undeployed cash from dividends and pending US bucket.

1. **Opportunities first**, sorted by discount to fair value (catalyst-flagged ones with no discount sort last among opportunities). Each may be funded up to its upper band, limited by its ramp tranche.
2. **Underweights next:** compute relative gap `(target − actual) / target` for each core stock and for cash. Fund the largest gap first, up to its target (or ramp tranche); repeat until money runs out.
3. **Minimum order:** skip any B3 suggestion below R$300 (configurable); leftover rolls to the next item.
4. **US batching:** money allocated to US assets goes into a "pending US" bucket held as cash until ≥ R$2,000 (configurable), then the planner suggests a remittance and a batched set of US buys.
5. Tactical positions never receive contributions.

Output: a plan (list of orders with quantity, estimated price, amount). The user executes at the broker, then records trades; the plan is marked executed (fully or partly).

### 8.5 Dividends and JCP

Recorded net of withholding (JCP 17.5% IRRF per LC 224/2025; dividends above R$50k/month from one payer have 10% IRRF per Lei 15.270/2025; rates are settings), credited to cash, and treated as new money in the next contribution plan. Yield on cost tracked per position. The daily job may detect announced dividends from data sources and propose entries for confirmation.

## 9. Trimming and selling

### 9.1 Principles

- Default action is a **slow trim**: each step sells **25% of the excess**, with a **30-day cooldown** per asset.
- A **broken thesis** allows a full exit at once (the user may choose staged instead).
- The system never sells by itself; every trigger produces an alert with a suggested order.

### 9.2 Triggers

| Trigger | Condition | Suggested step |
|---|---|---|
| Overweight | weight > upper band (or > 12%) | 25% of (actual − target) |
| Overvalued | price > fair value × 1.3 | 25% of (actual − lower band) |
| Thesis broken | thesis status = broken | full exit |
| Raise cash | cash < 4% | trims to reach cash target, see §9.3 |
| Better opportunity | watchlist candidate beats holding (§9.4) | 25% step, or full exit, to fund candidate's first tranche |

### 9.3 Raise-cash ordering

Take trims from, in order: overweight positions, overvalued positions, lowest-score positions. Respect cooldowns and tax pacing (§11.2).

### 9.4 Better opportunity

A watchlist candidate (passing the buy gate) beats a holding when **both**:

- candidate score ≥ holding score + 0.5, and
- candidate upside to fair value ≥ holding upside + 20 percentage points.

Alert pairs the two and proposes the swap.

## 10. Tactical positions

Short-horizon (months) research-driven bets, kept in the same portfolio, tagged `tactical`.

### 10.1 Entry

- Requires: catalyst research note, target price, stop price, expiry date.
- Buy gate as §8.1, but the horizon factor is excluded from the score; fair value = target price.
- Size: `size = clamp(1% / stop_distance, 1%, 2%)` of portfolio, where `stop_distance = (entry − stop) / entry`. Loss at stop ≤ 1% of portfolio.
- Count: 2–3 open tactical positions; the system warns before a 4th.

### 10.2 Life

- No contributions after entry; growth only by price.
- Normal bands do not apply. Only the 12% cap applies; above it, normal slow trim.
- Alerts: target price hit, stop hit, expiry passed without catalyst, thesis broken, better opportunity. The user decides every exit.
- **Promotion to core:** allowed if the thesis turns long-term and the asset passes core rating and buy gate; it then joins core target math.

## 11. Taxes (Brazil)

All rates and limits are settings; verify current law before release (see §17).

### 11.1 Cost basis

- Average cost per Receita rules: weighted average purchase cost including fees; sales do not change average cost.
- Splits / reverse splits: adjust quantity and average cost, total cost unchanged.
- Bonificação: new shares enter at the cost per share declared by the company.
- US assets: cost tracked in USD and in BRL (FX at each trade date).

### 11.2 B3 stocks (swing trade)

- Monthly sales of stocks ≤ R$20,000 → gains exempt.
- Above that, gains taxed 15%; DARF code 6015 due by last business day of the following month. Losses carried forward.
- Trim planning: when a suggested trim would push the month's sales above R$20,000, the system offers (a) split across months, (b) proceed with estimated tax shown.
- Core and tactical sales share the same monthly pool; warn when a tactical exit consumes room needed by planned core trims.
- Day trades (buy and sell same asset same day) are flagged; 20% rate shown. DARF code for day trade (6015 vs 6047) unconfirmed; it is a setting (`day_trade_darf_code`) flagged unconfirmed in the API and UI.

### 11.3 US assets

- Gains and dividends on foreign assets taxed annually (15%, Lei 14.754/2023), reported in the annual DIRPF. System produces a yearly report of realized gains in BRL and dividends with US withholding. Whether US withholding is creditable (art. 4, reciprocity) is unconfirmed; show it separately.
- FX convention for foreign cost basis must be confirmed before release.

### 11.4 Outputs

Monthly tax page (sales, exempt/not, gains, losses carried, DARF value) and annual report data for DIRPF (positions at 31/12, cost basis, dividends/JCP received).

## 12. Valuation and research

### 12.1 Fair value methods

- **Scenario DCF (primary):** bear/base/bull scenarios, each with a basis, 5-year revenue growth, a margin path (start and terminal), exit multiple, discount rate, cash conversion, tax rate and probability, plus a version-level list of new-business initiatives. Fair value = probability-weighted present value per share. Growth assumptions come from research and may exceed historical trends; historical multiples are shown only as a sanity check. The exact formulas (`internal/engine/dcf.go`, shared by the reverse DCF):

  *Basis (per scenario).* `ebit_ev`: the margin is an EBIT margin, the exit multiple is EV/EBIT, the discounted value is an enterprise value and net debt is subtracted. `net_pe`: the margin is a net margin, the multiple is P/E, the discounted value is already equity and net debt is not subtracted (it would count debt twice). A stored version without `basis` is read as `ebit_ev`.

  *Core.* With `r` the discount rate, `t = 1..5` the year, `g_t` the growth, `R_0` the latest annual revenue, `T` the terminal margin and `s` the start margin (default `T`):
  - `R_t = R_(t-1) x (1 + g_t)`
  - `m_t = s + (T - s) x t / 5` (linear, so `m_5 = T`)
  - interim cash `C_t = R_t x m_t x cc x a`, with `cc` the cash conversion (fraction of each year's earnings returned or accumulated for shareholders, default 0) and `a = 1 - tax_rate` for `ebit_ev` (default tax 34% BR, 21% US, set by the editor and the analyst) or `a = 1` for `net_pe`
  - terminal value `TV = R_5 x T x multiple`
  - core value `= TV / (1+r)^5 + sum_t C_t / (1+r)^t`; for `ebit_ev` subtract net debt; divide by shares. Discounting uses decimal `Pow`, never floats.

  *Initiatives (new business).* Each has a name, rationale, sources, `start_year` in 1..5, `revenue_year5` (> 0), a constant `margin` in [0, 1] on the scenario's basis (negative margins are not modelled: a loss-making ramp would need a year-by-year margin path) and a success probability in [0, 1] for each of bear, base and bull; at most 10. Its revenue if it succeeds ramps linearly: `I_t = 0` for `t < start_year`, else `revenue_year5 x (t - start_year + 1) / (6 - start_year)`, so it earns `revenue_year5 / (6 - start_year)` in its first year and `revenue_year5` in year 5. With `w` the success probability of the scenario, it adds `w x I_t x margin x cc x a` to `C_t` (discounted like the core) and `w x revenue_year5 x margin x multiple` to `TV`. Net debt belongs to the core. The result reports, per scenario and probability-weighted, the core value per share (net debt included) and the initiatives' value per share, which sum to the value per share, and the year-5 core revenue.

  *Compatibility.* With no `basis`, `start_margin`, `cash_conversion` (0), `tax_rate` or initiatives the formula reduces to `(TV / (1+r)^5 - net debt) / shares`, the original; a table test pins stored versions of that shape to their old values.
- **Helpers:** reverse DCF (the constant 5-year growth at which the same scenario formula, with a given basis, margin path, cash conversion, tax and optional fixed initiatives, equals the current price; the fundamentals panel uses `ebit_ev` when its default margin is the EBIT margin and `net_pe` when it falls back to the net margin), Bazin (dividend / 6%), Graham (√(22.5 × EPS × BVPS)).
- **Tactical:** target price.
- Every fair value is a dated version with inputs and sources. Older than 12 months → review alert; buy gate fails.

### 12.2 Fundamentals panel

Per asset: P/E, EV/EBIT, FCF yield, dividend yield, net debt/EBITDA, revenue and margin history, current vs 5-year range.

### 12.3 Research notes and catalysts

Notes store text and sources. A note can be flagged `opportunity` with an expiry date (§8.2). AI drafts become notes only after user approval.

### 12.4 Expected vs actual

After each earnings release, compare reported revenue and EPS against the base scenario's implied quarterly path. Missing the base for 2+ consecutive quarters → thesis review alert.

**Expected path (implemented in `internal/engine/expected.go`).** For a reported quarter, take the latest `scenario_dcf` fair value version dated before the quarter's period end, and its base scenario. The base year is the last fiscal year ending on or before the version date (revenue and net income from the fundamentals; the revenue used is stored with the comparison). Quarters are numbered from that fiscal year end (quarter 1 is the first after it, up to 20). With annual path `R_k = base x (1+g_1) x ... x (1+g_k)`, year `k` is split into four quarters growing geometrically at the year's own rate, `r = (1+g_k)^(1/4)`: quarter `i` of the year is `R_k x r^(i-1) / (1 + r + r^2 + r^3)`, so the four sum to `R_k` and an evenly split `R_k / 4` (which would put every early quarter of a growing business below its expected value) is avoided. Initiatives are part of the path: each is weighted by its base success probability `w`, year `k` revenue is `T_k = R_k + sum_i w x I_i,k` and the quarterly split uses the growth `T_k / T_(k-1)` of that total (the core rate when there are no initiatives). Expected EPS is quarter revenue x a blended margin over the version's share count. EPS is compared for both bases, after tax: for `net_pe` the margin is the net margin; for `ebit_ev` the EBIT margin is taxed with the scenario's `tax_rate` (`a = 1 - tax`), an approximation that ignores interest and other items (stored versions have tax 0, so their expected EPS is unchanged). The margin moves linearly from the scenario's start margin (or, when it has none, the base year's net margin `m_0`, as before) to the terminal margin `m_T` times `a`: `m_k = s + (m_T x a - s) x k / 5` for year `k` (with a start margin `s`, `m_k = (s + (m_T - s) x k / 5) x a`); the blended margin of year `k` is `(R_k x m_k + sum_i w x I_i,k x margin_i x a) / T_k`. A version without a base-year net income or share count compares revenue only (EPS is n/a). A quarter before the base year end or after the 5-year path has no expectation.

**Miss and alert.** A figure is a miss when `actual < expected - tolerance x |expected|`, tolerance default 2% (setting `expectations.miss_tolerance`). Revenue and EPS are tracked separately; when either has missed for `expectations.consecutive_quarters` (default 2) consecutive quarters ending at the latest reported quarter, `expected_vs_actual_miss` is raised for the asset (deduped by type and asset) and, the first time it opens, an active thesis moves to `under_review`. A quarter with no comparison breaks a streak. The alert resolves when the latest quarter meets or beats the path on both metrics. Quarterly actuals: CVM ITR (Q1 to Q3, 3-month column) and SEC 10-Q, with Q4 derived as annual minus Q3 year to date (docs/data-sources.md §6, §7). Evaluated by the daily `expected_vs_actual` evaluator and after each fundamentals refresh.

## 13. AI analyst

- On demand, per asset (button). Never automatic, never auto-applied.
- Model: `claude-sonnet-5-5` (configurable). Monthly spend cap (default USD 20); runs are refused when the cap is reached; usage shown in settings.
- Inputs: the asset's thesis, break conditions, current fair value and scenarios, fundamentals, and fetched documents:
  - US: SEC EDGAR 10-K, 10-Q, 8-K, earnings releases.
  - BR: CVM filings (ITR, DFP, fatos relevantes) and the company's RI releases.
- Output (structured JSON): catalysts, market size notes, bear/base/bull scenario drafts (each stating its basis, start margin, cash conversion and tax rate), new-business initiatives with sources and a success probability per scenario, fair value range, thesis check per break condition (holds / at risk / broken, with evidence), sources with links.
- The run is saved as a `draft`; the user edits and approves parts into research notes, scenarios, or a new fair value version.

## 14. Corporate actions

- Daily job pulls events (the `stockDividends` field of brapi's dividends endpoint; US splits from the US price source) and cross-checks each against the ratio of **raw** closes around the event date. brapi prices are retroactively adjusted, so raw closes come from B3 COTAHIST (FATCOT is always 1, so splits appear only as raw close jumps). brapi `exDate` is null for stock events; use the approval/last-date-with fields.
- Detected events wait in a confirmation queue; the user confirms or rejects. Confirmed events adjust quantity and average cost (§11.1).
- A suspicious price jump with no detected event raises an alert to enter the event manually.

## 15. Performance

- Daily snapshots of portfolio and per-position value in BRL.
- Time-weighted return, daily sub-periods: `r = (V_t - F_t) / V_{t-1} - 1`, flows at end of day. Dividends are internal (never a flow).
- Flow model. All values in BRL (USD at the day's PTAX).
  - A buy or sell is valued at the day's close (the same basis as `V_t`), so the move between execution price and close is neither return nor flow; fees do not enter the flow. Without a price on or before the day the cash amount is used.
  - Position TWR: its own buys are inflows and sells outflows. Sleeve (core, tactical) and market (BR, US) TWR: buys/sells of assets in that group.
  - Portfolio TWR: `F_t` = sum of trade flows + deposits - withdrawals on cash instruments (dividend credits excluded). A buy is therefore an external flow unless a cash instrument withdrawal of the same amount is recorded on the same date, which nets it to zero. A sale whose proceeds are not recorded as a cash deposit is an outflow.
  - The first day of a series is its starting capital, reported as a net flow (start value 0).
  - "Rebuild history" (`POST /api/performance/backfill`) deletes and recomputes every snapshot day, so changes to this model apply to existing databases.
- Benchmarks: CDI, IBOV; S&P 500 in BRL for the US sleeve.
- Views: total, core vs tactical, BR vs US, per position.

## 16. Daily job and alerts

### 16.1 Pipeline (19:00 BRT)

1. Fetch prices (B3, US), FX, CDI rate, Tesouro Selic price, index levels. CDI and Tesouro Selic are published D-1: always take the latest available point and record its reference date.
2. Detect dividends and corporate actions.
3. Recompute beta, weights, cash target, targets, bands.
4. Evaluate all triggers; upsert alerts (dedupe by type + asset); resolve alerts whose condition cleared.
5. Write snapshot.

Each step is idempotent; failures are logged and surfaced as a "data fetch failed" alert without blocking later steps that do not depend on it.

### 16.2 Alert types

overweight, overvalued, thesis_broken, thesis_review_due, fair_value_stale, missing_fair_value, raise_cash, deploy_cash, better_opportunity, below_floor, undeployed_budget, too_many_positions, tactical_target_hit, tactical_stop_hit, tactical_expired, tactical_count, opportunity, expected_vs_actual_miss, corporate_action_pending, price_jump_unexplained, tax_pool_conflict, bootstrap_exit, data_fetch_failed.

Delivery: dashboard inbox only.

## 17. Data sources

Details, endpoints and fixtures: `docs/data-sources.md`.

| Need | Source | Notes |
|---|---|---|
| B3 daily quotes | brapi.dev free plan (token) | 1 asset per request, 15k req/month, 30 min freshness. Store raw close daily. |
| B3 history (beta, backfill, corp-action check) | B3 COTAHIST yearly/monthly/daily files | Verified (#37). Official, unadjusted, fixed-width 245 chars. Daily file for D lands ~21:00 BRT (run COTAHIST step at 21:30 or take D-1). Decode UTF-8 with Latin-1 fallback, slice by characters. Lot market = TPMERC 010; join renamed tickers by ISIN. |
| B3 dividends, stock events | brapi dividends endpoint | Free plan "limited"; fallback CVM IPE (avisos aos acionistas). |
| US prices, splits, dividends | Tiingo free tier | 50 req/h, 1,000/day, 500 symbols/month, personal use; raw + adjusted OHLC, `divCash`, `splitFactor`. Docs only until a key exists. Yahoo returned 429; optional fallback. |
| USDBRL | BCB PTAX (Olinda API) | Verified. |
| CDI | BCB SGS series 12 | Verified; D-1; requires `dataInicial`, max 10-year window. |
| Tesouro Selic price | Tesouro Transparente CSV | Verified; D-1; 14MB file, refreshed ~07:23 BRT. |
| IBOV history | B3 index matrix endpoint (undocumented) | Tested 2024–2026, close only. COTAHIST has no indices; fallback BOVA11 proxy or manual CSV. |
| S&P 500 history | FRED CSV (SP500) | No key; price index only. |
| BR filings | CVM Dados Abertos (DFP, ITR, IPE) | Latin-1, `VERSAO` for restatements. RAD is UI only. |
| US filings, fundamentals | SEC EDGAR submissions, companyfacts | Needs descriptive User-Agent; check content type (HTML on block). |

Tax facts confirmed and still open are listed in `docs/data-sources.md`; all rates in §11 are settings.

## 18. UI

- **Dashboard:** total value, allocation bars (actual vs target vs band) per position, cash gauge vs dynamic target, bootstrap indicator, open alerts count.
- **Alerts inbox:** filter, acknowledge, snooze, act (opens prefilled trade or plan).
- **Asset page:** ratings + history, thesis + break conditions, fair value versions + scenario editor, research notes, fundamentals, trades, dividends, AI analyst runs.
- **Watchlist:** same as asset page; buy gate checklist.
- **Contribution planner:** enter amount, review plan, mark executed.
- **Trades / cash:** record trades, cash movements, dividends, corporate actions queue.
- **Taxes:** monthly and annual views.
- **Performance:** TWR charts vs benchmarks.
- **Settings:** weights, thresholds, API keys, AI cap.

## 19. Phases

- **P1 (MVP):** scaffold, domain + trades, market data, scoring/targets/bands/bootstrap, thesis/watchlist/fair value (manual + scenario DCF), cash target, contribution planner, trim engine, alerts, dashboard.
- **P2:** tactical positions, taxes, dividends, corporate actions, performance.
- **P3:** fundamentals panel + helpers, AI analyst, expected vs actual.
