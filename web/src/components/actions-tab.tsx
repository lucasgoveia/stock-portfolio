import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, money, qty } from '@/lib/format'
import { assetsQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Action = Schemas['Action']
type Kind = 'split' | 'reverse_split' | 'bonus'
const actionsKey = ['corporate-actions'] as const

const KIND_LABEL: Record<string, string> = {
  split: 'Split (desdobramento)',
  reverse_split: 'Reverse split (grupamento)',
  bonus: 'Bonificação',
}

function describeFactor(kind: string, factor: string): string {
  const f = Number(factor)
  if (kind === 'reverse_split' && f > 0) return `1 for ${qty(String(1 / f))}`
  if (kind === 'bonus') return `+${qty(String((f - 1) * 100))}% shares`
  return `${qty(factor)} for 1`
}

function useInvalidate() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries()
}

export function ActionsTab() {
  const list = useQuery({ queryKey: actionsKey, queryFn: () => unwrap(api.GET('/api/corporate-actions')) })
  const assets = useQuery(assetsQuery)
  const ticker = new Map((assets.data ?? []).map((a) => [a.id, a]))
  if (list.isPending) return <LoadingBlock rows={3} />
  if (list.isError) return <ErrorBlock error={list.error} />
  const rows = list.data ?? []
  const queue = rows.filter((a) => a.status === 'detected')
  const done = rows.filter((a) => a.status !== 'detected')
  return (
    <div className="space-y-6">
      {queue.length > 0 && (
        <Panel title={`To confirm (${queue.length})`}>
          <p className="text-muted-foreground mb-3 text-xs">
            Announced by brapi. The price check compares raw closes around the ex-date with the factor.
          </p>
          <ul className="divide-y">
            {queue.map((a) => (
              <QueueRow key={a.id} a={a} asset={ticker.get(a.asset_id)} />
            ))}
          </ul>
        </Panel>
      )}
      <Panel title="Enter an event">
        <ActionForm assets={(assets.data ?? []).filter((a) => a.status !== 'watchlist')} />
      </Panel>
      <Panel title="History">
        {done.length === 0 ? (
          <p className="text-muted-foreground text-sm">No corporate actions yet.</p>
        ) : (
          <ul className="divide-y">
            {done.map((a) => (
              <HistoryRow key={a.id} a={a} asset={ticker.get(a.asset_id)} />
            ))}
          </ul>
        )}
      </Panel>
    </div>
  )
}

function PriceCheck({ a }: { a: Action }) {
  if (!a.price_ratio) return <span className="text-muted-foreground text-xs">no raw closes to check</span>
  return (
    <span className={cn('text-xs', a.ratio_matches ? 'text-jade' : 'text-breach')}>
      price ratio {qty(a.price_ratio)} {a.ratio_matches ? 'matches' : 'does not match'}
    </span>
  )
}

function QueueRow({ a, asset }: { a: Action; asset?: Schemas['Asset'] }) {
  const invalidate = useInvalidate()
  const [cost, setCost] = useState('')
  const confirm = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/corporate-actions/{id}/confirm', {
          params: { path: { id: a.id } },
          body: { cost_per_share: a.kind === 'bonus' ? (decimalInput(cost) ?? undefined) : undefined },
        }),
      ),
    onSuccess: () => {
      toast.success('Event applied to the position')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const reject = useMutation({
    mutationFn: () => unwrap(api.POST('/api/corporate-actions/{id}/reject', { params: { path: { id: a.id } } })),
    onSuccess: () => {
      toast.success('Dismissed')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <li className="space-y-2 py-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span>
          <span className="font-semibold">{asset?.ticker ?? `#${a.asset_id}`}</span>{' '}
          <span className="text-sm">{KIND_LABEL[a.kind]}</span>{' '}
          <span className="num text-muted-foreground text-xs">ex {a.ex_date}</span>
        </span>
        <span className="num text-sm">{describeFactor(a.kind, a.factor)}</span>
      </div>
      <div className="flex flex-wrap items-end gap-2">
        <PriceCheck a={a} />
        {a.kind === 'bonus' && (
          <Field label={`Cost per new share (${asset?.currency ?? 'BRL'})`} className="w-48">
            {(id) => <Input id={id} inputMode="decimal" value={cost} onChange={(e) => setCost(e.target.value)} />}
          </Field>
        )}
        <Button size="sm" disabled={confirm.isPending || (a.kind === 'bonus' && !decimalInput(cost))} onClick={() => confirm.mutate()}>
          Apply
        </Button>
        <Button size="sm" variant="ghost" disabled={reject.isPending} onClick={() => reject.mutate()}>
          Dismiss
        </Button>
      </div>
    </li>
  )
}

function HistoryRow({ a, asset }: { a: Action; asset?: Schemas['Asset'] }) {
  const invalidate = useInvalidate()
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/corporate-actions/{id}', { params: { path: { id: a.id } } })),
    onSuccess: () => {
      toast.success('Event deleted')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <li className={cn('flex flex-wrap items-center justify-between gap-2 py-2 text-sm', a.status === 'rejected' && 'text-muted-foreground')}>
      <span>
        <span className="num">{a.ex_date}</span> <span className="font-medium">{asset?.ticker ?? `#${a.asset_id}`}</span>{' '}
        {KIND_LABEL[a.kind]} · {describeFactor(a.kind, a.factor)}
        {a.kind === 'bonus' && ` at ${money(a.cost_per_share, asset?.currency)}`}
        {a.status === 'rejected' && ' · dismissed'}
      </span>
      <span className="flex items-center gap-2">
        <PriceCheck a={a} />
        <Button size="icon-xs" variant="ghost" aria-label="Delete event" disabled={del.isPending} onClick={() => del.mutate()}>
          <Trash2 />
        </Button>
      </span>
    </li>
  )
}

function ActionForm({ assets }: { assets: Schemas['Asset'][] }) {
  const invalidate = useInvalidate()
  const [assetId, setAssetId] = useState<number | ''>('')
  const [kind, setKind] = useState<Kind>('split')
  const [ex, setEx] = useState('')
  const [factor, setFactor] = useState('')
  const [cost, setCost] = useState('')
  const [error, setError] = useState<string | null>(null)
  const asset = assets.find((a) => a.id === assetId)
  const save = useMutation({
    mutationFn: (body: Schemas['ActionCreate']) =>
      unwrap(api.POST('/api/assets/{id}/corporate-actions', { params: { path: { id: Number(assetId) } }, body })),
    onSuccess: (a) => {
      toast.success(a.ratio_matches === false ? 'Applied, but the price ratio does not match the factor' : 'Event applied')
      setFactor('')
      setCost('')
      setError(null)
      invalidate()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const hint =
    kind === 'split' ? 'Shares multiplier: 2 for a 2-for-1 split.' : kind === 'reverse_split' ? '0.1 for 1-for-10.' : '1.1 for a 10% bonus.'
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (assetId === '') return setError('Choose the asset.')
        const f = decimalInput(factor)
        if (!f) return setError('Enter the factor.')
        if (!ex) return setError('Enter the ex-date.')
        const c = kind === 'bonus' ? decimalInput(cost) : undefined
        if (kind === 'bonus' && !c) return setError('Enter the cost per new share declared by the company.')
        save.mutate({ kind, ex_date: ex, factor: f, cost_per_share: c ?? undefined })
      }}
      className="grid grid-cols-2 gap-3 sm:grid-cols-5"
      noValidate
    >
      <Field label="Asset">
        {(id) => (
          <NativeSelect id={id} value={assetId} onChange={(e) => setAssetId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">Choose…</option>
            {assets.map((a) => (
              <option key={a.id} value={a.id}>
                {a.ticker}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      <Field label="Event">
        {(id) => (
          <NativeSelect id={id} value={kind} onChange={(e) => setKind(e.target.value as Kind)}>
            <option value="split">Split</option>
            <option value="reverse_split">Reverse split</option>
            <option value="bonus">Bonificação</option>
          </NativeSelect>
        )}
      </Field>
      <Field label="Ex-date">{(id) => <Input id={id} type="date" value={ex} onChange={(e) => setEx(e.target.value)} />}</Field>
      <Field label="Factor" hint={hint}>
        {(id) => <Input id={id} inputMode="decimal" value={factor} onChange={(e) => setFactor(e.target.value)} />}
      </Field>
      {kind === 'bonus' && (
        <Field label={`Cost per new share (${asset?.currency ?? 'BRL'})`}>
          {(id) => <Input id={id} inputMode="decimal" value={cost} onChange={(e) => setCost(e.target.value)} />}
        </Field>
      )}
      <div className="col-span-full flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending}>
          Apply event
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}
