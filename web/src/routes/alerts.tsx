import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { BellOff, Check, CheckCheck, Clock } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime, humanize, money, pct, qty } from '@/lib/format'
import { assetsQuery, qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Alert = Schemas['Alert']
type State = 'open' | 'snoozed' | 'acknowledged' | 'resolved'
const STATES: State[] = ['open', 'snoozed', 'acknowledged', 'resolved']

interface Search {
  state?: State
  type?: string
  asset?: number
}

export const Route = createFileRoute('/alerts')({
  validateSearch: (s: Record<string, unknown>): Search => ({
    state: STATES.includes(s.state as State) && s.state !== 'open' ? (s.state as State) : undefined,
    type: typeof s.type === 'string' && s.type ? s.type : undefined,
    asset: Number(s.asset) > 0 ? Number(s.asset) : undefined,
  }),
  component: AlertsPage,
})

// Severity decides group order: things that need a sell or a fix come first.
const SEVERITY: Record<string, number> = {
  thesis_broken: 0,
  tactical_stop_hit: 0.5,
  tactical_target_hit: 0.6,
  tactical_expired: 0.7,
  raise_cash: 1,
  overweight: 2,
  overvalued: 3,
  below_floor: 4,
  better_opportunity: 5,
  data_fetch_failed: 6,
  missing_fair_value: 7,
  fair_value_stale: 8,
  thesis_review_due: 9,
  expected_vs_actual_miss: 8.5,
  opportunity: 10,
  deploy_cash: 11,
  tactical_count: 12,
  tax_pool_conflict: 4.5,
  price_jump_unexplained: 1.5,
  corporate_action_pending: 1.6,
}

const SEVERE = new Set(['price_jump_unexplained', 'tactical_stop_hit', 'thesis_broken', 'raise_cash', 'overweight', 'below_floor', 'data_fetch_failed'])

function AlertsPage() {
  const search = Route.useSearch()
  const state: State = search.state ?? 'open'
  const navigate = useNavigate({ from: '/alerts' })
  const assets = useQuery(assetsQuery)
  const list = useQuery({
    queryKey: [...qk.alerts, state, search.type ?? '', search.asset ?? 0],
    queryFn: () =>
      unwrap(
        api.GET('/api/alerts', {
          params: { query: { state: state, type: search.type, asset_id: search.asset } },
        }),
      ),
  })
  const tickers = new Map((assets.data ?? []).map((a) => [a.id, a.ticker]))
  const alerts = list.data?.alerts ?? []
  const groups = groupByType(alerts)
  const setSearch = (patch: Partial<Search>) => navigate({ search: (prev) => ({ ...prev, ...patch }) })

  return (
    <>
      <PageHeader
        title="Alerts"
        description="Every rule breach lands here. Act on it, or acknowledge or snooze it until it matters again."
      />
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div role="tablist" aria-label="Alert state" className="bg-muted inline-flex w-fit rounded-lg p-1">
          {STATES.map((s) => (
            <button
              key={s}
              role="tab"
              aria-selected={state === s}
              onClick={() => setSearch({ state: s === 'open' ? undefined : s })}
              className={cn(
                'rounded-md px-3 py-1 text-sm capitalize transition-colors',
                state === s ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {s}
            </button>
          ))}
        </div>
        <div className="flex gap-2">
          <Select
            value={search.type ?? null}
            onValueChange={(v) => setSearch({ type: v ?? undefined })}
          >
            <SelectTrigger className="w-44" aria-label="Filter by type">
              <SelectValue>{(v: string | null) => (v ? humanize(v) : 'All types')}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={null}>All types</SelectItem>
              {Object.keys(SEVERITY).map((t) => (
                <SelectItem key={t} value={t}>
                  {humanize(t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={search.asset ?? null}
            onValueChange={(v) => setSearch({ asset: v ?? undefined })}
          >
            <SelectTrigger className="w-36" aria-label="Filter by asset">
              <SelectValue>{(v: number | null) => (v ? (tickers.get(v) ?? `#${v}`) : 'All assets')}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={null}>All assets</SelectItem>
              {(assets.data ?? []).map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.ticker}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {list.isPending ? (
        <LoadingBlock rows={5} />
      ) : list.isError ? (
        <ErrorBlock error={list.error} />
      ) : alerts.length === 0 ? (
        <div className="text-muted-foreground rounded-lg border border-dashed p-8 text-center text-sm">
          {state === 'open'
            ? 'No open alerts. Every rule is satisfied.'
            : `No ${state} alerts for this filter.`}
        </div>
      ) : (
        <div className="space-y-8">
          {groups.map(([type, items]) => (
            <section key={type}>
              <h2 className="mb-2 flex items-center gap-2">
                <span className={cn('size-2 rounded-full', SEVERE.has(type) ? 'bg-breach' : 'bg-brass')} />
                <span className="display text-lg font-semibold">{humanize(type)}</span>
                <span className="num text-muted-foreground text-sm">{items.length}</span>
              </h2>
              <ul className="bg-card divide-y rounded-lg border">
                {items.map((a) => (
                  <AlertRow key={a.id} alert={a} ticker={a.asset_id != null ? tickers.get(a.asset_id) : undefined} />
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </>
  )
}

function groupByType(alerts: Alert[]): [string, Alert[]][] {
  const m = new Map<string, Alert[]>()
  for (const a of alerts) m.set(a.type, [...(m.get(a.type) ?? []), a])
  return [...m.entries()].sort(([a], [b]) => (SEVERITY[a] ?? 99) - (SEVERITY[b] ?? 99) || a.localeCompare(b))
}

type Payload = Record<string, unknown>
type Suggestion = Schemas['TrimSuggestion']

const str = (v: unknown) => (typeof v === 'string' ? v : v == null ? '' : String(v))

function payloadOf(a: Alert): Payload {
  return a.payload && typeof a.payload === 'object' ? (a.payload as Payload) : {}
}

/** One sentence saying what the alert means, from its payload. */
function describe(a: Alert): string {
  const p = payloadOf(a)
  const sug = p.suggestion as Suggestion | undefined
  switch (a.type) {
    case 'overweight':
    case 'overvalued': {
      if (!sug) return ''
      const tax = p.tax as Schemas['TrimTax'] | undefined
      const base = `Trim ${qty(sug.quantity)} shares at about ${money(sug.price)} (${money(sug.amount)}).`
      if (!tax?.exceeds_exemption) return base
      return `${base} Selling it all this month passes the R$ 20.000 exemption: about ${money(tax.estimated_tax)} in tax. ${qty(tax.fit_quantity)} shares fit this month; sell the rest next month to stay exempt.`
    }
    case 'better_opportunity':
      return sug
        ? `${str(p.candidate)} scores and prices better. ${sug.full_exit ? 'Exit' : 'Trim'} ${qty(sug.quantity)} shares (${money(sug.amount)}) to fund it.`
        : ''
    case 'thesis_broken':
      return str(p.message) || 'Thesis is broken: consider a full exit.'
    case 'thesis_review_due':
      return `Thesis review was due on ${str(p.next_review_on)}.`
    case 'expected_vs_actual_miss':
      return expectedMissText(p)
    case 'fair_value_stale':
      return `Fair value is from ${str(p.as_of)}, older than 12 months. The buy gate fails until it is refreshed.`
    case 'missing_fair_value':
      return 'No fair value set. It counts as not overvalued and cannot pass the buy gate.'
    case 'raise_cash':
      return `Cash is ${pct(str(p.cash_weight), 1)} against a ${pct(str(p.cash_target), 1)} target.`
    case 'deploy_cash':
      return `Cash is ${pct(str(p.cash_weight), 1)}, above the 20% band. Plan a contribution from cash.`
    case 'below_floor':
      return `Weight is ${pct(str(p.weight))}, below the 1.5% floor. Top up or exit.`
    case 'opportunity':
      return p.catalyst_only
        ? 'Flagged by a research note catalyst.'
        : `Trading ${pct(str(p.discount), 1)} below fair value.`
    case 'undeployed_budget':
      return `${pct(str(p.undeployed), 1)} of the core budget has no position to go to. Add watchlist names.`
    case 'too_many_positions':
      return `${str(p.holdings)} holdings: every core target sits at the 2% minimum.`
    case 'bootstrap_exit':
      return `Portfolio passed ${money(str(p.threshold))}. Bands, floors and caps now apply.`
    case 'tactical_target_hit':
      return `Price ${money(str(p.price))} reached the target ${money(str(p.target_price))}. Decide the exit.`
    case 'tactical_stop_hit':
      return `Price ${money(str(p.price))} reached the stop ${money(str(p.stop_price))}. Decide the exit.`
    case 'tactical_expired':
      return `The setup expired on ${str(p.expires_on)} without the catalyst. Exit, or extend the setup.`
    case 'tactical_count':
      return `${str(p.open)} open tactical positions; the rules allow ${str(p.max)}.`
    case 'price_jump_unexplained':
      return `Raw close moved from ${money(str(p.before))} to ${money(str(p.after))} on ${str(p.date)} (ratio ${str(p.ratio)}) with no split or bonus on record. Enter the event if there was one.`
    case 'corporate_action_pending':
      return `${str(p.count)} announced event(s) wait for confirmation before the position changes.`
    case 'tax_pool_conflict':
      return 'Tactical exits this month used exemption room the core trims need. Delay the trims to next month or accept the tax.'
    case 'data_fetch_failed':
      return `Step "${str(p.step)}" failed: ${str(p.error)}`
    default:
      return ''
  }
}

/** The streak that tripped the alert plus the latest quarter's gaps to the base scenario. */
function expectedMissText(p: Payload): string {
  const quarters = (p.quarters as Schemas['QuarterComparison'][] | undefined) ?? []
  const last = quarters[quarters.length - 1]
  const streaks = [
    Number(p.revenue_miss_streak) >= Number(p.required_quarters) ? `revenue ${str(p.revenue_miss_streak)}` : '',
    Number(p.eps_miss_streak) >= Number(p.required_quarters) ? `EPS ${str(p.eps_miss_streak)}` : '',
  ].filter(Boolean)
  const gaps = last
    ? ` Latest quarter (${last.period_end}): revenue ${signedPct(last.revenue_delta)}, EPS ${signedPct(last.eps_delta)} against the base scenario.`
    : ''
  return `Below the base scenario in a row: ${streaks.join(' and ')} quarters.${gaps} Review the thesis and the fair value.`
}

function signedPct(v: string | undefined): string {
  if (v == null || v === '') return 'n/a'
  return `${Number(v) >= 0 ? '+' : ''}${pct(v, 1)}`
}

const SNOOZE: { label: string; days: number }[] = [
  { label: '1 day', days: 1 },
  { label: '1 week', days: 7 },
  { label: '1 month', days: 30 },
]

function AlertRow({ alert: a, ticker }: { alert: Alert; ticker?: string }) {
  const qc = useQueryClient()
  const p = payloadOf(a)
  const name = ticker ?? (str(p.ticker) || null)
  const done = () => {
    void qc.invalidateQueries({ queryKey: qk.alerts })
  }
  const ack = useMutation({
    mutationFn: () => unwrap(api.POST('/api/alerts/{id}/ack', { params: { path: { id: a.id } } })),
    onSuccess: () => {
      toast.success('Alert acknowledged')
      done()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const resolve = useMutation({
    mutationFn: () => unwrap(api.POST('/api/alerts/{id}/resolve', { params: { path: { id: a.id } } })),
    onSuccess: () => {
      toast.success('Alert resolved')
      done()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const snooze = useMutation({
    mutationFn: (days: number) =>
      unwrap(
        api.POST('/api/alerts/{id}/snooze', {
          params: { path: { id: a.id } },
          body: { until: new Date(Date.now() + days * 86_400_000).toISOString() },
        }),
      ),
    onSuccess: (r) => {
      toast.success(`Snoozed until ${dateTime(r.snoozed_until)}`)
      done()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const busy = ack.isPending || resolve.isPending || snooze.isPending
  const closed = a.state === 'resolved'

  return (
    <li className="flex flex-col gap-3 p-3 sm:flex-row sm:items-start sm:justify-between sm:p-4">
      <div className="min-w-0 space-y-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          {name && a.asset_id != null ? (
            <Link to="/assets/$assetId" params={{ assetId: String(a.asset_id) }} className="font-semibold hover:underline">
              {name}
            </Link>
          ) : (
            <span className="font-semibold">{name ?? 'Portfolio'}</span>
          )}
          <span className="num text-muted-foreground text-xs">
            since {dateTime(a.created_at)}
            {a.snoozed_until && a.state === 'snoozed' ? ` · snoozed until ${dateTime(a.snoozed_until)}` : ''}
          </span>
        </div>
        <p className="text-sm break-words [overflow-wrap:anywhere]">{describe(a)}</p>
        <RaiseCashTrims alert={a} />
        <AlertAction alert={a} />
      </div>
      {!closed && (
        <div className="flex shrink-0 gap-1">
          {a.state !== 'acknowledged' && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => ack.mutate()}>
              <Check /> Acknowledge
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button size="sm" variant="ghost" disabled={busy} />}>
              <Clock /> Snooze
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {SNOOZE.map((s) => (
                <DropdownMenuItem key={s.days} onClick={() => snooze.mutate(s.days)}>
                  <BellOff /> {s.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => resolve.mutate()}>
            <CheckCheck /> Resolve
          </Button>
        </div>
      )}
    </li>
  )
}

const actionClass =
  'inline-flex items-center rounded-md border px-2.5 py-1 text-xs font-medium hover:bg-accent transition-colors'

/** The next step for the alert: a prefilled trade, a plan, or the asset page section to fix. */
function AlertAction({ alert: a }: { alert: Alert }) {
  const p = payloadOf(a)
  const sug = p.suggestion as Suggestion | undefined
  const id = a.asset_id
  switch (a.type) {
    case 'overweight':
    case 'overvalued':
    case 'better_opportunity':
      if (!sug || id == null) return null
      return (
        <Link
          to="/trades"
          search={{ asset: id, side: 'sell', quantity: sug.quantity, price: sug.price }}
          className={actionClass}
        >
          Record sell of {qty(sug.quantity)}
        </Link>
      )
    case 'thesis_broken':
    case 'tactical_target_hit':
    case 'tactical_stop_hit':
    case 'tactical_expired':
      if (id == null) return null
      return (
        <Link to="/trades" search={{ asset: id, side: 'sell', full: true }} className={actionClass}>
          Record full exit
        </Link>
      )
    case 'below_floor':
      if (id == null) return null
      return (
        <Link to="/trades" search={{ asset: id, side: 'buy' }} className={actionClass}>
          Record top-up buy
        </Link>
      )
    case 'deploy_cash':
    case 'undeployed_budget':
    case 'opportunity':
    case 'bootstrap_exit':
      return (
        <Link to="/contributions" className={actionClass}>
          Plan a contribution
        </Link>
      )
    case 'price_jump_unexplained':
    case 'corporate_action_pending':
      return (
        <Link to="/trades" search={{ tab: 'actions' }} className={actionClass}>
          Review corporate actions
        </Link>
      )
    case 'missing_fair_value':
    case 'fair_value_stale':
      if (id == null) return null
      return (
        <Link to="/assets/$assetId" params={{ assetId: String(id) }} search={{ tab: 'valuation' }} className={actionClass}>
          Set fair value
        </Link>
      )
    case 'thesis_review_due':
    case 'expected_vs_actual_miss':
      if (id == null) return null
      return (
        <Link to="/assets/$assetId" params={{ assetId: String(id) }} search={{ tab: 'thesis' }} className={actionClass}>
          Review thesis
        </Link>
      )
    default:
      return null
  }
}

function RaiseCashTrims({ alert: a }: { alert: Alert }) {
  if (a.type !== 'raise_cash') return null
  const p = payloadOf(a)
  const trims = (p.trims as Suggestion[] | null) ?? []
  const tk = (p.tickers as Record<string, string> | undefined) ?? {}
  if (trims.length === 0) return null
  return (
    <ul className="space-y-1 text-sm">
      {trims.map((t) => (
        <li key={t.asset_id} className="flex flex-wrap items-center gap-2">
          <span className="num">
            {tk[t.asset_id] ?? `#${t.asset_id}`}: sell {qty(t.quantity)} ({money(t.amount)})
          </span>
          <Link
            to="/trades"
            search={{ asset: Number(t.asset_id), side: 'sell', quantity: t.quantity, price: t.price }}
            className={actionClass}
          >
            Record sell
          </Link>
        </li>
      ))}
    </ul>
  )
}
