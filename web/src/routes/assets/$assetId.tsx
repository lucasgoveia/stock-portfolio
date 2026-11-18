import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'

import { FundamentalsTab } from '@/components/asset/fundamentals-tab'
import { GateChecklist } from '@/components/asset/gate-checklist'
import { NotesTab } from '@/components/asset/notes-tab'
import { Panel } from '@/components/asset/panel'
import { RatingsTab } from '@/components/asset/ratings-tab'
import { TacticalPanel } from '@/components/asset/tactical-panel'
import { ThesisTab } from '@/components/asset/thesis-tab'
import { ValuationTab } from '@/components/asset/valuation-tab'
import { NativeSelect } from '@/components/form'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { TradeForm } from '@/components/trade-form'
import { TradeRow } from '@/components/trade-row'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, ApiError, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { humanize, money, pct, qty } from '@/lib/format'
import { portfolioQuery, qk } from '@/lib/queries'
import { parseAssetSearch, type AssetTab } from '@/lib/search'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/assets/$assetId')({ validateSearch: parseAssetSearch, component: AssetPage })

const TABS: { id: AssetTab; label: string }[] = [
  { id: 'overview', label: 'Overview' },
  { id: 'ratings', label: 'Ratings' },
  { id: 'thesis', label: 'Thesis' },
  { id: 'valuation', label: 'Fair value' },
  { id: 'fundamentals', label: 'Fundamentals' },
  { id: 'notes', label: 'Research' },
  { id: 'trades', label: 'Trades' },
]

function AssetPage() {
  const { assetId } = Route.useParams()
  const id = Number(assetId)
  const { tab = 'overview' } = Route.useSearch()
  const navigate = useNavigate({ from: '/assets/$assetId' })
  const asset = useQuery({
    queryKey: [...qk.asset(id), 'asset'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}', { params: { path: { id } } })),
    enabled: id > 0,
  })
  const snap = useQuery(portfolioQuery)
  const position = [...(snap.data?.holdings ?? []), ...(snap.data?.watchlist ?? [])].find((p) => p.asset_id === id)

  if (asset.isPending) return <LoadingBlock rows={6} />
  if (asset.isError) {
    if (asset.error instanceof ApiError && asset.error.status === 404) {
      return (
        <p className="text-muted-foreground text-sm">
          There is no asset #{assetId}.{' '}
          <Link to="/assets" className="text-foreground underline">
            See all assets
          </Link>
          .
        </p>
      )
    }
    return <ErrorBlock error={asset.error} />
  }
  const a = asset.data
  return (
    <>
      <PageHeader
        eyebrow={
          <Link to={a.status === 'watchlist' ? '/watchlist' : '/assets'} className="hover:underline">
            {a.status === 'watchlist' ? 'Watchlist' : 'Assets'} · {a.market} · {a.kind}
          </Link>
        }
        title={a.ticker}
        description={a.name || undefined}
        actions={<AssetControls asset={a} />}
      />
      <KeyFigures asset={a} position={position} />
      <div
        role="tablist"
        aria-label="Asset sections"
        className="mb-5 flex gap-1 overflow-x-auto border-b [scrollbar-width:none]"
      >
        {TABS.map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => navigate({ search: { tab: t.id === 'overview' ? undefined : t.id } })}
            className={cn(
              '-mb-px shrink-0 border-b-2 px-3 py-2 text-sm transition-colors',
              tab === t.id
                ? 'border-foreground text-foreground font-medium'
                : 'text-muted-foreground hover:text-foreground border-transparent',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'overview' && <Overview asset={a} position={position} snapshotError={snap.error} />}
      {tab === 'ratings' && <RatingsTab assetId={id} />}
      {tab === 'thesis' && <ThesisTab assetId={id} />}
      {tab === 'valuation' && <ValuationTab assetId={id} currency={a.currency} price={position?.price} />}
      {tab === 'fundamentals' && <FundamentalsTab asset={a} />}
      {tab === 'notes' && <NotesTab assetId={id} />}
      {tab === 'trades' && <TradesSection asset={a} />}
    </>
  )
}

type Asset = Schemas['Asset']
type Position = Schemas['PortfolioPosition']

function AssetControls({ asset: a }: { asset: Asset }) {
  const qc = useQueryClient()
  const done = (msg: string) => {
    toast.success(msg)
    void qc.invalidateQueries()
  }
  const sleeve = useMutation({
    mutationFn: (s: string) => unwrap(api.PATCH('/api/assets/{id}', { params: { path: { id: a.id } }, body: { sleeve: s } })),
    onSuccess: (r) => done(`Moved to the ${r.sleeve} sleeve`),
    onError: (e) => toast.error(errorMessage(e)),
  })
  const toWatchlist = useMutation({
    mutationFn: () =>
      unwrap(api.POST('/api/assets/{id}/status', { params: { path: { id: a.id } }, body: { status: 'watchlist' } })),
    onSuccess: () => done('Back on the watchlist'),
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <>
      <span
        className={cn(
          'self-center rounded-full border px-2.5 py-0.5 text-xs font-medium',
          a.status === 'holding' ? 'border-jade/50 text-jade' : 'text-muted-foreground',
        )}
      >
        {humanize(a.status)}
      </span>
      <NativeSelect
        aria-label="Sleeve"
        className="w-auto"
        value={a.sleeve}
        disabled={sleeve.isPending}
        onChange={(e) => {
          const next = e.target.value
          const snap = qc.getQueryData<Schemas['Snapshot']>(qk.portfolio)
          if (next === 'tactical' && a.status === 'holding' && snap && snap.tactical_open >= snap.tactical_max_open) {
            toast.warning(
              `This makes ${snap.tactical_open + 1} open tactical positions; the rules allow ${snap.tactical_max_open}.`,
            )
          }
          sleeve.mutate(next)
        }}
      >
        <option value="core">Core</option>
        <option value="tactical">Tactical</option>
      </NativeSelect>
      {a.status === 'exited' && (
        <Button size="sm" variant="outline" disabled={toWatchlist.isPending} onClick={() => toWatchlist.mutate()}>
          Return to watchlist
        </Button>
      )}
    </>
  )
}

function KeyFigures({ asset: a, position: p }: { asset: Asset; position?: Position }) {
  const items: [string, string][] = [
    ['Price', p?.price ? money(p.price, a.currency) : '—'],
    p?.tactical
      ? ['Exit target', money(p.tactical.setup.target_price, a.currency)]
      : ['Fair value', p?.fair_value ? money(p.fair_value, a.currency) : '—'],
    ['Score', p?.score ? Number(p.score).toFixed(2) : '—'],
  ]
  if (a.status === 'holding' && p) {
    items.push(['Weight', pct(p.weight)], ['Target', p.target ? pct(p.target.weight) : '—'], ['Value', money(p.value_brl)])
  }
  return (
    <dl className="mb-6 grid grid-cols-3 gap-x-4 gap-y-3 sm:flex sm:flex-wrap sm:gap-x-6">
      {items.map(([k, v]) => (
        <div key={k}>
          <dt className="eyebrow">{k}</dt>
          <dd className="num text-base sm:text-lg">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

function Overview({ asset: a, position: p, snapshotError }: { asset: Asset; position?: Position; snapshotError: unknown }) {
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Panel title="Buy gate">
        {snapshotError ? (
          <ErrorBlock error={snapshotError} />
        ) : (
          <GateChecklist gate={p?.gate} error={p?.gate_error} />
        )}
        <p className="text-muted-foreground mt-3 text-xs">
          Fix a failing check on its tab: <Link to="." search={{ tab: 'ratings' }} className="underline">ratings</Link>,{' '}
          <Link to="." search={{ tab: 'thesis' }} className="underline">thesis</Link> or{' '}
          <Link to="." search={{ tab: 'valuation' }} className="underline">fair value</Link>.
        </p>
      </Panel>
      {a.sleeve === 'tactical' && <TacticalPanel asset={a} view={p?.tactical} />}
      <Panel title="Position">
        {!p || Number(p.quantity) === 0 ? (
          <p className="text-muted-foreground text-sm">Not held. Buys come from the contribution planner.</p>
        ) : (
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <dt className="text-muted-foreground">Quantity</dt>
            <dd className="num text-right">{qty(p.quantity)}</dd>
            <dt className="text-muted-foreground">Average cost</dt>
            <dd className="num text-right">{money(p.avg_cost, a.currency)}</dd>
            <dt className="text-muted-foreground">Value</dt>
            <dd className="num text-right">{money(p.value_brl)}</dd>
            <dt className="text-muted-foreground">Dividends, 12 months</dt>
            <dd className="num text-right">
              {money(p.dividends_ttm)} <span className="text-muted-foreground">({pct(p.yield_on_cost)} on cost)</span>
            </dd>
            <dt className="text-muted-foreground">Ramp-in stage</dt>
            <dd className="num text-right">{p.ramp_stage} of 3</dd>
            {p.last_sell && (
              <>
                <dt className="text-muted-foreground">Last sell</dt>
                <dd className="num text-right">{p.last_sell}</dd>
              </>
            )}
            {p.target && (
              <>
                <dt className="text-muted-foreground">Band</dt>
                <dd className="num text-right">
                  {pct(p.target.lower)} – {pct(p.target.upper)}
                </dd>
              </>
            )}
          </dl>
        )}
      </Panel>
    </div>
  )
}

function TradesSection({ asset: a }: { asset: Asset }) {
  const trades = useQuery({
    queryKey: [...qk.asset(a.id), 'trades'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/trades', { params: { path: { id: a.id } } })),
  })
  return (
    <div className="space-y-4">
      <Panel title="Record a trade">
        <TradeForm assets={[a]} initial={{ asset: a.id }} lockAsset />
      </Panel>
      <Panel title="Trades">
        {trades.isPending ? (
          <LoadingBlock rows={2} />
        ) : trades.isError ? (
          <ErrorBlock error={trades.error} />
        ) : (trades.data ?? []).length === 0 ? (
          <p className="text-muted-foreground text-sm">No trades yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Date</TableHead>
                <TableHead>Side</TableHead>
                <TableHead className="text-right">Quantity</TableHead>
                <TableHead className="text-right">Price</TableHead>
                <TableHead className="text-right">Fees</TableHead>
                <TableHead className="w-8" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...(trades.data ?? [])].sort((x, y) => y.date.localeCompare(x.date) || y.id - x.id).map((t) => (
                <TradeRow key={t.id} trade={t} />
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
    </div>
  )
}
