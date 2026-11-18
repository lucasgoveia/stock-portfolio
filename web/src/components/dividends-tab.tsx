import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, money, qty, today } from '@/lib/format'
import { assetsQuery, instrumentsQuery, qk } from '@/lib/queries'

type Dividend = Schemas['Dividend']
const dividendsKey = ['dividends'] as const

function useInvalidate() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: dividendsKey })
    void qc.invalidateQueries({ queryKey: qk.cash })
    void qc.invalidateQueries({ queryKey: qk.portfolio })
  }
}

/** Instruments a dividend of this currency can be credited to. */
function useCreditable(currency?: string) {
  const ins = useQuery(instrumentsQuery)
  return (ins.data ?? []).filter((i) => i.kind !== 'tesouro_selic' && (!currency || i.currency === currency))
}

export function DividendsTab() {
  const list = useQuery({ queryKey: dividendsKey, queryFn: () => unwrap(api.GET('/api/dividends')) })
  const assets = useQuery(assetsQuery)
  const byId = new Map((assets.data ?? []).map((a) => [a.id, a]))
  if (list.isPending) return <LoadingBlock rows={4} />
  if (list.isError) return <ErrorBlock error={list.error} />
  const rows = list.data ?? []
  const queue = rows.filter((d) => d.status === 'detected')
  const confirmed = rows.filter((d) => d.status === 'confirmed')
  return (
    <div className="space-y-6">
      {queue.length > 0 && (
        <Panel title={`To confirm (${queue.length})`}>
          <p className="text-muted-foreground mb-3 text-xs">
            Announced by brapi and sized by the shares you held on the record date. Confirm once the money arrives.
          </p>
          <ul className="divide-y">
            {queue.map((d) => (
              <QueueRow key={d.id} d={d} asset={byId.get(d.asset_id)} />
            ))}
          </ul>
        </Panel>
      )}
      <Panel title="Record a payment">
        <RecordForm assets={(assets.data ?? []).filter((a) => a.status !== 'watchlist')} />
      </Panel>
      <Panel title="Received">
        {confirmed.length === 0 ? (
          <p className="text-muted-foreground text-sm">No dividends recorded yet.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Paid</TableHead>
                <TableHead>Asset</TableHead>
                <TableHead className="text-right">Gross</TableHead>
                <TableHead className="text-right">Withheld</TableHead>
                <TableHead className="text-right">Net</TableHead>
                <TableHead>Plan</TableHead>
                <TableHead className="w-8" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {confirmed.map((d) => (
                <ConfirmedRow key={d.id} d={d} ticker={byId.get(d.asset_id)?.ticker} />
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
    </div>
  )
}

function QueueRow({ d, asset }: { d: Dividend; asset?: Schemas['Asset'] }) {
  const invalidate = useInvalidate()
  const instruments = useCreditable(d.currency)
  const [ins, setIns] = useState<number | ''>(instruments[0]?.id ?? '')
  const [pay, setPay] = useState(d.pay_date ?? '')
  const confirm = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/dividends/{id}/confirm', {
          params: { path: { id: d.id } },
          body: { instrument_id: Number(ins), pay_date: pay || undefined },
        }),
      ),
    onSuccess: (r) => {
      toast.success(`Credited ${money(r.net, r.currency)} to cash`)
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const reject = useMutation({
    mutationFn: () => unwrap(api.POST('/api/dividends/{id}/reject', { params: { path: { id: d.id } } })),
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
          <span className="font-semibold">{asset?.ticker ?? `#${d.asset_id}`}</span>{' '}
          <span className="text-muted-foreground text-xs uppercase">{d.kind}</span>{' '}
          <span className="num text-muted-foreground text-xs">record {d.record_date}</span>
        </span>
        <span className="num text-sm">
          {qty(d.quantity)} × {d.currency === 'USD' ? '$' : 'R$'} {qty(d.per_share)} = {money(d.gross, d.currency)}
          <span className="text-muted-foreground"> · net ≈ {money(d.net, d.currency)}</span>
        </span>
      </div>
      <div className="flex flex-wrap items-end gap-2">
        <Field label="Credit to" className="w-44">
          {(id) => (
            <NativeSelect id={id} value={ins} onChange={(e) => setIns(e.target.value ? Number(e.target.value) : '')}>
              <option value="">Choose…</option>
              {instruments.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name}
                </option>
              ))}
            </NativeSelect>
          )}
        </Field>
        <Field label="Paid on" className="w-40">
          {(id) => <Input id={id} type="date" value={pay} onChange={(e) => setPay(e.target.value)} />}
        </Field>
        <Button size="sm" disabled={confirm.isPending || ins === '' || !pay} onClick={() => confirm.mutate()}>
          Confirm
        </Button>
        <Button size="sm" variant="ghost" disabled={reject.isPending} onClick={() => reject.mutate()}>
          Dismiss
        </Button>
      </div>
      {d.currency === 'USD' && <p className="text-muted-foreground text-xs">USD payments need the FX rate: record them by hand.</p>}
    </li>
  )
}

function ConfirmedRow({ d, ticker }: { d: Dividend; ticker?: string }) {
  const invalidate = useInvalidate()
  const [confirming, setConfirming] = useState(false)
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/dividends/{id}', { params: { path: { id: d.id } } })),
    onSuccess: () => {
      toast.success('Payment and its cash credit deleted')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <TableRow>
      <TableCell className="num">{d.pay_date ?? d.record_date}</TableCell>
      <TableCell>
        <span className="font-medium">{ticker ?? `#${d.asset_id}`}</span>{' '}
        <span className="text-muted-foreground text-xs uppercase">{d.kind}</span>
      </TableCell>
      <TableCell className="num text-right">{money(d.gross, d.currency)}</TableCell>
      <TableCell className="num text-right">{money(d.withholding, d.currency)}</TableCell>
      <TableCell className="num text-right">
        {money(d.net, d.currency)}
        {d.currency === 'USD' && <div className="text-muted-foreground text-xs">{money(d.net_brl)}</div>}
      </TableCell>
      <TableCell className="text-muted-foreground text-xs">{d.contribution_id ? `#${d.contribution_id}` : 'to deploy'}</TableCell>
      <TableCell className="text-right">
        {confirming ? (
          <span className="inline-flex gap-1">
            <Button size="xs" variant="destructive" disabled={del.isPending} onClick={() => del.mutate()}>
              Delete
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(false)}>
              Keep
            </Button>
          </span>
        ) : (
          <Button size="icon-xs" variant="ghost" aria-label="Delete payment" onClick={() => setConfirming(true)}>
            <Trash2 />
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

function RecordForm({ assets }: { assets: Schemas['Asset'][] }) {
  const invalidate = useInvalidate()
  const [assetId, setAssetId] = useState<number | ''>('')
  const asset = assets.find((a) => a.id === assetId)
  const instruments = useCreditable(asset?.currency)
  const [kind, setKind] = useState<'dividend' | 'jcp'>('dividend')
  const [record, setRecord] = useState('')
  const [pay, setPay] = useState(today())
  const [perShare, setPerShare] = useState('')
  const [quantity, setQuantity] = useState('')
  const [withholding, setWithholding] = useState('')
  const [fx, setFx] = useState('')
  const [ins, setIns] = useState<number | ''>('')
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (body: Schemas['DividendCreate']) =>
      unwrap(api.POST('/api/assets/{id}/dividends', { params: { path: { id: Number(assetId) } }, body })),
    onSuccess: (d) => {
      toast.success(`Recorded ${money(d.net, d.currency)} net (${money(d.withholding, d.currency)} withheld)`)
      setPerShare('')
      setQuantity('')
      setWithholding('')
      setError(null)
      invalidate()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const usd = asset?.currency === 'USD'
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (assetId === '') return setError('Choose the asset.')
        const ps = decimalInput(perShare)
        if (!ps) return setError('Enter the gross amount per share.')
        if (!record || !pay) return setError('Enter the record (data-com) and payment dates.')
        if (ins === '') return setError('Choose the cash instrument that received the money.')
        const q = quantity ? decimalInput(quantity) : undefined
        const w = withholding ? decimalInput(withholding) : undefined
        const f = fx ? decimalInput(fx) : undefined
        if (q === null || w === null || f === null) return setError('Quantity, withholding and FX must be numbers.')
        if (usd && !f) return setError('USD payments need the USD/BRL rate of the payment date.')
        save.mutate({
          kind,
          record_date: record,
          pay_date: pay,
          per_share: ps,
          quantity: q,
          withholding: w,
          fx_rate: f,
          instrument_id: ins,
        })
      }}
      className="grid grid-cols-2 gap-3 sm:grid-cols-4"
      noValidate
    >
      <Field label="Asset">
        {(id) => (
          <NativeSelect
            id={id}
            value={assetId}
            onChange={(e) => {
              setAssetId(e.target.value ? Number(e.target.value) : '')
              setIns('')
            }}
          >
            <option value="">Choose…</option>
            {assets.map((a) => (
              <option key={a.id} value={a.id}>
                {a.ticker}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      <Field label="Kind">
        {(id) => (
          <NativeSelect id={id} value={kind} onChange={(e) => setKind(e.target.value as 'dividend' | 'jcp')}>
            <option value="dividend">Dividend</option>
            {!usd && <option value="jcp">JCP</option>}
          </NativeSelect>
        )}
      </Field>
      <Field label="Record date (data-com)">
        {(id) => <Input id={id} type="date" value={record} onChange={(e) => setRecord(e.target.value)} />}
      </Field>
      <Field label="Paid on">{(id) => <Input id={id} type="date" value={pay} onChange={(e) => setPay(e.target.value)} />}</Field>
      <Field label={`Gross per share (${asset?.currency ?? 'BRL'})`}>
        {(id) => <Input id={id} inputMode="decimal" value={perShare} onChange={(e) => setPerShare(e.target.value)} />}
      </Field>
      <Field label="Shares" hint="Blank: held on the record date.">
        {(id) => <Input id={id} inputMode="decimal" value={quantity} onChange={(e) => setQuantity(e.target.value)} />}
      </Field>
      <Field label="Withheld" hint={usd ? 'Blank: 30%.' : 'Blank: JCP 17.5%; dividends 10% over R$ 50k/month.'}>
        {(id) => <Input id={id} inputMode="decimal" value={withholding} onChange={(e) => setWithholding(e.target.value)} />}
      </Field>
      {usd && (
        <Field label="USD/BRL on payment">
          {(id) => <Input id={id} inputMode="decimal" value={fx} onChange={(e) => setFx(e.target.value)} />}
        </Field>
      )}
      <Field label="Credit to">
        {(id) => (
          <NativeSelect id={id} value={ins} onChange={(e) => setIns(e.target.value ? Number(e.target.value) : '')}>
            <option value="">Choose…</option>
            {instruments.map((i) => (
              <option key={i.id} value={i.id}>
                {i.name}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      <div className="col-span-full flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending}>
          Record payment
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}
