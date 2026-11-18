import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'

import { Panel } from '@/components/asset/panel'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { api, unwrap, type Schemas } from '@/lib/api'
import { money, moneyCompact, pct } from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Asset = Schemas['Asset']
type Quarter = Schemas['QuarterComparison']

/** How many of the latest quarters the charts show; the table lists all. */
const CHART_QUARTERS = 8

const label = (q: Quarter) => `Q${q.fiscal_quarter} ${q.fiscal_year}`
const shortLabel = (q: Quarter) => `Q${q.fiscal_quarter} '${String(q.fiscal_year).slice(2)}`

/** Reported quarters against the base scenario of the fair value that preceded each (spec 12.4). */
export function ExpectedVsActual({ asset }: { asset: Asset }) {
  const cmp = useQuery({
    queryKey: [...qk.asset(asset.id), 'expected-vs-actual'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/expected-vs-actual', { params: { path: { id: asset.id } } })),
  })
  if (cmp.isPending) return <LoadingBlock rows={3} />
  if (cmp.isError) return <ErrorBlock error={cmp.error} />
  const c = cmp.data
  if (c.status === 'unsupported') return null
  const quarters = c.quarters ?? []
  const compared = quarters.filter((q) => q.expected_revenue != null)
  return (
    <Panel title="Expected vs actual, by quarter">
      <p className="text-muted-foreground mb-4 max-w-prose text-xs">
        Each reported quarter is checked against the base scenario of the scenario DCF version dated before it ended. Revenue follows the
        base growth path; EPS follows the net margin moving linearly to the terminal margin. A figure more than {pct(c.miss_tolerance, 1)}{' '}
        below the path is a miss; {c.consecutive_quarters} misses in a row raise an alert.
      </p>
      {c.alerting && (
        <div role="alert" className="border-breach/40 bg-breach/10 mb-4 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border p-3 text-sm">
          <TriangleAlert className="text-breach size-4 shrink-0" aria-hidden />
          <span>
            Below the base scenario {Math.max(c.revenue_miss_streak, c.eps_miss_streak)} quarters in a row
            {c.revenue_miss_streak >= c.consecutive_quarters && c.eps_miss_streak >= c.consecutive_quarters
              ? ' (revenue and EPS)'
              : c.revenue_miss_streak >= c.consecutive_quarters
                ? ' (revenue)'
                : ' (EPS)'}
            .
          </span>
          <Link to="." search={{ tab: 'thesis' }} className="text-xs underline">
            Review thesis
          </Link>
        </div>
      )}
      {c.message && <p className="text-muted-foreground mb-3 text-sm">{c.message}</p>}
      {quarters.length > 0 && (
        <>
          {compared.length > 0 && (
            <div className="mb-5 grid gap-6 md:grid-cols-2">
              <PairBars title={`Revenue (${c.currency})`} quarters={quarters} kind="revenue" currency={c.currency} />
              <PairBars title={`EPS (${c.currency})`} quarters={quarters} kind="eps" currency={c.currency} />
            </div>
          )}
          <QuarterTable quarters={quarters} currency={c.currency} />
          <ul className="text-muted-foreground mt-3 space-y-1 text-xs">
            {quarters
              .filter((q) => q.note)
              .slice(-3)
              .map((q) => (
                <li key={q.period_end}>
                  {label(q)}: {q.note}
                </li>
              ))}
            {quarters.some((q) => q.derived) && (
              <li>Q4 is derived: the annual figure minus the year to date through Q3, as companies do not report it on its own.</li>
            )}
          </ul>
        </>
      )}
    </Panel>
  )
}

const W = 320
const H = 150
const PAD = { l: 6, r: 6, t: 16, b: 22 }

/** Expected (outlined) and actual (filled) bars per quarter, for revenue or EPS. */
function PairBars({ title, quarters, kind, currency }: { title: string; quarters: Quarter[]; kind: 'revenue' | 'eps'; currency: string }) {
  const rows = quarters.slice(-CHART_QUARTERS).map((q) => ({
    q,
    actual: kind === 'revenue' ? q.actual_revenue : q.actual_eps,
    expected: kind === 'revenue' ? q.expected_revenue : q.expected_eps,
    status: kind === 'revenue' ? q.revenue_status : q.eps_status,
  }))
  const vals = rows.flatMap((r) => [r.actual, r.expected].filter((v): v is string => v != null).map(Number))
  if (vals.length === 0) return null
  const lo = Math.min(0, ...vals)
  const hi = Math.max(0.0001, ...vals)
  const y = (v: number) => PAD.t + (1 - (v - lo) / (hi - lo)) * (H - PAD.t - PAD.b)
  const slot = (W - PAD.l - PAD.r) / rows.length
  const fmt = (v: string) => (kind === 'revenue' ? moneyCompact(v, currency) : money(v, currency))
  const aria = rows.map((r) => `${label(r.q)} actual ${r.actual ? fmt(r.actual) : 'n/a'}, expected ${r.expected ? fmt(r.expected) : 'n/a'}`).join('; ')
  return (
    <figure>
      <figcaption className="text-muted-foreground mb-1 flex flex-wrap items-center gap-x-3 text-xs">
        <span>{title}</span>
        <span className="flex items-center gap-1">
          <span className="inline-block size-2.5 rounded-sm" style={{ background: 'var(--series-1)' }} /> Actual
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block size-2.5 rounded-sm border-2" style={{ borderColor: 'var(--series-2)' }} /> Expected
        </span>
      </figcaption>
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${title} by quarter: ${aria}`} className="w-full">
        {rows.map((r, i) => {
          const x0 = PAD.l + i * slot + slot * 0.1
          const bw = slot * 0.38
          const bar = (v: string | undefined, dx: number, expected: boolean) => {
            if (v == null) return null
            const n = Number(v)
            const top = y(Math.max(n, 0))
            const h = Math.max(Math.abs(y(n) - y(0)), 1)
            return expected ? (
              <rect x={x0 + dx} y={top} width={bw} height={h} rx={2} fill="none" stroke="var(--series-2)" strokeWidth="2" />
            ) : (
              <rect x={x0 + dx} y={top} width={bw} height={h} rx={2} fill="var(--series-1)" />
            )
          }
          return (
            <g key={r.q.period_end}>
              {bar(r.expected, 0, true)}
              {bar(r.actual, bw + slot * 0.04, false)}
              {r.status === 'miss' && (
                <text x={x0 + bw} y={PAD.t - 5} textAnchor="middle" fontSize="9" fontWeight="600" className="fill-breach">
                  miss
                </text>
              )}
              <text x={x0 + bw} y={H - 6} textAnchor="middle" fontSize="9" className="num fill-muted-foreground">
                {shortLabel(r.q)}
              </text>
            </g>
          )
        })}
        <line x1={PAD.l} x2={W - PAD.r} y1={y(0)} y2={y(0)} className="stroke-border" />
      </svg>
    </figure>
  )
}

function QuarterTable({ quarters, currency }: { quarters: Quarter[]; currency: string }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-sm">
        <caption className="sr-only">Reported quarters against the base scenario</caption>
        <thead>
          <tr className="text-muted-foreground border-b text-left text-xs">
            <th scope="col" className="py-1.5 pr-3 font-medium">
              Quarter
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              Revenue
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              Expected
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              vs base
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              EPS
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              Expected
            </th>
            <th scope="col" className="px-2 py-1.5 text-right font-medium">
              vs base
            </th>
            <th scope="col" className="py-1.5 pl-3 font-medium">
              Fair value
            </th>
          </tr>
        </thead>
        <tbody className="num">
          {[...quarters].reverse().map((q) => (
            <tr key={q.period_end} className="border-b last:border-0">
              <th scope="row" className="py-1.5 pr-3 text-left font-normal whitespace-nowrap">
                {label(q)}
                <span className="text-muted-foreground ml-1.5 text-xs">{q.period_end}</span>
              </th>
              <td className="px-2 py-1.5 text-right">{moneyCompact(q.actual_revenue, currency)}</td>
              <td className="text-muted-foreground px-2 py-1.5 text-right">{moneyCompact(q.expected_revenue, currency)}</td>
              <Delta value={q.revenue_delta} status={q.revenue_status} />
              <td className="px-2 py-1.5 text-right">{money(q.actual_eps, currency)}</td>
              <td className="text-muted-foreground px-2 py-1.5 text-right">{money(q.expected_eps, currency)}</td>
              <Delta value={q.eps_delta} status={q.eps_status} />
              <td className="text-muted-foreground py-1.5 pl-3 text-xs">{q.fair_value_as_of ? `DCF of ${q.fair_value_as_of}` : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Delta({ value, status }: { value?: string; status: Quarter['revenue_status'] }) {
  if (value == null || status === 'na') return <td className="text-muted-foreground px-2 py-1.5 text-right">—</td>
  const n = Number(value)
  return (
    <td className={cn('px-2 py-1.5 text-right', status === 'miss' && 'text-breach font-medium')}>
      {n >= 0 ? '+' : ''}
      {pct(value, 1)}
      {status === 'miss' && <span className="ml-1 text-xs">miss</span>}
    </td>
  )
}
