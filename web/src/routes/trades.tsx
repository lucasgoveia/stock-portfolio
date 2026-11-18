import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ActionsTab } from '@/components/actions-tab'
import { DividendsTab } from '@/components/dividends-tab'
import { Field, FormError, NativeSelect } from '@/components/form'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { TradeForm } from '@/components/trade-form'
import { TradeRow } from '@/components/trade-row'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, humanize, money, qty, today } from '@/lib/format'
import { assetsQuery, cashSummaryQuery, instrumentsQuery, qk } from '@/lib/queries'
import { parseTradeSearch, type TradeSearch } from '@/lib/search'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/trades')({ validateSearch: parseTradeSearch, component: Page })

const TABS = [
  { id: 'trades', label: 'Trades' },
  { id: 'cash', label: 'Cash' },
  { id: 'dividends', label: 'Dividends' },
  { id: 'actions', label: 'Corporate actions' },
  { id: 'positions', label: 'Positions' },
] as const

function Page() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/trades' })
  const tab = search.tab ?? 'trades'
  return (
    <>
      <PageHeader
        title="Trades & cash"
        description="Record what you executed at the broker. Positions and average cost follow from these entries."
      />
      <div role="tablist" aria-label="Section" className="bg-muted mb-5 inline-flex max-w-full overflow-x-auto rounded-lg p-1 [scrollbar-width:none]">
        {TABS.map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => navigate({ search: { tab: t.id === 'trades' ? undefined : t.id } })}
            className={cn(
              'shrink-0 rounded-md px-3 py-1 text-sm transition-colors',
              tab === t.id ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'trades' && <TradesTab search={search} />}
      {tab === 'cash' && <CashTab />}
      {tab === 'dividends' && <DividendsTab />}
      {tab === 'actions' && <ActionsTab />}
      {tab === 'positions' && <PositionsTab />}
    </>
  )
}

function Panel({ title, children, className }: { title: string; children: React.ReactNode; className?: string }) {
  return (
    <section className={cn('bg-card rounded-lg border p-4', className)}>
      <h2 className="eyebrow mb-3">{title}</h2>
      {children}
    </section>
  )
}

function TradesTab({ search }: { search: TradeSearch }) {
  const assets = useQuery(assetsQuery)
  const list = assets.data ?? []
  // Key the form on the prefill so a new link from the inbox resets it.
  const formKey = `${search.asset}-${search.side}-${search.quantity}-${search.price}-${search.full}`
  return (
    <div className="space-y-6">
      <Panel title="Record a trade">
        {assets.isPending ? (
          <LoadingBlock rows={2} />
        ) : assets.isError ? (
          <ErrorBlock error={assets.error} />
        ) : list.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            Add an asset to the{' '}
            <Link to="/watchlist" className="text-foreground underline">
              watchlist
            </Link>{' '}
            first.
          </p>
        ) : (
          <TradeForm key={formKey} assets={list} initial={search} />
        )}
      </Panel>
      <RecentTrades assets={list} />
    </div>
  )
}

function RecentTrades({ assets }: { assets: Schemas['Asset'][] }) {
  const traded = assets.filter((a) => a.status !== 'watchlist')
  const results = useQueries({
    queries: traded.map((a) => ({
      queryKey: [...qk.asset(a.id), 'trades'],
      queryFn: () => unwrap(api.GET('/api/assets/{id}/trades', { params: { path: { id: a.id } } })),
    })),
  })
  const ticker = new Map(assets.map((a) => [a.id, a.ticker]))
  const trades = results
    .flatMap((r) => r.data ?? [])
    .sort((a, b) => b.date.localeCompare(a.date) || b.id - a.id)
  if (traded.length === 0) return null
  return (
    <Panel title="Trades">
      {trades.length === 0 ? (
        <p className="text-muted-foreground text-sm">No trades recorded yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Asset</TableHead>
              <TableHead>Side</TableHead>
              <TableHead className="text-right">Quantity</TableHead>
              <TableHead className="text-right">Price</TableHead>
              <TableHead className="text-right">Fees</TableHead>
              <TableHead className="w-8" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {trades.map((t) => (
              <TradeRow key={t.id} trade={t} ticker={ticker.get(t.asset_id) ?? `#${t.asset_id}`} />
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  )
}

function PositionsTab() {
  const assets = useQuery(assetsQuery)
  const positions = useQuery({ queryKey: [...qk.portfolio, 'positions'], queryFn: () => unwrap(api.GET('/api/positions')) })
  if (positions.isPending) return <LoadingBlock rows={4} />
  if (positions.isError) return <ErrorBlock error={positions.error} />
  const ticker = new Map((assets.data ?? []).map((a) => [a.id, a.ticker]))
  const rows = positions.data ?? []
  return (
    <Panel title="Positions (Receita average cost)">
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">No open positions. Record a buy to start one.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Asset</TableHead>
              <TableHead className="text-right">Quantity</TableHead>
              <TableHead className="text-right">Average cost</TableHead>
              <TableHead className="text-right">Total cost</TableHead>
              <TableHead className="text-right">Realized gain</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((p) => (
              <TableRow key={p.asset_id}>
                <TableCell>
                  <Link to="/assets/$assetId" params={{ assetId: String(p.asset_id) }} className="font-medium hover:underline">
                    {ticker.get(p.asset_id) ?? `#${p.asset_id}`}
                  </Link>
                </TableCell>
                <TableCell className="num text-right">{qty(p.quantity)}</TableCell>
                <TableCell className="num text-right">
                  {money(p.avg_cost, p.currency)}
                  {p.currency === 'USD' && <div className="text-muted-foreground text-xs">{money(p.avg_cost_brl)}</div>}
                </TableCell>
                <TableCell className="num text-right">{money(p.total_cost_brl)}</TableCell>
                <TableCell
                  className={cn('num text-right', Number(p.realized_gain_brl) < 0 ? 'text-breach' : '')}
                >
                  {money(p.realized_gain_brl)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  )
}

const KIND_LABEL: Record<string, string> = { tesouro_selic: 'Tesouro Selic', cdi: 'CDI', usd_cash: 'USD cash' }

function CashTab() {
  const summary = useQuery(cashSummaryQuery)
  const instruments = useQuery(instrumentsQuery)
  if (summary.isPending || instruments.isPending) return <LoadingBlock rows={4} />
  if (summary.isError) return <ErrorBlock error={summary.error} />
  if (instruments.isError) return <ErrorBlock error={instruments.error} />
  const values = new Map((summary.data.instruments ?? []).map((v) => [v.id, v]))
  return (
    <div className="space-y-6">
      <dl className="flex flex-wrap gap-x-8 gap-y-2 border-b pb-4">
        <div>
          <dt className="eyebrow">Total cash</dt>
          <dd className="display num text-3xl font-semibold">{money(summary.data.total_brl)}</dd>
        </div>
        <div>
          <dt className="eyebrow">BRL</dt>
          <dd className="num text-lg">{money(summary.data.brl)}</dd>
        </div>
        <div>
          <dt className="eyebrow">USD</dt>
          <dd className="num text-lg">
            {money(summary.data.usd_balance, 'USD')}{' '}
            <span className="text-muted-foreground text-sm">≈ {money(summary.data.usd_in_brl)}</span>
          </dd>
        </div>
      </dl>
      <div className="grid gap-4 lg:grid-cols-2">
        {(instruments.data ?? []).map((i) => (
          <InstrumentCard key={i.id} instrument={i} value={values.get(i.id)} />
        ))}
        <NewInstrument />
      </div>
    </div>
  )
}

function InstrumentCard({ instrument: i, value }: { instrument: Schemas['Instrument']; value?: Schemas['Valuation'] }) {
  const [showLedger, setShowLedger] = useState(false)
  return (
    <section className="bg-card rounded-lg border p-4">
      <div className="flex items-start justify-between gap-2">
        <div>
          <h3 className="font-semibold">{i.name}</h3>
          <p className="text-muted-foreground text-xs">
            {KIND_LABEL[i.kind] ?? i.kind}
            {i.kind === 'cdi' ? ` · ${i.pct_cdi}% of CDI` : ''}
          </p>
        </div>
        <div className="text-right">
          <div className="num font-medium">{money(value?.balance, i.currency)}</div>
          {i.currency === 'USD' && <div className="num text-muted-foreground text-xs">≈ {money(value?.value_brl)}</div>}
          {value?.quantity && <div className="num text-muted-foreground text-xs">{qty(value.quantity)} titles</div>}
          {value?.rate_date && <div className="text-muted-foreground text-xs">rate of {value.rate_date}</div>}
        </div>
      </div>
      <MovementForm instrument={i} />
      <button
        className="text-muted-foreground hover:text-foreground mt-3 text-xs underline"
        onClick={() => setShowLedger((v) => !v)}
        aria-expanded={showLedger}
      >
        {showLedger ? 'Hide ledger' : 'Show ledger'}
      </button>
      {showLedger && <Ledger instrument={i} />}
    </section>
  )
}

function MovementForm({ instrument: i }: { instrument: Schemas['Instrument'] }) {
  const qc = useQueryClient()
  const tesouro = i.kind === 'tesouro_selic'
  const [type, setType] = useState('deposit')
  const [date, setDate] = useState(today())
  const [amount, setAmount] = useState('')
  const [quantity, setQuantity] = useState('')
  const [error, setError] = useState<string | null>(null)
  const add = useMutation({
    mutationFn: (body: Schemas['MovementCreate']) =>
      unwrap(api.POST('/api/cash/instruments/{id}/movements', { params: { path: { id: i.id } }, body })),
    onSuccess: (m) => {
      toast.success(`${humanize(m.type)} of ${money(m.amount, i.currency)} recorded`)
      setAmount('')
      setQuantity('')
      setError(null)
      void qc.invalidateQueries({ queryKey: qk.cash })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const a = decimalInput(amount)
    if (!a) return setError('Amount must be a number.')
    const q = tesouro ? decimalInput(quantity) : undefined
    if (tesouro && !q) return setError('Tesouro Selic movements need the number of titles.')
    add.mutate({ type, date, amount: a, quantity: q ?? undefined })
  }
  return (
    <form onSubmit={submit} className="mt-4 grid grid-cols-2 gap-2" noValidate>
      <Field label="Movement">
        {(id) => (
          <NativeSelect id={id} value={type} onChange={(e) => setType(e.target.value)}>
            <option value="deposit">Deposit</option>
            <option value="withdraw">Withdraw</option>
            {!tesouro && <option value="yield_adjust">Yield adjustment</option>}
            {!tesouro && <option value="balance">Set balance</option>}
          </NativeSelect>
        )}
      </Field>
      <Field label="Date">{(id) => <Input id={id} type="date" value={date} onChange={(e) => setDate(e.target.value)} />}</Field>
      <Field label={`Amount (${i.currency})`}>
        {(id) => <Input id={id} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />}
      </Field>
      {tesouro && (
        <Field label="Titles">
          {(id) => <Input id={id} inputMode="decimal" value={quantity} onChange={(e) => setQuantity(e.target.value)} />}
        </Field>
      )}
      <div className="col-span-2 flex flex-wrap items-center gap-3">
        <Button type="submit" variant="outline" disabled={add.isPending}>
          Add {humanize(type).toLowerCase()}
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}

function Ledger({ instrument: i }: { instrument: Schemas['Instrument'] }) {
  const qc = useQueryClient()
  const list = useQuery({
    queryKey: [...qk.cash, 'movements', i.id],
    queryFn: () => unwrap(api.GET('/api/cash/instruments/{id}/movements', { params: { path: { id: i.id } } })),
  })
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/api/cash/movements/{id}', { params: { path: { id } } })),
    onSuccess: () => {
      toast.success('Movement deleted')
      void qc.invalidateQueries({ queryKey: qk.cash })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  if (list.isPending) return <LoadingBlock rows={2} />
  if (list.isError) return <ErrorBlock error={list.error} />
  const rows = list.data ?? []
  if (rows.length === 0) return <p className="text-muted-foreground mt-2 text-sm">No movements yet.</p>
  return (
    <ul className="mt-2 divide-y text-sm">
      {rows.map((m) => (
        <li key={m.id} className="flex items-center justify-between gap-2 py-1.5">
          <span className="num text-muted-foreground">{m.date}</span>
          <span className="flex-1">{humanize(m.type)}</span>
          <span className="num">
            {money(m.amount, i.currency)}
            {Number(m.quantity) > 0 && <span className="text-muted-foreground"> · {qty(m.quantity)}</span>}
          </span>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label={`Delete movement of ${m.date}`}
            disabled={del.isPending}
            onClick={() => del.mutate(m.id)}
          >
            <Trash2 />
          </Button>
        </li>
      ))}
    </ul>
  )
}

function NewInstrument() {
  const qc = useQueryClient()
  const [kind, setKind] = useState('cdi')
  const [name, setName] = useState('')
  const [pctCdi, setPctCdi] = useState('100')
  const [error, setError] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: (body: Schemas['InstrumentCreate']) => unwrap(api.POST('/api/cash/instruments', { body })),
    onSuccess: (i) => {
      toast.success(`Added ${i.name}`)
      setName('')
      setError(null)
      void qc.invalidateQueries({ queryKey: qk.cash })
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return setError('Give the instrument a name, e.g. "CDB Banco X".')
    const p = kind === 'cdi' ? decimalInput(pctCdi) : undefined
    if (kind === 'cdi' && !p) return setError('Percent of CDI must be a number.')
    create.mutate({ kind, name: name.trim(), pct_cdi: p ?? undefined })
  }
  return (
    <section className="rounded-lg border border-dashed p-4">
      <h3 className="eyebrow mb-3">Add an instrument</h3>
      <form onSubmit={submit} className="grid grid-cols-2 gap-2" noValidate>
        <Field label="Kind">
          {(id) => (
            <NativeSelect id={id} value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="cdi">CDI instrument</option>
              <option value="tesouro_selic">Tesouro Selic</option>
              <option value="usd_cash">USD cash</option>
            </NativeSelect>
          )}
        </Field>
        <Field label="Name">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} />}</Field>
        {kind === 'cdi' && (
          <Field label="% of CDI">
            {(id) => <Input id={id} inputMode="decimal" value={pctCdi} onChange={(e) => setPctCdi(e.target.value)} />}
          </Field>
        )}
        <div className="col-span-2 flex flex-wrap items-center gap-3">
          <Button type="submit" variant="outline" disabled={create.isPending}>
            Add instrument
          </Button>
          <FormError error={error} />
        </div>
      </form>
    </section>
  )
}
