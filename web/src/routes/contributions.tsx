import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { Check, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError } from '@/components/form'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { TradeForm } from '@/components/trade-form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime, decimalInput, humanize, money, qty } from '@/lib/format'
import { assetsQuery, portfolioQuery, qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Contribution = Schemas['Contribution']
type Order = Schemas['Order']
type OrderState = Schemas['OrderState']

export const Route = createFileRoute('/contributions')({
  validateSearch: (s: Record<string, unknown>): { plan?: number } => ({
    plan: Number(s.plan) > 0 ? Number(s.plan) : undefined,
  }),
  component: Page,
})

const contributionsKey = ['contributions'] as const

function Page() {
  const { plan: selected } = Route.useSearch()
  const navigate = useNavigate({ from: '/contributions' })
  const history = useQuery({
    queryKey: contributionsKey,
    queryFn: () => unwrap(api.GET('/api/contributions')),
  })
  const list = history.data ?? []
  // Default to the newest plan still being executed.
  const current =
    list.find((c) => c.id === selected) ??
    (selected ? undefined : list.find((c) => c.status === 'planned' || c.status === 'partial'))
  const select = (id: number) => navigate({ search: { plan: id } })

  return (
    <>
      <PageHeader
        title="Contributions"
        description="New money is the main rebalancing tool. Plan where it goes, execute at the broker, then record each order here."
      />
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="min-w-0 space-y-6">
          <NewPlan onCreated={(c) => select(c.id)} />
          {history.isPending ? (
            <LoadingBlock rows={4} />
          ) : history.isError ? (
            <ErrorBlock error={history.error} />
          ) : current ? (
            <PlanView key={current.id} c={current} />
          ) : null}
        </div>
        <History list={list} current={current?.id} onSelect={select} />
      </div>
    </>
  )
}

function NewPlan({ onCreated }: { onCreated: (c: Contribution) => void }) {
  const qc = useQueryClient()
  const snap = useQuery(portfolioQuery)
  const [amount, setAmount] = useState('')
  const [error, setError] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: (a: string) => unwrap(api.POST('/api/contributions', { body: { amount: a } })),
    onSuccess: (c) => {
      setAmount('')
      setError(null)
      void qc.invalidateQueries({ queryKey: contributionsKey })
      onCreated(c)
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <Panel title="New contribution">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const a = decimalInput(amount)
          if (!a || Number(a) <= 0) return setError('Enter the amount you are adding, in reais.')
          create.mutate(a)
        }}
        className="flex flex-wrap items-end gap-3"
        noValidate
      >
        <Field label="Amount (BRL)" className="w-48">
          {(id) => (
            <Input id={id} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="3000" />
          )}
        </Field>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? 'Planning…' : 'Plan contribution'}
        </Button>
        <FormError error={error} />
      </form>
      {snap.data && Number(snap.data.pending_us) > 0 && (
        <p className="text-muted-foreground mt-2 text-xs">
          The pending US bucket ({money(snap.data.pending_us)}) is included when it reaches the remittance minimum.
        </p>
      )}
    </Panel>
  )
}

const STATUS_STYLE: Record<string, string> = {
  planned: 'border-brass/50 text-brass',
  partial: 'border-brass/50 text-brass',
  executed: 'border-jade/50 text-jade',
  discarded: 'text-muted-foreground',
}

function StatusChip({ status }: { status: string }) {
  return (
    <span className={cn('rounded-full border px-2 py-0.5 text-xs font-medium', STATUS_STYLE[status])}>{humanize(status)}</span>
  )
}

function History({ list, current, onSelect }: { list: Contribution[]; current?: number; onSelect: (id: number) => void }) {
  if (list.length === 0) return null
  return (
    <aside className="min-w-0">
      <h2 className="eyebrow mb-2">Plan history</h2>
      <ul className="bg-card divide-y rounded-lg border">
        {list.map((c) => {
          const done = c.orders?.filter((o) => o.status !== 'open').length ?? 0
          return (
            <li key={c.id}>
              <button
                onClick={() => onSelect(c.id)}
                aria-current={c.id === current}
                className={cn(
                  'hover:bg-accent flex w-full items-center justify-between gap-2 px-3 py-2.5 text-left transition-colors',
                  c.id === current && 'bg-accent',
                )}
              >
                <span>
                  <span className="num block font-medium">{money(c.amount)}</span>
                  <span className="text-muted-foreground num text-xs">{dateTime(c.created_at)}</span>
                </span>
                <span className="flex flex-col items-end gap-1">
                  <StatusChip status={c.status} />
                  <span className="text-muted-foreground num text-xs">
                    {done}/{c.orders?.length ?? 0} orders
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ul>
    </aside>
  )
}

const KIND_LABEL: Record<string, string> = {
  buy: 'Buy',
  cash: 'To cash',
  remit: 'Remit to US',
  hold_us: 'Hold for US',
}

const SKIP_REASON: Record<string, string> = {
  below_min_order: 'below the minimum order',
  below_one_share: 'not enough for one share',
  no_room: 'no room under its target',
}

function PlanView({ c }: { c: Contribution }) {
  const qc = useQueryClient()
  const plan = c.plan
  const tickers = (plan.tickers ?? {}) as Record<string, string>
  const states = new Map((c.orders ?? []).map((o) => [o.seq, o]))
  const discard = useMutation({
    mutationFn: () => unwrap(api.POST('/api/contributions/{id}/discard', { params: { path: { id: c.id } } })),
    onSuccess: () => {
      toast.success('Plan discarded')
      void qc.invalidateQueries({ queryKey: contributionsKey })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const editable = c.status !== 'discarded'
  const orders = plan.orders ?? []

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="eyebrow">Plan of {dateTime(c.created_at)}</div>
          <div className="flex items-center gap-3">
            <span className="display num text-4xl font-semibold">{money(c.amount)}</span>
            <StatusChip status={c.status} />
          </div>
        </div>
        {editable && c.status !== 'executed' && (
          <Button variant="ghost" size="sm" disabled={discard.isPending} onClick={() => discard.mutate()}>
            Discard plan
          </Button>
        )}
      </div>
      <dl className="text-muted-foreground mb-4 flex flex-wrap gap-x-6 gap-y-1 text-sm">
        <div className="flex gap-1.5">
          <dt>Placed</dt>
          <dd className="num text-foreground">{money(plan.new_money)}</dd>
        </div>
        <div className="flex gap-1.5">
          <dt>Left as cash</dt>
          <dd className="num text-foreground">{money(plan.unallocated)}</dd>
        </div>
        <div className="flex gap-1.5">
          <dt>Pending US after</dt>
          <dd className="num text-foreground">{money(plan.pending_us_after)}</dd>
        </div>
      </dl>

      {orders.length === 0 ? (
        <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
          Nothing to buy: the whole amount stays as cash.
        </p>
      ) : (
        <ol className="bg-card divide-y rounded-lg border">
          {orders.map((o) => (
            <OrderRow
              key={o.seq}
              c={c}
              order={o}
              state={states.get(o.seq)}
              ticker={tickers[o.asset_id] ?? o.asset_id}
              editable={editable}
            />
          ))}
        </ol>
      )}

      {(plan.skipped ?? []).length > 0 && (
        <div className="mt-4">
          <h3 className="eyebrow mb-1">Not funded this time</h3>
          <ul className="text-muted-foreground space-y-0.5 text-sm">
            {(plan.skipped ?? []).map((s) => (
              <li key={s.asset_id}>
                {tickers[s.asset_id] ?? s.asset_id}: {SKIP_REASON[s.reason] ?? humanize(s.reason)}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

function OrderRow({
  c,
  order: o,
  state,
  ticker,
  editable,
}: {
  c: Contribution
  order: Order
  state?: OrderState
  ticker: string
  editable: boolean
}) {
  const qc = useQueryClient()
  const assets = useQuery(assetsQuery)
  const [recording, setRecording] = useState(false)
  const status = state?.status ?? 'open'
  const set = useMutation({
    mutationFn: (body: Schemas['OrderUpdate']) =>
      unwrap(
        api.POST('/api/contributions/{id}/orders/{seq}', {
          params: { path: { id: c.id, seq: o.seq } },
          body,
        }),
      ),
    onSuccess: async (_, body) => {
      // US money held or remitted changes the pending bucket to what the plan left.
      if (body.status === 'done' && (o.kind === 'hold_us' || o.kind === 'remit')) {
        try {
          await unwrap(api.PUT('/api/portfolio/pending-us', { body: { amount: c.plan.pending_us_after } }))
        } catch (e) {
          toast.error(`Order saved, but the pending US bucket was not updated: ${errorMessage(e)}`)
        }
      }
      setRecording(false)
      void qc.invalidateQueries({ queryKey: contributionsKey })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const assetId = Number(o.asset_id)
  const asset = assets.data?.find((a) => a.id === assetId)
  const isBuy = o.kind === 'buy'

  return (
    <li className={cn('p-3 sm:p-4', status !== 'open' && 'bg-muted/40')}>
      <div className="grid grid-cols-[2rem_minmax(0,1fr)] gap-x-3 gap-y-2 sm:grid-cols-[2rem_minmax(0,1fr)_auto]">
        <span
          className={cn(
            'num flex size-7 items-center justify-center rounded-full border text-xs',
            status === 'done' && 'bg-jade border-jade text-background',
            status === 'skipped' && 'text-muted-foreground line-through',
          )}
          aria-label={`Order ${o.seq}, ${status}`}
        >
          {status === 'done' ? <Check className="size-4" /> : o.seq}
        </span>
        <div className="min-w-0">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{KIND_LABEL[o.kind] ?? o.kind}</span>
            {isBuy ? (
              <Link to="/assets/$assetId" params={{ assetId: o.asset_id }} className="font-semibold hover:underline">
                {ticker}
              </Link>
            ) : (
              <span className="font-semibold">{o.kind === 'cash' ? 'Cash' : 'US bucket'}</span>
            )}
            {o.step && <span className="text-muted-foreground text-xs">{o.step === 'opportunity' ? 'opportunity' : 'underweight'}</span>}
          </div>
          <div className="num text-sm">
            {isBuy ? (
              <>
                {qty(o.quantity)} × {money(o.price)} = <span className="font-medium">{money(o.amount)}</span>
                {o.ramp_stage_after > 0 && <span className="text-muted-foreground"> · tranche {o.ramp_stage_after} of 3</span>}
              </>
            ) : (
              <span className="font-medium">{money(o.amount)}</span>
            )}
          </div>
          {status === 'done' && state?.trade_id != null && (
            <div className="text-muted-foreground text-xs">Recorded as trade #{state.trade_id}</div>
          )}
          {status === 'skipped' && <div className="text-muted-foreground text-xs">Skipped</div>}
        </div>
        {editable && (
          <div className="col-span-2 flex flex-wrap gap-1 sm:col-span-1 sm:justify-end">
            {status === 'open' ? (
              <>
                {isBuy ? (
                  <Button size="sm" onClick={() => setRecording((v) => !v)} aria-expanded={recording}>
                    {recording ? 'Close' : 'Record buy'}
                  </Button>
                ) : (
                  <Button size="sm" disabled={set.isPending} onClick={() => set.mutate({ status: 'done' })}>
                    {o.kind === 'cash' ? 'Mark deposited' : o.kind === 'remit' ? 'Mark remitted' : 'Mark held'}
                  </Button>
                )}
                <Button size="sm" variant="ghost" disabled={set.isPending} onClick={() => set.mutate({ status: 'skipped' })}>
                  Skip
                </Button>
              </>
            ) : (
              <Button size="sm" variant="ghost" disabled={set.isPending} onClick={() => set.mutate({ status: 'open' })}>
                <RotateCcw /> Reopen
              </Button>
            )}
          </div>
        )}
      </div>
      {recording && asset && (
        <div className="mt-3 rounded-md border p-3 sm:ml-11">
          <p className="text-muted-foreground mb-2 text-xs">
            Enter what was executed. Quantity and price can differ from the plan.
            {asset.currency === 'USD' ? ' The planned price is in BRL; enter the USD price.' : ''}
          </p>
          <TradeForm
            assets={[asset]}
            lockAsset
            initial={{
              asset: asset.id,
              side: 'buy',
              quantity: o.quantity,
              price: asset.currency === 'BRL' ? o.price : undefined,
            }}
            submitLabel="Record buy and mark done"
            onRecorded={(t) => set.mutate({ status: 'done', trade_id: t.id })}
          />
        </div>
      )}
      {o.kind === 'cash' && status === 'open' && editable && (
        <p className="text-muted-foreground mt-2 text-xs sm:ml-11">
          Deposit it into a cash instrument, then record the movement on{' '}
          <Link to="/trades" search={{ tab: 'cash' }} className="underline">
            Trades & cash
          </Link>
          .
        </p>
      )}
    </li>
  )
}
