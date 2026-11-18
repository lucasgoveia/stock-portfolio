import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { Bell, Play } from 'lucide-react'
import { toast } from 'sonner'

import { BandGauge, type GaugeState } from '@/components/band-gauge'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime, frac, money, pct } from '@/lib/format'
import { lastJobQuery, openAlertsQuery, portfolioQuery } from '@/lib/queries'

export const Route = createFileRoute('/')({ component: Dashboard })

type Position = Schemas['PortfolioPosition']
type Snapshot = Schemas['Snapshot']

const HARD_FLOOR = 0.015
const HARD_CAP = 0.12

function Dashboard() {
  const snap = useQuery(portfolioQuery)
  return (
    <>
      <PageHeader
        eyebrow={snap.data ? `Portfolio · as of ${snap.data.as_of}` : 'Portfolio'}
        title="Dashboard"
        actions={<RunJobButton />}
      />
      {snap.isPending ? (
        <LoadingBlock rows={6} />
      ) : snap.isError ? (
        <ErrorBlock error={snap.error} />
      ) : (
        <div className="space-y-8">
          <Headline snap={snap.data} />
          <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
            <CashPanel snap={snap.data} />
            <StatusPanel />
          </div>
          <Allocation snap={snap.data} />
        </div>
      )}
    </>
  )
}

function Headline({ snap }: { snap: Snapshot }) {
  return (
    <section className="border-b pb-6">
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-2">
        <div className="display num text-5xl font-semibold sm:text-6xl">{money(snap.total_value)}</div>
        {snap.bootstrap ? (
          <span className="border-brass/50 text-brass rounded-full border px-2.5 py-0.5 text-xs font-medium">
            Bootstrap: bands off until R$ 10.000
          </span>
        ) : (
          <span className="border-jade/50 text-jade rounded-full border px-2.5 py-0.5 text-xs font-medium">Full rules</span>
        )}
      </div>
      <dl className="text-muted-foreground mt-3 flex flex-wrap gap-x-6 gap-y-1 text-sm">
        <Stat label="Stocks" value={money(snap.equity_value)} />
        <Stat label="Cash" value={money(snap.cash_value)} />
        <Stat label="Core budget" value={pct(snap.core_budget, 1)} />
        <Stat
          label="USD/BRL"
          value={`${Number(snap.usdbrl).toFixed(4)}${snap.usdbrl_date ? ` (${snap.usdbrl_date})` : ''}`}
        />
      </dl>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex gap-1.5">
      <dt>{label}</dt>
      <dd className="num text-foreground">{value}</dd>
    </div>
  )
}

function CashPanel({ snap }: { snap: Snapshot }) {
  const weight = frac(snap.cash_weight)
  const target = frac(snap.cash_target)
  const usdShare = frac(snap.cash.usd_share)
  const state: GaugeState = snap.bootstrap
    ? 'neutral'
    : snap.cash_signal === 'ok'
      ? 'ok'
      : 'breach'
  return (
    <section className="bg-card rounded-lg border p-4">
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="eyebrow">Cash</h2>
        <span className="text-muted-foreground text-sm">
          <span className="num text-foreground">{pct(weight, 1)}</span> of portfolio, target{' '}
          <span className="num text-foreground">{pct(target, 1)}</span>
        </span>
      </div>
      <BandGauge
        className="mt-3"
        max={0.25}
        actual={weight}
        target={target}
        lower={0.04}
        upper={0.2}
        state={state}
        label={`Cash ${pct(weight, 1)}, target ${pct(target, 1)}, band 4% to 20%`}
      />
      <div className="text-muted-foreground num mt-1 flex justify-between text-[11px]">
        <span>0%</span>
        <span>band 4–20%</span>
        <span>25%</span>
      </div>
      {snap.cash_signal !== 'ok' && !snap.bootstrap && (
        <p className="text-breach mt-2 text-sm">
          {snap.cash_signal === 'raise' ? 'Cash is below 4%: see suggested trims.' : 'Cash is above 20%: plan a contribution from cash.'}
        </p>
      )}
      <div className="mt-4">
        <div className="bg-muted flex h-2 overflow-hidden rounded-full" aria-hidden>
          <div className="bg-chart-1" style={{ width: `${(1 - usdShare) * 100}%` }} />
          <div className="bg-chart-2" style={{ width: `${usdShare * 100}%` }} />
        </div>
        <dl className="mt-2 grid grid-cols-2 gap-2 text-sm">
          <div>
            <dt className="text-muted-foreground flex items-center gap-1.5">
              <span className="bg-chart-1 size-2 rounded-full" /> BRL
            </dt>
            <dd className="num">{money(snap.cash.brl)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground flex items-center gap-1.5">
              <span className="bg-chart-2 size-2 rounded-full" /> USD
            </dt>
            <dd className="num">
              {money(snap.cash.usd_balance, 'USD')}{' '}
              <span className="text-muted-foreground">≈ {money(snap.cash.usd_in_brl)}</span>
            </dd>
          </div>
        </dl>
        {frac(snap.pending_us) > 0 && (
          <p className="text-muted-foreground mt-2 text-xs">
            Pending US bucket: <span className="num text-foreground">{money(snap.pending_us)}</span>
          </p>
        )}
      </div>
    </section>
  )
}

function StatusPanel() {
  const alerts = useQuery(openAlertsQuery)
  const job = useQuery(lastJobQuery)
  const open = alerts.data?.alerts?.length ?? 0
  return (
    <section className="bg-card grid gap-4 rounded-lg border p-4 sm:grid-cols-2 lg:grid-cols-1">
      <Link to="/alerts" className="group hover:bg-accent -m-2 rounded-md p-2 transition-colors">
        <h2 className="eyebrow">Open alerts</h2>
        <div className="mt-1 flex items-center gap-2">
          <Bell className={open > 0 ? 'text-brass size-5' : 'text-muted-foreground size-5'} />
          <span className="display num text-3xl font-semibold">{alerts.isPending ? '…' : open}</span>
          <span className="text-muted-foreground text-sm group-hover:underline">Open inbox</span>
        </div>
      </Link>
      <div>
        <h2 className="eyebrow">Last daily job</h2>
        {job.isPending ? (
          <p className="mt-1 text-sm">…</p>
        ) : job.isError ? (
          <p className="text-breach mt-1 text-sm">Could not load job status.</p>
        ) : job.data == null ? (
          <p className="text-muted-foreground mt-1 text-sm">Never ran. Run it now to fetch prices.</p>
        ) : (
          <div className="mt-1 text-sm">
            <span
              className={
                job.data.status === 'ok' ? 'text-jade font-medium' : 'text-breach font-medium'
              }
            >
              {job.data.status}
            </span>{' '}
            <span className="num text-muted-foreground">{dateTime(job.data.finished_at ?? job.data.started_at)}</span>
            {job.data.steps?.some((s) => s.status !== 'ok') && (
              <ul className="text-muted-foreground mt-1 space-y-0.5 text-xs">
                {job.data.steps
                  .filter((s) => s.status !== 'ok')
                  .map((s) => (
                    <li key={s.name}>
                      {s.name}: {s.status}
                      {s.error ? ` (${s.error})` : ''}
                    </li>
                  ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </section>
  )
}

function RunJobButton() {
  const qc = useQueryClient()
  const run = useMutation({
    mutationFn: () => unwrap(api.POST('/api/jobs/daily/run')),
    onSuccess: (r) => {
      toast.success(`Daily job finished: ${r.status}`)
      void qc.invalidateQueries()
    },
    onError: (e) => toast.error(`Daily job failed: ${errorMessage(e)}`),
  })
  return (
    <Button variant="outline" onClick={() => run.mutate()} disabled={run.isPending}>
      <Play /> {run.isPending ? 'Running…' : 'Run daily job'}
    </Button>
  )
}

function positionState(p: Position, bootstrap: boolean): GaugeState {
  if (p.thesis_status === 'broken') return 'breach'
  if (bootstrap) return 'neutral'
  if (p.band.above_cap || p.band.above_upper || p.band.below_floor) return 'breach'
  if (p.band.below_lower || p.overvalued) return 'watch'
  return 'ok'
}

function positionNote(p: Position, bootstrap: boolean): string | null {
  const notes: string[] = []
  if (p.thesis_status === 'broken') notes.push('thesis broken')
  if (!bootstrap) {
    if (p.band.above_cap) notes.push('above 12% cap')
    else if (p.band.above_upper) notes.push('above band')
    if (p.band.below_floor) notes.push('below 1.5% floor')
    else if (p.band.below_lower) notes.push('below band')
  }
  if (p.overvalued) notes.push('overvalued')
  if (p.price == null) notes.push('no price')
  if (p.score == null) notes.push('not rated')
  return notes.length ? notes.join(' · ') : null
}

function Allocation({ snap }: { snap: Snapshot }) {
  const holdings = snap.holdings ?? []
  const core = holdings.filter((h) => h.sleeve === 'core')
  const tactical = holdings.filter((h) => h.sleeve !== 'core')
  const peak = Math.max(
    0,
    ...holdings.map((h) => Math.max(frac(h.weight), frac(h.target?.weight), frac(h.target?.upper))),
  )
  // Full rules: a fixed scale that shows the 12% cap. Bootstrap: fit the data.
  const max = snap.bootstrap ? Math.min(1, Math.max(0.12, peak * 1.1)) : 0.14

  return (
    <section>
      <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="display text-xl font-semibold">Allocation</h2>
          <p className="text-muted-foreground text-sm">
            Actual weight against target{snap.bootstrap ? '' : ' and band'}, on a 0–{pct(max, 0)} scale.
          </p>
        </div>
        <Legend bootstrap={snap.bootstrap} />
      </div>
      {holdings.length === 0 ? (
        <div className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
          No holdings yet. Add names to the{' '}
          <Link to="/watchlist" className="text-foreground underline">
            watchlist
          </Link>
          , then plan your first{' '}
          <Link to="/contributions" className="text-foreground underline">
            contribution
          </Link>
          .
        </div>
      ) : (
        <div className="space-y-6">
          <PositionRows title="Core" rows={core} snap={snap} max={max} />
          {tactical.length > 0 && <PositionRows title="Tactical" rows={tactical} snap={snap} max={max} />}
        </div>
      )}
      {(snap.too_few || snap.too_many || frac(snap.undeployed) > 0) && (
        <p className="text-brass mt-3 text-sm">
          {snap.too_many
            ? 'Too many positions: every core target is at the 2% minimum.'
            : `Too few positions: ${pct(snap.undeployed, 1)} of the core budget is undeployed and goes to cash.`}
        </p>
      )}
    </section>
  )
}

function Legend({ bootstrap }: { bootstrap: boolean }) {
  return (
    <ul className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs">
      {!bootstrap && (
        <li className="flex items-center gap-1.5">
          <span className="bg-band inline-block h-3 w-5 rounded-sm" /> band
        </li>
      )}
      <li className="flex items-center gap-1.5">
        <span className="border-t-foreground inline-block size-0 border-x-[5px] border-t-[6px] border-x-transparent" /> target
      </li>
      <li className="flex items-center gap-1.5">
        <span className="bg-jade inline-block h-3 w-1 rounded-full" /> actual
      </li>
      {!bootstrap && (
        <li className="flex items-center gap-1.5">
          <span className="border-breach inline-block h-3 border-l" /> 12% cap
        </li>
      )}
    </ul>
  )
}

function PositionRows({ title, rows, snap, max }: { title: string; rows: Position[]; snap: Snapshot; max: number }) {
  const sorted = [...rows].sort((a, b) => frac(b.weight) - frac(a.weight))
  return (
    <div>
      <h3 className="eyebrow mb-1">{title}</h3>
      <ul className="divide-y border-y">
        {sorted.map((p) => {
          const state = positionState(p, snap.bootstrap)
          const note = positionNote(p, snap.bootstrap)
          const t = p.target
          const showBand = !snap.bootstrap && p.sleeve === 'core' && t
          return (
            <li
              key={p.asset_id}
              className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1 py-2.5 sm:grid-cols-[7rem_minmax(0,1fr)_9rem]"
            >
              <div className="min-w-0">
                <Link
                  to="/assets/$assetId"
                  params={{ assetId: String(p.asset_id) }}
                  className="font-semibold hover:underline"
                >
                  {p.ticker}
                </Link>
                <div className="text-muted-foreground num text-xs">score {p.score ? Number(p.score).toFixed(2) : '—'}</div>
              </div>
              <BandGauge
                max={max}
                actual={frac(p.weight)}
                target={t ? frac(t.weight) : undefined}
                lower={showBand ? frac(t.lower) : undefined}
                upper={showBand ? frac(t.upper) : undefined}
                floor={snap.bootstrap ? undefined : HARD_FLOOR}
                cap={snap.bootstrap ? undefined : HARD_CAP}
                state={state}
                label={`${p.ticker}: ${pct(p.weight)} actual, target ${t ? pct(t.weight) : 'none'}`}
              />
              <div className="num col-span-2 flex items-baseline justify-between gap-2 text-sm sm:col-span-1 sm:block sm:text-right">
                <div>
                  <span className={state === 'breach' ? 'text-breach' : state === 'watch' ? 'text-brass' : ''}>
                    {pct(p.weight)}
                  </span>
                  <span className="text-muted-foreground"> / {t ? pct(t.weight) : '—'}</span>
                </div>
                <div className="text-muted-foreground text-xs">{money(p.value_brl)}</div>
              </div>
              {note && <div className="text-muted-foreground col-span-full text-xs sm:col-start-2">{note}</div>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
