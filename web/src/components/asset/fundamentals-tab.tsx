import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { ExpectedVsActual } from '@/components/asset/expected-vs-actual'
import { Panel } from '@/components/asset/panel'
import { Field, FormError } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import {
  dateTime,
  decimalInput,
  fractionToPercent,
  money,
  moneyCompact,
  multiple,
  pct,
  percentToFraction,
  upside,
} from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Asset = Schemas['Asset']
type PanelData = Schemas['FundamentalsPanel']
type HistoryPoint = Schemas['HistoryPoint']
type RangeData = Schemas['Range']

/** Helper inputs as typed (percent for rates and yields); '' keeps the server default. */
interface Inputs {
  bazinYield: string
  margin: string
  exit: string
  discount: string
}

/** Typed inputs to API query values: fractions for rates, null when blank or invalid. */
function toQuery(i: Inputs) {
  const frac = (v: string) => (v.trim() === '' ? undefined : (percentToFraction(v) ?? undefined))
  const dec = (v: string) => (v.trim() === '' ? undefined : (decimalInput(v) ?? undefined))
  return {
    bazin_yield: frac(i.bazinYield),
    terminal_margin: frac(i.margin),
    exit_multiple: dec(i.exit),
    discount_rate: frac(i.discount),
  }
}

export function FundamentalsTab({ asset }: { asset: Asset }) {
  const [inputs, setInputs] = useState<Inputs>({ bazinYield: '', margin: '', exit: '', discount: '' })
  const typed = useMemo(() => toQuery(inputs), [inputs])
  const query = useDeferredValue(typed)
  const panel = useQuery({
    queryKey: [...qk.asset(asset.id), 'fundamentals', query],
    queryFn: () =>
      unwrap(api.GET('/api/assets/{id}/fundamentals', { params: { path: { id: asset.id }, query } })),
    placeholderData: keepPreviousData,
  })

  if (panel.isPending) return <LoadingBlock rows={5} />
  if (panel.isError) return <ErrorBlock error={panel.error} />
  const p = panel.data
  if (p.status === 'unsupported') {
    return <p className="text-muted-foreground text-sm">{p.message}</p>
  }
  const loaded = p.status === 'ok'
  return (
    <div className="space-y-4">
      <SourceBar asset={asset} panel={p} />
      {!loaded && <EmptyState asset={asset} panel={p} />}
      {loaded && (
        <>
          <MetricCards panel={p} />
          <HistoryPanel history={p.history ?? []} currency={p.currency} />
          <ExpectedVsActual asset={asset} />
          <RangesPanel panel={p} />
          <HelpersPanel asset={asset} panel={p} inputs={inputs} setInputs={setInputs} query={query} />
          <Notes notes={p.notes ?? []} />
        </>
      )}
      <IdentifierForm asset={asset} panel={p} />
    </div>
  )
}

/** Source, as-of date, refresh button and the outcome of the last failed refresh. */
function SourceBar({ asset, panel: p }: { asset: Asset; panel: PanelData }) {
  const qc = useQueryClient()
  const refresh = useMutation({
    mutationFn: () => unwrap(api.POST('/api/assets/{id}/fundamentals/refresh', { params: { path: { id: asset.id } } })),
    onSuccess: () => {
      toast.success('Fundamentals refreshed')
      void qc.invalidateQueries({ queryKey: qk.asset(asset.id) })
    },
    onError: (e) => {
      toast.error(errorMessage(e))
      void qc.invalidateQueries({ queryKey: qk.asset(asset.id) })
    },
  })
  const canRefresh = p.status !== 'needs_identifier'
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <p className="text-muted-foreground text-xs">
          {p.source_name ? (
            <>
              {p.source_name}
              {p.entity ? ` · ${p.entity}` : ''}
              {p.metrics.fiscal_year_end ? ` · fiscal year ended ${p.metrics.fiscal_year_end}` : ''}
              {p.fetched_at ? ` · fetched ${dateTime(p.fetched_at)}` : ''}
            </>
          ) : (
            'No data fetched yet.'
          )}
        </p>
        <Button size="sm" variant="outline" disabled={!canRefresh || refresh.isPending} onClick={() => refresh.mutate()}>
          <RefreshCw className={cn(refresh.isPending && 'animate-spin')} />
          {refresh.isPending ? 'Fetching…' : 'Refresh now'}
        </Button>
      </div>
      {refresh.isPending && p.market === 'B3' && (
        <p className="text-muted-foreground text-xs">
          The first fetch downloads up to six CVM annual files and three quarterly (ITR) files, 13 to 30 MB each; this can take a few minutes.
        </p>
      )}
      {p.last_error && (
        <p role="alert" className="text-breach text-sm">
          The last refresh failed: {p.last_error}
          {p.status === 'ok' ? ' Showing the data from the previous fetch.' : ''}
        </p>
      )}
    </div>
  )
}

function EmptyState({ asset, panel: p }: { asset: Asset; panel: PanelData }) {
  return (
    <Panel title="Fundamentals">
      <p className="max-w-prose text-sm">{p.message}</p>
      <p className="text-muted-foreground mt-2 max-w-prose text-xs">
        {p.status === 'needs_identifier'
          ? 'CVM files do not map tickers to companies, so the CNPJ is entered once per asset. Find it on the company’s RI page or on CVM’s company register.'
          : asset.market === 'US'
            ? 'The SEC CIK is looked up from the ticker on the first fetch.'
            : 'Press “Refresh now” to load the annual statements from CVM.'}
      </p>
    </Panel>
  )
}

/** Set or change the CNPJ (B3) or CIK (US) used to find the company. */
function IdentifierForm({ asset, panel: p }: { asset: Asset; panel: PanelData }) {
  const qc = useQueryClient()
  const isB3 = p.identifier_kind === 'cnpj'
  const [value, setValue] = useState('')
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (v: string) =>
      unwrap(
        api.PATCH('/api/assets/{id}', {
          params: { path: { id: asset.id } },
          body: isB3 ? { cnpj: v } : { cik: v },
        }),
      ),
    onSuccess: () => {
      toast.success(`${isB3 ? 'CNPJ' : 'CIK'} saved`)
      setValue('')
      setError(null)
      void qc.invalidateQueries()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const needs = p.status === 'needs_identifier'
  const form = (
    <form
      className="flex flex-wrap items-end gap-3"
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        if (!isB3 && value.trim() !== '' && !/^\d{1,10}$/.test(value.trim())) return setError('A CIK has up to 10 digits.')
        if (isB3 && value.trim() === '') return setError('Enter the 14-digit CNPJ.')
        save.mutate(value.trim())
      }}
    >
      <Field label={isB3 ? 'CNPJ' : 'SEC CIK'} className="w-full sm:w-64" hint={isB3 ? 'Example: 33.000.167/0001-01' : 'Optional; looked up from the ticker when empty.'}>
        {(id) => (
          <Input
            id={id}
            inputMode="numeric"
            value={value}
            placeholder={p.identifier || (isB3 ? '00.000.000/0000-00' : '0000320193')}
            onChange={(e) => setValue(e.target.value)}
          />
        )}
      </Field>
      <Button type="submit" variant={needs ? 'default' : 'outline'} disabled={save.isPending}>
        Save {isB3 ? 'CNPJ' : 'CIK'}
      </Button>
      <FormError error={error} />
    </form>
  )
  if (needs) {
    return <Panel title="Company identifier">{form}</Panel>
  }
  return (
    <details className="text-sm">
      <summary className="text-muted-foreground hover:text-foreground cursor-pointer text-xs">
        {isB3 ? 'CNPJ' : 'CIK'}: <span className="num">{p.identifier || 'not set yet'}</span> · change
      </summary>
      <div className="mt-3">{form}</div>
    </details>
  )
}

function MetricCards({ panel: p }: { panel: PanelData }) {
  const m = p.metrics
  const c = p.currency
  const cards: { label: string; value: string; sub?: string }[] = [
    { label: 'P/E', value: multiple(m.pe), sub: m.eps ? `EPS ${money(m.eps, c)}` : undefined },
    { label: 'EV/EBIT', value: multiple(m.ev_ebit), sub: m.enterprise_value ? `EV ${moneyCompact(m.enterprise_value, c)}` : undefined },
    { label: 'FCF yield', value: pct(m.fcf_yield), sub: m.fcf ? `FCF ${moneyCompact(m.fcf, c)}` : undefined },
    {
      label: 'Dividend yield',
      value: pct(m.dividend_yield),
      sub: m.dividends_per_share ? `${money(m.dividends_per_share, c)} per share` : undefined,
    },
    {
      label: 'Net debt / EBITDA',
      value: multiple(m.net_debt_ebitda),
      sub: m.net_debt ? `Net debt ${moneyCompact(m.net_debt, c)}` : undefined,
    },
    { label: 'Market cap', value: moneyCompact(m.market_cap, c), sub: m.price ? `Price ${money(m.price, c)} · ${m.price_date}` : 'No stored price' },
  ]
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
      {cards.map((k) => (
        <div key={k.label} className="bg-card rounded-lg border p-3">
          <dt className="eyebrow">{k.label}</dt>
          <dd className="num mt-1 text-xl font-semibold">{k.value}</dd>
          <dd className="num text-muted-foreground mt-0.5 truncate text-xs">{k.sub ?? ' '}</dd>
        </div>
      ))}
    </dl>
  )
}

const W = 320
const H = 150
const PAD = { l: 8, r: 8, t: 16, b: 22 }

const yearOf = (d: string) => d.slice(0, 4)

/** Revenue bars and margin lines over up to five fiscal years. */
function HistoryPanel({ history, currency }: { history: HistoryPoint[]; currency: string }) {
  const withRev = history.filter((h) => h.revenue != null)
  if (withRev.length === 0) {
    return (
      <Panel title="Revenue and margins">
        <p className="text-muted-foreground text-sm">No revenue history stored.</p>
      </Panel>
    )
  }
  return (
    <Panel title={`Revenue and margins, ${history.length} years`}>
      <div className="grid gap-6 md:grid-cols-2">
        <RevenueBars history={history} currency={currency} />
        <MarginLines history={history} />
      </div>
    </Panel>
  )
}

function RevenueBars({ history, currency }: { history: HistoryPoint[]; currency: string }) {
  const max = Math.max(...history.map((h) => Number(h.revenue ?? 0)), 1)
  const bw = (W - PAD.l - PAD.r) / history.length
  return (
    <figure>
      <figcaption className="text-muted-foreground mb-1 text-xs">Revenue ({currency})</figcaption>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Revenue by fiscal year: ${history.map((h) => `${yearOf(h.period_end)} ${moneyCompact(h.revenue, currency)}`).join(', ')}`}
        className="w-full"
      >
        {history.map((h, i) => {
          const v = Number(h.revenue ?? 0)
          const bh = (v / max) * (H - PAD.t - PAD.b)
          const x = PAD.l + i * bw + bw * 0.15
          return (
            <g key={h.period_end}>
              <rect x={x} y={H - PAD.b - bh} width={bw * 0.7} height={bh} rx={2} fill="var(--series-1)" />
              <text x={x + bw * 0.35} y={H - PAD.b - bh - 4} textAnchor="middle" fontSize="9" className="num fill-foreground">
                {moneyCompact(h.revenue, currency).replace(/^[^\d-]+/, '')}
              </text>
              <text x={x + bw * 0.35} y={H - 6} textAnchor="middle" fontSize="10" className="num fill-muted-foreground">
                {yearOf(h.period_end)}
              </text>
            </g>
          )
        })}
        <line x1={PAD.l} x2={W - PAD.r} y1={H - PAD.b} y2={H - PAD.b} className="stroke-border" />
      </svg>
    </figure>
  )
}

const MARGINS = [
  { key: 'ebit_margin', label: 'EBIT margin', color: 'var(--series-2)' },
  { key: 'net_margin', label: 'Net margin', color: 'var(--series-3)' },
] as const

function MarginLines({ history }: { history: HistoryPoint[] }) {
  const vals = history.flatMap((h) => MARGINS.map((m) => h[m.key]).filter((v): v is string => v != null).map(Number))
  if (vals.length === 0) {
    return <p className="text-muted-foreground text-sm">No margin history stored.</p>
  }
  const lo = Math.min(0, ...vals)
  const hi = Math.max(0.01, ...vals)
  const x = (i: number) => PAD.l + 12 + (i / Math.max(history.length - 1, 1)) * (W - PAD.l - PAD.r - 24)
  const y = (v: number) => PAD.t + (1 - (v - lo) / (hi - lo)) * (H - PAD.t - PAD.b)
  return (
    <figure>
      <figcaption className="text-muted-foreground mb-1 flex flex-wrap gap-x-3 text-xs">
        {MARGINS.map((m) => (
          <span key={m.key} className="flex items-center gap-1">
            <span className="inline-block h-0.5 w-3" style={{ background: m.color }} />
            {m.label}
          </span>
        ))}
      </figcaption>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Margins by fiscal year: ${history.map((h) => `${yearOf(h.period_end)} EBIT ${pct(h.ebit_margin, 1)}, net ${pct(h.net_margin, 1)}`).join('; ')}`}
        className="w-full"
      >
        {lo < 0 && <line x1={PAD.l} x2={W - PAD.r} y1={y(0)} y2={y(0)} className="stroke-border" strokeDasharray="3 3" />}
        <line x1={PAD.l} x2={W - PAD.r} y1={H - PAD.b} y2={H - PAD.b} className="stroke-border" />
        {MARGINS.map((m) => {
          const pts = history.map((h, i) => ({ i, v: h[m.key] })).filter((p): p is { i: number; v: string } => p.v != null)
          return (
            <g key={m.key}>
              <polyline
                fill="none"
                stroke={m.color}
                strokeWidth="2"
                strokeLinejoin="round"
                points={pts.map((p) => `${x(p.i)},${y(Number(p.v))}`).join(' ')}
              />
              {pts.map((p) => (
                <circle key={p.i} cx={x(p.i)} cy={y(Number(p.v))} r="2.5" fill={m.color} />
              ))}
              {pts.length > 0 && (
                <text
                  x={x(pts[pts.length - 1]!.i)}
                  y={y(Number(pts[pts.length - 1]!.v)) - 6}
                  textAnchor="end"
                  fontSize="9"
                  className="num fill-foreground"
                >
                  {pct(pts[pts.length - 1]!.v, 1)}
                </text>
              )}
            </g>
          )
        })}
        {history.map((h, i) => (
          <text key={h.period_end} x={x(i)} y={H - 6} textAnchor="middle" fontSize="10" className="num fill-muted-foreground">
            {yearOf(h.period_end)}
          </text>
        ))}
      </svg>
    </figure>
  )
}

function RangesPanel({ panel: p }: { panel: PanelData }) {
  return (
    <Panel title="Current vs 5-year range">
      <div className="grid gap-5 md:grid-cols-2 md:gap-8">
        <RangeBar label="P/E" range={p.ranges.pe} />
        <RangeBar label="EV/EBIT" range={p.ranges.ev_ebit} />
      </div>
      <p className="text-muted-foreground mt-4 text-xs">
        Each past point is the stored closing price at a fiscal year-end times that year’s shares, over that year’s earnings. A range needs
        prices at two or more year-ends; run the backfill to load more history.
      </p>
    </Panel>
  )
}

function RangeBar({ label, range }: { label: string; range?: RangeData }) {
  const cur = range?.current
  if (!cur) {
    return (
      <div>
        <div className="eyebrow">{label}</div>
        <p className="text-muted-foreground text-sm">Not available (negative earnings or no price).</p>
      </div>
    )
  }
  const hasRange = range.min != null && range.max != null
  const pos = Math.min(Math.max(Number(range.position ?? 0.5), 0), 1)
  return (
    <div>
      <div className="flex items-baseline justify-between">
        <div className="eyebrow">{label}</div>
        <div className="num text-sm">{multiple(cur)}</div>
      </div>
      {hasRange ? (
        <>
          <div
            role="img"
            aria-label={`${label} is ${multiple(cur)}, within a historical range of ${multiple(range.min)} to ${multiple(range.max)}`}
            className="relative mt-1 h-6"
          >
            <div className="bg-muted absolute inset-x-0 top-1/2 h-2 -translate-y-1/2 rounded-full" />
            <div
              className="bg-foreground absolute top-1/2 h-5 w-1 -translate-x-1/2 -translate-y-1/2 rounded-full"
              style={{ left: `calc(4px + (100% - 8px) * ${pos})` }}
            />
          </div>
          <div className="num text-muted-foreground flex justify-between text-xs">
            <span>low {multiple(range.min)}</span>
            <span>high {multiple(range.max)}</span>
          </div>
        </>
      ) : (
        <p className="text-muted-foreground mt-1 text-xs">
          Only the current value: stored prices cover {(range.points ?? []).length} fiscal year-end
          {(range.points ?? []).length === 1 ? '' : 's'}, and a range needs two.
        </p>
      )}
    </div>
  )
}

function HelpersPanel({
  asset,
  panel: p,
  inputs,
  setInputs,
  query,
}: {
  asset: Asset
  panel: PanelData
  inputs: Inputs
  setInputs: (i: Inputs) => void
  query: ReturnType<typeof toQuery>
}) {
  const h = p.helpers
  const price = p.metrics.price
  return (
    <Panel title="Valuation helpers">
      <p className="text-muted-foreground mb-4 max-w-prose text-xs">
        Cross-checks, not the primary method. Create a fair value version from Bazin or Graham to keep it in the history with its
        inputs; the reverse DCF only shows what growth the price implies.
      </p>
      <div className="grid gap-4 lg:grid-cols-3">
        <div className="space-y-2">
          <div className="eyebrow">Bazin</div>
          <HelperNumber value={h.bazin.value} currency={p.currency} price={price} />
          <Field label="Required dividend yield, %">
            {(id) => (
              <Input
                id={id}
                inputMode="decimal"
                className="num"
                value={inputs.bazinYield}
                placeholder={h.bazin.required_yield ? fractionToPercent(h.bazin.required_yield) : '6'}
                onChange={(e) => setInputs({ ...inputs, bazinYield: e.target.value })}
              />
            )}
          </Field>
          <p className="text-muted-foreground text-xs">{h.bazin.note}</p>
          <CreateVersion asset={asset} method="bazin" disabled={h.bazin.value == null} query={query} />
        </div>
        <div className="space-y-2">
          <div className="eyebrow">Graham</div>
          <HelperNumber value={h.graham.value} currency={p.currency} price={price} />
          <p className="text-muted-foreground text-xs">
            {h.graham.note}
            {p.metrics.eps && p.metrics.bvps
              ? ` · EPS ${money(p.metrics.eps, p.currency)}, book value ${money(p.metrics.bvps, p.currency)} per share`
              : ''}
          </p>
          <CreateVersion asset={asset} method="graham" disabled={h.graham.value == null} query={query} />
        </div>
        <ReverseDCFBox panel={p} inputs={inputs} setInputs={setInputs} />
      </div>
    </Panel>
  )
}

function HelperNumber({ value, currency, price }: { value?: string; currency: string; price?: string }) {
  if (value == null) return <div className="num text-3xl font-semibold">—</div>
  const up = upside(value, price)
  return (
    <div className="flex flex-wrap items-baseline gap-x-3">
      <span className="display num text-3xl font-semibold">{money(value, currency)}</span>
      {up && <span className="num text-muted-foreground text-sm">{up} vs price</span>}
    </div>
  )
}

/** Fetches the draft for a helper and stores it as a fair value version. */
function CreateVersion({
  asset,
  method,
  disabled,
  query,
}: {
  asset: Asset
  method: 'bazin' | 'graham'
  disabled: boolean
  query: ReturnType<typeof toQuery>
}) {
  const qc = useQueryClient()
  const create = useMutation({
    mutationFn: async () => {
      const draft = await unwrap(
        api.GET('/api/assets/{id}/fundamentals/fair-value-draft', {
          params: { path: { id: asset.id }, query: { ...query, method } },
        }),
      )
      return unwrap(
        api.POST('/api/assets/{id}/fair-values', {
          params: { path: { id: asset.id } },
          body: {
            method: draft.method,
            value_per_share: draft.value_per_share,
            currency: draft.currency,
            as_of: draft.as_of,
            sources: draft.sources ?? [],
            note: draft.note,
            inputs: draft.inputs,
          },
        }),
      )
    },
    onSuccess: (fv) => {
      toast.success(`Fair value version created: ${money(fv.value_per_share, fv.currency)} (${method})`, {
        description: 'It is now the current fair value of this asset.',
      })
      void qc.invalidateQueries({ queryKey: qk.asset(asset.id) })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button size="sm" variant="outline" disabled={disabled || create.isPending} onClick={() => create.mutate()}>
        Create fair value version
      </Button>
      {create.isSuccess && (
        <Link to="." search={{ tab: 'valuation' }} className="text-xs underline">
          See versions
        </Link>
      )}
    </div>
  )
}

function ReverseDCFBox({ panel: p, inputs, setInputs }: { panel: PanelData; inputs: Inputs; setInputs: (i: Inputs) => void }) {
  const r = p.helpers.reverse_dcf
  const growth = r.implied_growth
  return (
    <div className="space-y-2">
      <div className="eyebrow">Reverse DCF, implied growth</div>
      {r.status === 'ok' && growth ? (
        <div className="display num text-3xl font-semibold">
          {pct(growth, 1)}
          <span className="text-muted-foreground ml-1 text-sm font-normal">a year, 5 years</span>
        </div>
      ) : (
        <div className="num text-3xl font-semibold">—</div>
      )}
      <div className="grid grid-cols-3 gap-2">
        <Field label={r.basis === 'net_pe' ? 'Net margin, %' : 'EBIT margin, %'}>
          {(id) => (
            <Input
              id={id}
              inputMode="decimal"
              className="num"
              value={inputs.margin}
              placeholder={fractionToPercent(r.terminal_margin)}
              onChange={(e) => setInputs({ ...inputs, margin: e.target.value })}
            />
          )}
        </Field>
        <Field label={r.basis === 'net_pe' ? 'P/E ×' : 'EV/EBIT ×'}>
          {(id) => (
            <Input
              id={id}
              inputMode="decimal"
              className="num"
              value={inputs.exit}
              placeholder={r.exit_multiple}
              onChange={(e) => setInputs({ ...inputs, exit: e.target.value })}
            />
          )}
        </Field>
        <Field label="Discount, %">
          {(id) => (
            <Input
              id={id}
              inputMode="decimal"
              className="num"
              value={inputs.discount}
              placeholder={fractionToPercent(r.discount_rate)}
              onChange={(e) => setInputs({ ...inputs, discount: e.target.value })}
            />
          )}
        </Field>
      </div>
      <p className="text-muted-foreground text-xs">{r.note}</p>
    </div>
  )
}

function Notes({ notes }: { notes: string[] }) {
  if (notes.length === 0) return null
  return (
    <details className="text-xs">
      <summary className="text-muted-foreground hover:text-foreground cursor-pointer">Data notes ({notes.length})</summary>
      <ul className="text-muted-foreground mt-2 list-disc space-y-1 pl-5">
        {notes.map((n) => (
          <li key={n}>{n}</li>
        ))}
      </ul>
    </details>
  )
}
