import { useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'

import { CopyBlock } from '@/components/copy-block'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { SettingsSection, type FieldDef } from '@/components/settings-section'
import { Badge } from '@/components/ui/badge'
import { api, unwrap, type Schemas } from '@/lib/api'
import { money } from '@/lib/format'
import { mcpSetupQuery, ratingWeightsQuery, settingsQuery, settingsStatusQuery } from '@/lib/queries'

export const Route = createFileRoute('/settings')({ component: Page })

// Factor weights default to the engine's weights (engine.DefaultWeights).
const WEIGHT_DEFAULTS: Schemas['WeightsInput'] = {
  volatility: '1',
  understanding: '1',
  moat: '1.5',
  predictability: '1.5',
  horizon: '1',
}

const WEIGHT_FIELDS: FieldDef[] = [
  { key: 'volatility', label: 'Volatility', kind: 'decimal' },
  { key: 'understanding', label: 'Understanding', kind: 'decimal' },
  { key: 'moat', label: 'Moat', kind: 'decimal' },
  { key: 'predictability', label: 'Predictability', kind: 'decimal' },
  { key: 'horizon', label: 'Horizon', kind: 'decimal' },
]

const ALLOCATION_FIELDS: FieldDef[] = [
  { key: 'raw_base', label: 'Raw weight at score 1', kind: 'percent', group: 'Targets' },
  { key: 'raw_slope', label: 'Added per score point', kind: 'percent' },
  { key: 'clamp_min', label: 'Lowest target', kind: 'percent' },
  { key: 'clamp_max', label: 'Highest target', kind: 'percent' },
  { key: 'band_lower_mult', label: 'Lower band (× target)', kind: 'decimal', unit: '×', group: 'Bands and caps' },
  { key: 'band_upper_mult', label: 'Upper band (× target)', kind: 'decimal', unit: '×' },
  { key: 'band_upper_cap', label: 'Upper band ceiling', kind: 'percent' },
  { key: 'hard_floor', label: 'Hard floor', kind: 'percent' },
  { key: 'hard_cap', label: 'Hard cap', kind: 'percent' },
  { key: 'cash_base', label: 'Cash base', kind: 'percent', group: 'Cash' },
  { key: 'cash_span', label: 'Cash span', kind: 'percent' },
  { key: 'cash_band_min', label: 'Cash minimum', kind: 'percent' },
  { key: 'cash_band_max', label: 'Cash maximum', kind: 'percent' },
  { key: 'overvalued_mult', label: 'Overvalued above (× fair value)', kind: 'decimal', unit: '×', group: 'Buy gate and opportunities' },
  { key: 'min_score', label: 'Buy gate minimum score', kind: 'decimal', unit: 'pts' },
  { key: 'fair_value_max_months', label: 'Fair value max age (months)', kind: 'int' },
  { key: 'fair_value_tolerance', label: 'Fair value tolerance (above fair value)', kind: 'percent', hint: 'The buy gate still passes while the price is up to this much above fair value.' },
  { key: 'opportunity_mult', label: 'Opportunity at or below (× fair value)', kind: 'decimal', unit: '×' },
  { key: 'ramp_stages', label: 'Ramp tranches', kind: 'int', hint: 'Tranches a new position is built in.' },
  { key: 'trim_fraction', label: 'Trim step', kind: 'percent', group: 'Trims' },
  { key: 'cooldown_days', label: 'Trim cooldown', kind: 'int', unit: 'days' },
  { key: 'better_score_delta', label: 'Better alternative: score lead', kind: 'decimal', unit: 'pts' },
  { key: 'better_upside_delta', label: 'Better alternative: upside lead', kind: 'percent' },
]

const PLANNER_FIELDS: FieldDef[] = [
  { key: 'bootstrap_below', label: 'Bootstrap below', kind: 'decimal', unit: 'R$', hint: 'Portfolio value under which bootstrap rules apply.' },
  { key: 'min_order_b3', label: 'Minimum B3 order', kind: 'decimal', unit: 'R$', hint: 'Smaller orders are skipped and the money rolls on.' },
  { key: 'us_batch_min', label: 'US batch', kind: 'decimal', unit: 'R$', hint: 'BRL accumulated before a US purchase is proposed.' },
  { key: 'max_new_positions', label: 'New positions per plan', kind: 'int', hint: 'Most positions one contribution opens (0 for no limit).' },
  { key: 'us_vehicle', label: 'US vehicle', kind: 'text', hint: 'bdr buys the linked BDR on B3 when one exists; direct buys US shares through a batched remittance.' },
]

const TAX_FIELDS: FieldDef[] = [
  { key: 'exemption_limit', label: 'Monthly exemption limit', kind: 'decimal', unit: 'R$', group: 'B3 tax' },
  { key: 'swing_rate', label: 'Swing trade rate', kind: 'percent' },
  { key: 'day_trade_rate', label: 'Day trade rate', kind: 'percent' },
  { key: 'darf_min', label: 'Minimum DARF', kind: 'decimal', unit: 'R$' },
  { key: 'darf_code', label: 'DARF code (swing)', kind: 'text' },
  { key: 'day_trade_darf_code', label: 'DARF code (day trade)', kind: 'text', hint: '6015 or 6047: confirm with your contador.' },
  { key: 'day_trade_darf_code_confirmed', label: 'Day trade code confirmed', kind: 'bool' },
  { key: 'jcp_rate', label: 'JCP withholding', kind: 'percent', group: 'Dividends' },
  { key: 'dividend_rate', label: 'Dividend withholding', kind: 'percent' },
  { key: 'dividend_monthly', label: 'Dividend limit per payer', kind: 'decimal', unit: 'R$', hint: 'Per month, above which withholding applies.' },
  { key: 'us_rate', label: 'US dividend withholding', kind: 'percent' },
]

const TACTICAL_FIELDS: FieldDef[] = [
  { key: 'max_loss', label: 'Loss at the stop', kind: 'percent', hint: 'Share of the portfolio.' },
  { key: 'min_size', label: 'Smallest position', kind: 'percent' },
  { key: 'max_size', label: 'Largest position', kind: 'percent' },
  { key: 'max_open', label: 'Open positions before a warning', kind: 'int' },
]

const EXPECTATIONS_FIELDS: FieldDef[] = [
  {
    key: 'miss_tolerance',
    label: 'Miss tolerance',
    kind: 'percent',
    hint: 'A reported revenue or EPS this far below the base scenario still counts as met.',
  },
  {
    key: 'consecutive_quarters',
    label: 'Consecutive misses to alert',
    kind: 'int',
    unit: 'quarters',
    hint: 'Quarters in a row below the base scenario that raise the alert and prompt a thesis review.',
  },
]

const AI_FIELDS: FieldDef[] = [
  { key: 'model', label: 'Model', kind: 'text' },
  { key: 'monthly_cap_usd', label: 'Monthly cap', kind: 'decimal', unit: 'US$' },
]

const MCP_FIELDS: FieldDef[] = [
  {
    key: 'daily_new_symbols',
    label: 'New symbols per day',
    kind: 'int',
    hint: 'Tickers not held in the app that Claude may look up each day (America/Sao_Paulo). Over the limit the tools refuse with a clear message.',
  },
]

const VALUATION_FIELDS: FieldDef[] = [
  {
    key: 'erp_us',
    label: 'Equity risk premium, US',
    kind: 'percent',
    hint: 'Expected stock market return over the effective federal funds rate.',
  },
  {
    key: 'erp_b3',
    label: 'Equity risk premium, B3',
    kind: 'percent',
    hint: 'Expected B3 return over the Selic target rate.',
  },
  {
    key: 'min_equity_premium',
    label: 'Minimum premium over risk-free',
    kind: 'percent',
    hint: 'Floor of the cost of equity: risk-free rate + this. Defensive stocks with a low beta land here.',
  },
]

type Values = Record<string, unknown>

function Page() {
  return (
    <>
      <PageHeader
        title="Settings"
        description="Every number the rules use is editable. Saving applies to the next evaluation and contribution plan."
      />
      <div className="space-y-6">
        <WeightsSection />
        <RuleSections />
        <McpSection />
        <DataSources />
      </div>
    </>
  )
}

function WeightsSection() {
  const weights = useQuery(ratingWeightsQuery)
  const qc = useQueryClient()
  if (weights.isPending) return <LoadingBlock rows={2} />
  if (weights.isError) return <ErrorBlock error={weights.error} />
  const put = (body: Schemas['WeightsInput']) => unwrap(api.PUT('/api/settings/rating-weights', { body }))
  return (
    <SettingsSection
      title="Factor weights"
      description="How much each of the five rating factors counts in the weighted score."
      fields={WEIGHT_FIELDS}
      values={weights.data as unknown as Values}
      defaults={WEIGHT_DEFAULTS as unknown as Values}
      save={async (body) => {
        const stored = await put(body as unknown as Schemas['WeightsInput'])
        await qc.invalidateQueries({ queryKey: ratingWeightsQuery.queryKey })
        return stored as unknown as Values
      }}
      reset={async () => {
        await put(WEIGHT_DEFAULTS)
        await qc.invalidateQueries({ queryKey: ratingWeightsQuery.queryKey })
      }}
    />
  )
}

function RuleSections() {
  const all = useQuery(settingsQuery)
  const status = useQuery(settingsStatusQuery)
  const qc = useQueryClient()
  if (all.isPending) return <LoadingBlock rows={6} />
  if (all.isError) return <ErrorBlock error={all.error} />
  const d = all.data
  const refresh = () => qc.invalidateQueries({ queryKey: settingsQuery.queryKey })
  const reset = (group: 'allocation' | 'planner' | 'tax' | 'tactical' | 'ai' | 'expectations' | 'mcp' | 'valuation') => async () => {
    await unwrap(api.POST('/api/settings/{group}/reset', { params: { path: { group } } }))
    await refresh()
  }
  const after = async <T,>(p: Promise<T>) => {
    const stored = await p
    await refresh()
    return stored as unknown as Values
  }
  const usage = status.data?.ai_usage_usd
  return (
    <>
      <SettingsSection
        title="Allocation rules"
        description="Target weights, bands, cash, the buy gate and trims. Percentages are shares of the portfolio."
        fields={ALLOCATION_FIELDS}
        values={d.allocation.values as unknown as Values}
        defaults={d.allocation.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/allocation', { body: body as unknown as Schemas['Allocation'] })))}
        reset={reset('allocation')}
      />
      <SettingsSection
        title="Bootstrap and planner"
        fields={PLANNER_FIELDS}
        values={d.planner.values as unknown as Values}
        defaults={d.planner.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/planner', { body: body as unknown as Schemas['Planner'] })))}
        reset={reset('planner')}
      />
      <SettingsSection
        title="Tax"
        description="B3 monthly tax and dividend withholding. Re-check these against current law."
        fields={TAX_FIELDS}
        values={d.tax.values as unknown as Values}
        defaults={d.tax.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/tax', { body: body as unknown as Schemas['Tax'] })))}
        reset={reset('tax')}
      />
      <SettingsSection
        title="Tactical sleeve"
        fields={TACTICAL_FIELDS}
        values={d.tactical.values as unknown as Values}
        defaults={d.tactical.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/tactical', { body: body as unknown as Schemas['Tactical'] })))}
        reset={reset('tactical')}
      />
      <SettingsSection
        title="Expected vs actual"
        description="Reported quarters are compared with the base scenario of the fair value version that preceded them."
        fields={EXPECTATIONS_FIELDS}
        values={d.expectations.values as unknown as Values}
        defaults={d.expectations.defaults as unknown as Values}
        save={(body) =>
          after(unwrap(api.PUT('/api/settings/expectations', { body: body as unknown as Schemas['Expectations'] })))
        }
        reset={reset('expectations')}
      />
      <SettingsSection
        title="Discount rate (CAPM)"
        description="Default discount rate of each asset: risk-free rate + adjusted beta × equity risk premium, where adjusted beta = 0.67 × beta + 0.33. Risk-free rates are fetched daily (effective federal funds rate from FRED for US, Selic target from the BCB for B3)."
        fields={VALUATION_FIELDS}
        values={d.valuation.values as unknown as Values}
        defaults={d.valuation.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/valuation', { body: body as unknown as Schemas['CAPMInputs'] })))}
        reset={reset('valuation')}
      />
      <SettingsSection
        title="AI analyst"
        description="Model and monthly spending cap for the analyst."
        aside={
          <p className="text-muted-foreground text-sm">
            Month to date <span className="num text-foreground">{money(usage ?? '0', 'USD')}</span>
          </p>
        }
        fields={AI_FIELDS}
        values={d.ai.values as unknown as Values}
        defaults={d.ai.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/ai', { body: body as unknown as Schemas['AI'] })))}
        reset={reset('ai')}
      />
      <SettingsSection
        title="MCP limits"
        description="Limits for the MCP server that lets Claude read your data and submit drafts."
        fields={MCP_FIELDS}
        values={d.mcp.values as unknown as Values}
        defaults={d.mcp.defaults as unknown as Values}
        save={(body) => after(unwrap(api.PUT('/api/settings/mcp', { body: body as unknown as Schemas['MCP'] })))}
        reset={reset('mcp')}
      />
    </>
  )
}

function McpSection() {
  const setup = useQuery(mcpSetupQuery)
  const exe = setup.data?.executable || '<repo>/bin/stockportfolio'
  const config = setup.data?.config_path || '<repo>/config.local.toml'
  const code = `claude mcp add stock-portfolio --scope user -- ${exe} mcp -config ${config}`
  const desktop = JSON.stringify(
    { mcpServers: { 'stock-portfolio': { command: 'wsl.exe', args: ['-e', exe, 'mcp', '-config', config] } } },
    null,
    2,
  )
  return (
    <section className="bg-card min-w-0 overflow-hidden rounded-lg border p-4">
      <h2 className="eyebrow">Claude over MCP</h2>
      <p className="text-muted-foreground mt-1 mb-3 max-w-prose text-sm">
        Run analyses in your own Claude (Claude Code or Claude Desktop) instead of paid API calls. The MCP server reads
        this same database, works whether or not this web app is running, and only ever submits drafts for you to
        review. This app never edits Claude&apos;s configuration: copy the snippets yourself.
      </p>
      {setup.data && !setup.data.executable && (
        <p role="status" className="text-brass mb-3 text-sm">
          This server is not running from a built binary, so the path below is a placeholder. Run <code>make build</code>{' '}
          and replace <code>&lt;repo&gt;</code> with the repository&apos;s absolute path.
        </p>
      )}
      <h3 className="mt-2 text-sm font-medium">Claude Code (WSL)</h3>
      <p className="text-muted-foreground text-sm">
        Run once in a terminal. <code>--scope user</code> makes it available in every directory; the config path makes the
        database resolve wherever Claude starts it.
      </p>
      <CopyBlock text={code} label="Claude Code command" />
      <h3 className="mt-4 text-sm font-medium">Claude Desktop (Windows)</h3>
      <p className="text-muted-foreground text-sm">
        Add this to <code>%APPDATA%\Claude\claude_desktop_config.json</code> (merge it into any existing{' '}
        <code>mcpServers</code>), then restart Claude Desktop. It starts the Linux binary through WSL.
      </p>
      <CopyBlock text={desktop} label="Claude Desktop JSON" />
      <p className="text-muted-foreground mt-4 max-w-prose text-sm">
        Then use the prompts <code>analyze_asset</code>, <code>research_ticker</code> and <code>portfolio_review</code>
        (Claude Code: type <code>/mcp__stock-portfolio__analyze_asset WEGE3</code>). Drafts appear on the asset&apos;s AI
        analyst tab and new tickers under Candidates on the Watchlist page.
      </p>
    </section>
  )
}

function DataSources() {
  const status = useQuery(settingsStatusQuery)
  return (
    <section className="bg-card rounded-lg border p-4">
      <h2 className="eyebrow">Data sources and keys</h2>
      <p className="text-muted-foreground mt-1 mb-3 max-w-prose text-sm">
        Keys are set in the config file or environment variables and are never shown here.
      </p>
      {status.isPending ? (
        <LoadingBlock rows={2} />
      ) : status.isError ? (
        <ErrorBlock error={status.error} />
      ) : (
        <ul className="divide-y">
          {(status.data.keys ?? []).map((k) => (
            <li key={k.name} className="flex flex-wrap items-center justify-between gap-2 py-2 text-sm">
              <span>{k.label}</span>
              <span className="flex items-center gap-2">
                {k.configured && <span className="text-muted-foreground text-xs">from {k.source}</span>}
                <Badge variant={k.configured ? 'secondary' : 'outline'}>{k.configured ? 'Configured' : 'Not set'}</Badge>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
