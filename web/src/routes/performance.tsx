import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useCallback, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { money, pct } from '@/lib/format'
import { cn } from '@/lib/utils'

type Range = '3m' | '6m' | '1y' | 'ytd' | 'all'
const RANGES: { id: Range; label: string }[] = [
  { id: '3m', label: '3M' },
  { id: '6m', label: '6M' },
  { id: 'ytd', label: 'YTD' },
  { id: '1y', label: '1Y' },
  { id: 'all', label: 'All' },
]

export const Route = createFileRoute('/performance')({
  validateSearch: (s: Record<string, unknown>): { range?: Range } => ({
    range: RANGES.some((r) => r.id === s.range) ? (s.range as Range) : undefined,
  }),
  component: Page,
})

function rangeFrom(r: Range): string {
  const d = new Date()
  if (r === 'all') return '2000-01-01'
  if (r === 'ytd') return `${d.getFullYear()}-01-01`
  d.setMonth(d.getMonth() - (r === '3m' ? 3 : r === '6m' ? 6 : 12))
  return d.toISOString().slice(0, 10)
}

const SERIES = [
  { key: 'total', label: 'Portfolio', short: 'Portfolio', color: 'var(--series-1)' },
  { key: 'cdi', label: 'CDI', short: 'CDI', color: 'var(--series-2)' },
  { key: 'ibov', label: 'IBOV', short: 'IBOV', color: 'var(--series-3)' },
  { key: 'sp500_brl', label: 'S&P 500 (BRL)', short: 'S&P 500', color: 'var(--series-4)' },
] as const
type SeriesKey = (typeof SERIES)[number]['key']

function Page() {
  const { range = '1y' } = Route.useSearch()
  const navigate = useNavigate({ from: '/performance' })
  const report = useQuery({
    queryKey: ['performance', range],
    queryFn: () => unwrap(api.GET('/api/performance', { params: { query: { from: rangeFrom(range) } } })),
  })
  return (
    <>
      <PageHeader
        title="Performance"
        description="Time-weighted return: contributions and withdrawals do not count as gains, dividends do. Benchmarks rebased to the same start."
        actions={<BackfillButton />}
      />
      <div role="tablist" aria-label="Period" className="bg-muted mb-5 inline-flex rounded-lg p-1">
        {RANGES.map((r) => (
          <button
            key={r.id}
            role="tab"
            aria-selected={range === r.id}
            onClick={() => navigate({ search: { range: r.id === '1y' ? undefined : r.id } })}
            className={cn(
              'rounded-md px-3 py-1 text-sm transition-colors',
              range === r.id ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {r.label}
          </button>
        ))}
      </div>
      {report.isPending ? (
        <LoadingBlock rows={6} />
      ) : report.isError ? (
        <ErrorBlock error={report.error} />
      ) : (report.data.series ?? []).length < 2 ? (
        <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
          Not enough history yet. Snapshots are written by the daily job; use “Rebuild history” to compute past days from
          your trades, cash and stored prices.
        </p>
      ) : (
        <div className="space-y-6">
          <Headline r={report.data} />
          <ReturnChart series={report.data.series ?? []} />
          <Segments segments={report.data.segments ?? []} />
        </div>
      )}
    </>
  )
}

function BackfillButton() {
  const qc = useQueryClient()
  const run = useMutation({
    mutationFn: () => unwrap(api.POST('/api/performance/backfill')),
    onSuccess: (r) => {
      toast.success(`Rebuilt ${r.days} days of history`)
      void qc.invalidateQueries({ queryKey: ['performance'] })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <Button variant="outline" disabled={run.isPending} onClick={() => run.mutate()}>
      {run.isPending ? 'Rebuilding…' : 'Rebuild history'}
    </Button>
  )
}

const signed = (v?: string | null) => (v == null ? '—' : `${Number(v) >= 0 ? '+' : ''}${pct(v, 2)}`)

function Headline({ r }: { r: Schemas['Report'] }) {
  const total = r.segments?.find((s) => s.segment === 'total')
  return (
    <section className="flex flex-wrap items-end gap-x-8 gap-y-3 border-b pb-5">
      <div>
        <div className="eyebrow">Portfolio, time-weighted</div>
        <div className="display num text-5xl font-semibold">{signed(total?.twr)}</div>
        <div className="text-muted-foreground num text-xs">
          {r.series?.[0]?.date} → {r.series?.[(r.series?.length ?? 1) - 1]?.date}
        </div>
      </div>
      <dl className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
        {(
          [
            ['CDI', r.benchmarks.cdi],
            ['IBOV', r.benchmarks.ibov],
            ['S&P 500 (BRL)', r.benchmarks.sp500_brl],
          ] as const
        ).map(([k, v]) => (
          <div key={k}>
            <dt className="text-muted-foreground text-xs">{k}</dt>
            <dd className="num">{signed(v)}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

type Point = Schemas['SeriesPoint']
const H = 280
const PAD = { l: 48, r: 92, t: 12, b: 28 }

/** Tracks an element's width so the SVG draws at real pixels (legible text on phones). */
function useWidth(fallback: number) {
  const [w, setW] = useState(fallback)
  const ro = useRef<ResizeObserver | null>(null)
  const ref = useCallback((el: HTMLDivElement | null) => {
    ro.current?.disconnect()
    if (!el) return
    setW(el.clientWidth || fallback)
    ro.current = new ResizeObserver(([e]) => setW(Math.round(e?.contentRect.width ?? fallback)))
    ro.current.observe(el)
  }, [fallback])
  return [w, ref] as const
}

function ReturnChart({ series }: { series: Point[] }) {
  const [hover, setHover] = useState<number | null>(null)
  const [table, setTable] = useState(false)
  const svgRef = useRef<SVGSVGElement>(null)
  const [width, wrapRef] = useWidth(720)
  const W = Math.max(300, width)
  const { lines, yTicks, xTicks, x, y } = useMemo(() => {
    const vals = series.flatMap((p) => SERIES.map((s) => p[s.key as SeriesKey]).filter((v): v is string => v != null).map(Number))
    let lo = Math.min(...vals, 1)
    let hi = Math.max(...vals, 1)
    const span = hi - lo || 0.02
    lo -= span * 0.08
    hi += span * 0.08
    const x = (i: number) => PAD.l + (i / Math.max(series.length - 1, 1)) * (W - PAD.l - PAD.r)
    const y = (v: number) => PAD.t + (1 - (v - lo) / (hi - lo)) * (H - PAD.t - PAD.b)
    const lines = SERIES.map((s) => {
      let d = ''
      let last: { i: number; v: number } | null = null
      series.forEach((p, i) => {
        const raw = p[s.key as SeriesKey]
        if (raw == null) return
        const v = Number(raw)
        d += `${d ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`
        last = { i, v }
      })
      return { ...s, d, last: last as { i: number; v: number } | null }
    })
    const step = niceStep((hi - lo) / 4)
    const yTicks: number[] = []
    for (let v = Math.ceil((lo - 1) / step) * step; v <= hi - 1; v += step) yTicks.push(1 + v)
    const n = series.length
    const xTicks = (W < 500 ? [0, Math.floor(n / 2), n - 1] : [0, Math.floor(n / 3), Math.floor((2 * n) / 3), n - 1]).filter(
      (v, i, a) => a.indexOf(v) === i,
    )
    return { lines, yTicks, xTicks, x, y }
  }, [series, W])

  // Spread end labels so they never collide.
  const labels = useMemo(() => {
    const ls = lines.filter((l) => l.last).map((l) => ({ key: l.key, label: l.short, color: l.color, v: l.last!.v, y: y(l.last!.v) }))
    ls.sort((a, b) => a.y - b.y)
    for (let i = 1; i < ls.length; i++) {
      const cur = ls[i]!
      const prev = ls[i - 1]!
      if (cur.y - prev.y < 14) cur.y = prev.y + 14
    }
    return ls
  }, [lines, y])

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const svg = svgRef.current
    if (!svg) return
    const rect = svg.getBoundingClientRect()
    const px = ((e.clientX - rect.left) / rect.width) * W
    const i = Math.round(((px - PAD.l) / (W - PAD.l - PAD.r)) * (series.length - 1))
    setHover(Math.max(0, Math.min(series.length - 1, i)))
  }
  const hp = hover != null ? series[hover] : null

  return (
    <section className="bg-card rounded-lg border p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs" aria-label="Legend">
          {SERIES.map((s) => (
            <li key={s.key} className="text-muted-foreground flex items-center gap-1.5">
              <span className="inline-block h-0.5 w-4 rounded-full" style={{ background: s.color }} />
              {s.label}
            </li>
          ))}
        </ul>
        <Button size="xs" variant="ghost" onClick={() => setTable((v) => !v)} aria-pressed={table}>
          {table ? 'Show chart' : 'Show table'}
        </Button>
      </div>
      {table ? (
        <div className="max-h-96 overflow-auto">
          <table className="num w-full text-xs">
            <thead className="bg-card sticky top-0">
              <tr className="text-muted-foreground">
                <th className="py-1 text-left font-medium">Date</th>
                {SERIES.map((s) => (
                  <th key={s.key} className="py-1 text-right font-medium">
                    {s.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[...series].reverse().map((p) => (
                <tr key={p.date} className="border-t">
                  <td className="py-1">{p.date}</td>
                  {SERIES.map((s) => (
                    <td key={s.key} className="py-1 text-right">
                      {signed(p[s.key as SeriesKey] == null ? null : String(Number(p[s.key as SeriesKey]) - 1))}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="relative" ref={wrapRef}>
          <svg
            ref={svgRef}
            viewBox={`0 0 ${W} ${H}`}
            width={W}
            height={H}
            className="block max-w-full touch-none select-none"
            role="img"
            aria-label="Cumulative return of the portfolio and benchmarks"
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
          >
            {yTicks.map((v) => (
              <g key={v}>
                <line x1={PAD.l} x2={W - PAD.r} y1={y(v)} y2={y(v)} className="stroke-border" strokeWidth={v === 1 ? 1.5 : 1} />
                <text x={PAD.l - 6} y={y(v) + 3} textAnchor="end" className="fill-muted-foreground num text-[10px]">
                  {`${v >= 1 ? '+' : ''}${((v - 1) * 100).toFixed(Math.abs(v - 1) < 0.1 ? 1 : 0)}%`}
                </text>
              </g>
            ))}
            {xTicks.map((i) => (
              <text key={i} x={x(i)} y={H - 8} textAnchor="middle" className="fill-muted-foreground num text-[10px]">
                {tickLabel(series[i]?.date, series.length)}
              </text>
            ))}
            {lines.map((l) => (
              <path
                key={l.key}
                d={l.d}
                fill="none"
                stroke={l.color}
                strokeWidth={l.key === 'total' ? 2.5 : 2}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            ))}
            {labels.map((l) => (
              <text key={l.key} x={W - PAD.r + 6} y={l.y + 3} className="fill-foreground text-[10px]">
                <tspan>{l.label}</tspan>
                <tspan className="fill-muted-foreground num"> {`${l.v >= 1 ? '+' : ''}${((l.v - 1) * 100).toFixed(1)}%`}</tspan>
              </text>
            ))}
            {hover != null && (
              <g>
                <line x1={x(hover)} x2={x(hover)} y1={PAD.t} y2={H - PAD.b} className="stroke-muted-foreground" strokeDasharray="3 3" />
                {lines.map((l) => {
                  const raw = series[hover]?.[l.key as SeriesKey]
                  if (raw == null) return null
                  return (
                    <circle key={l.key} cx={x(hover)} cy={y(Number(raw))} r={4} fill={l.color} className="stroke-card" strokeWidth={2} />
                  )
                })}
              </g>
            )}
          </svg>
          {hp && hover != null && (
            <div
              className="bg-popover text-popover-foreground pointer-events-none absolute top-2 rounded-md border px-2.5 py-1.5 text-xs shadow-sm"
              style={hover > series.length / 2 ? { left: 8 } : { right: 8 }}
            >
              <div className="num mb-1 font-medium">{hp.date}</div>
              {SERIES.map((s) => (
                <div key={s.key} className="flex items-center justify-between gap-4">
                  <span className="text-muted-foreground flex items-center gap-1.5">
                    <span className="inline-block size-2 rounded-full" style={{ background: s.color }} />
                    {s.label}
                  </span>
                  <span className="num">
                    {signed(hp[s.key as SeriesKey] == null ? null : String(Number(hp[s.key as SeriesKey]) - 1))}
                  </span>
                </div>
              ))}
              <div className="text-muted-foreground num mt-1 border-t pt-1">value {money(hp.value)}</div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

/** dd/mm for short ranges, mm/yy for long ones. */
function tickLabel(date: string | undefined, n: number): string {
  if (!date) return ''
  const [y, m, d] = date.split('-')
  return n > 130 ? `${m}/${y?.slice(2)}` : `${d}/${m}`
}

function niceStep(raw: number): number {
  const p = Math.pow(10, Math.floor(Math.log10(raw)))
  const m = raw / p
  return (m <= 1 ? 1 : m <= 2 ? 2 : m <= 5 ? 5 : 10) * p
}

function Segments({ segments }: { segments: Schemas['Segment'][] }) {
  const groups = segments.filter((s) => !s.segment.startsWith('asset:'))
  const assets = segments.filter((s) => s.segment.startsWith('asset:'))
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <SegmentTable title="By sleeve and market" rows={groups} />
      <SegmentTable title="By position" rows={assets} />
    </div>
  )
}

function SegmentTable({ title, rows }: { title: string; rows: Schemas['Segment'][] }) {
  if (rows.length === 0) return null
  return (
    <section className="bg-card rounded-lg border p-4">
      <h2 className="eyebrow mb-2">{title}</h2>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-muted-foreground text-xs">
            <th className="py-1 text-left font-medium" />
            <th className="py-1 text-right font-medium">Value</th>
            <th className="py-1 text-right font-medium">Net flows</th>
            <th className="py-1 text-right font-medium">TWR</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((s) => (
            <tr key={s.segment} className="border-t">
              <td className="py-1.5">{s.label}</td>
              <td className="num py-1.5 text-right">{money(s.end_value)}</td>
              <td className="num text-muted-foreground py-1.5 text-right">{money(s.net_flows)}</td>
              <td className={cn('num py-1.5 text-right font-medium', Number(s.twr) < 0 ? 'text-breach' : '')}>{signed(s.twr)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
