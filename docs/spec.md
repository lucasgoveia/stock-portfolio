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

- **Backend:** Go, single binary. `net/http` + chi, SQLite via `modernc.org/sqlite` (pure Go), sqlc for queries, goose for migrations. Monetary and quantity values use `shopspring/decimal`, stored as TEXT.
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
- Day trades (buy and sell same asset same day) are flagged; 20% rate shown. DARF code for day trade (6015 vs 6047) unconfirmed.

### 11.3 US assets

- Gains and dividends on foreign assets taxed annually (15%, Lei 14.754/2023), reported in the annual DIRPF. System produces a yearly report of realized gains in BRL and dividends with US withholding. Whether US withholding is creditable (art. 4, reciprocity) is unconfirmed; show it separately.
- FX convention for foreign cost basis must be confirmed before release.

### 11.4 Outputs

Monthly tax page (sales, exempt/not, gains, losses carried, DARF value) and annual report data for DIRPF (positions at 31/12, cost basis, dividends/JCP received).

## 12. Valuation and research

### 12.1 Fair value methods

- **Scenario DCF (primary):** bear/base/bull scenarios, each with 5-year revenue growth, terminal margin, exit multiple, discount rate, probability. Fair value = probability-weighted present value per share. Growth assumptions come from research and may exceed historical trends; historical multiples are shown only as a sanity check.
- **Helpers:** reverse DCF (implied growth at current price), Bazin (dividend / 6%), Graham (√(22.5 × EPS × BVPS)).
- **Tactical:** target price.
- Every fair value is a dated version with inputs and sources. Older than 12 months → review alert; buy gate fails.

### 12.2 Fundamentals panel

Per asset: P/E, EV/EBIT, FCF yield, dividend yield, net debt/EBITDA, revenue and margin history, current vs 5-year range.

### 12.3 Research notes and catalysts

Notes store text and sources. A note can be flagged `opportunity` with an expiry date (§8.2). AI drafts become notes only after user approval.

### 12.4 Expected vs actual

After each earnings release, compare reported revenue and EPS against the base scenario's implied quarterly path. Missing the base for 2+ consecutive quarters → thesis review alert.

## 13. AI analyst

- On demand, per asset (button). Never automatic, never auto-applied.
- Model: `claude-sonnet-5-5` (configurable). Monthly spend cap (default USD 20); runs are refused when the cap is reached; usage shown in settings.
- Inputs: the asset's thesis, break conditions, current fair value and scenarios, fundamentals, and fetched documents:
  - US: SEC EDGAR 10-K, 10-Q, 8-K, earnings releases.
  - BR: CVM filings (ITR, DFP, fatos relevantes) and the company's RI releases.
- Output (structured JSON): catalysts, market size notes, bear/base/bull scenario drafts, fair value range, thesis check per break condition (holds / at risk / broken, with evidence), sources with links.
- The run is saved as a `draft`; the user edits and approves parts into research notes, scenarios, or a new fair value version.

## 14. Corporate actions

- Daily job pulls events (the `stockDividends` field of brapi's dividends endpoint; US splits from the US price source) and cross-checks each against the ratio of **raw** closes around the event date. brapi prices are retroactively adjusted, so the system stores each day's raw close at fetch time and uses B3 COTAHIST (unadjusted) for history. brapi `exDate` is null for stock events; use the approval/last-date-with fields.
- Detected events wait in a confirmation queue; the user confirms or rejects. Confirmed events adjust quantity and average cost (§11.1).
- A suspicious price jump with no detected event raises an alert to enter the event manually.

## 15. Performance

- Daily snapshots of portfolio and per-position value in BRL.
- Time-weighted return (sub-periods split at contributions/withdrawals; dividends are internal).
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
| B3 history (beta, backfill, corp-action check) | B3 COTAHIST yearly/daily files | Official, unadjusted. To verify (#37). |
| B3 dividends, stock events | brapi dividends endpoint | Free plan "limited"; fallback CVM IPE (avisos aos acionistas). |
| US prices, splits, dividends | Tiingo free tier | To verify (#37). Yahoo returned 429; optional fallback only. |
| USDBRL | BCB PTAX (Olinda API) | Verified. |
| CDI | BCB SGS series 12 | Verified; D-1; requires `dataInicial`, max 10-year window. |
| Tesouro Selic price | Tesouro Transparente CSV | Verified; D-1; 14MB file, refreshed ~07:23 BRT. |
| IBOV history | B3 index endpoint (undocumented) | Fragile; COTAHIST fallback to verify. |
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
