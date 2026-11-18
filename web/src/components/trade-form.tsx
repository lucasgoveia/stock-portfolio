import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Field, FormError, NativeSelect } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, qty, today } from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

export interface TradeDraft {
  asset?: number
  side?: 'buy' | 'sell'
  quantity?: string
  price?: string
  full?: boolean
}

interface Props {
  assets: Schemas['Asset'][]
  initial?: TradeDraft
  /** Hide the asset picker (asset page). */
  lockAsset?: boolean
  submitLabel?: string
  onRecorded?: (t: Schemas['Trade']) => void
}

export function TradeForm({ assets, initial, lockAsset, submitLabel, onRecorded }: Props) {
  const qc = useQueryClient()
  const [assetId, setAssetId] = useState<number | undefined>(initial?.asset)
  const [side, setSide] = useState<'buy' | 'sell'>(initial?.side ?? 'buy')
  const [date, setDate] = useState(today())
  const [quantity, setQuantity] = useState(initial?.quantity ?? '')
  const [price, setPrice] = useState(initial?.price ?? '')
  const [fees, setFees] = useState('')
  const [fx, setFx] = useState('')
  const [error, setError] = useState<string | null>(null)

  const asset = assets.find((a) => a.id === assetId)
  const usd = asset?.currency === 'USD'

  // A full exit sells the whole position: read it once the asset is known.
  const position = useQuery({
    queryKey: [...qk.asset(assetId ?? 0), 'position'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/position', { params: { path: { id: assetId ?? 0 } } })),
    enabled: assetId != null,
  })
  const [fullPending, setFullPending] = useState(initial?.full ?? false)
  const quantityValue = fullPending && position.data ? position.data.quantity : quantity

  const record = useMutation({
    mutationFn: (body: Schemas['TradeCreate']) =>
      unwrap(api.POST('/api/assets/{id}/trades', { params: { path: { id: assetId ?? 0 } }, body })),
    onSuccess: (t) => {
      toast.success(`Recorded ${t.side} of ${qty(t.quantity)} ${asset?.ticker ?? ''}`)
      setQuantity('')
      setFullPending(false)
      setFees('')
      setError(null)
      void qc.invalidateQueries()
      onRecorded?.(t)
    },
    onError: (e) => setError(errorMessage(e)),
  })

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (assetId == null) return setError('Choose an asset.')
    const q = decimalInput(quantityValue)
    const p = decimalInput(price)
    if (!q || Number(q) <= 0) return setError('Quantity must be a positive number, e.g. 10 or 0.5.')
    if (!p || Number(p) <= 0) return setError('Price must be a positive number.')
    const f = fees ? decimalInput(fees) : undefined
    if (f === null) return setError('Fees must be a number.')
    const r = fx ? decimalInput(fx) : undefined
    if (usd && !r) return setError('USD trades need the USD/BRL rate of the trade date.')
    record.mutate({ date, side, quantity: q, price: p, fees: f, fx_rate: r ?? undefined })
  }

  const held = position.data && Number(position.data.quantity) > 0 ? position.data : null

  return (
    <form
      onSubmit={submit}
      className={cn('grid grid-cols-2 gap-3', lockAsset ? 'sm:grid-cols-3 xl:grid-cols-5' : 'sm:grid-cols-6')}
      noValidate
    >
      {!lockAsset && (
        <Field label="Asset" className="col-span-2 sm:col-span-2">
          {(id) => (
            <NativeSelect
              id={id}
              value={assetId ?? ''}
              onChange={(e) => setAssetId(e.target.value ? Number(e.target.value) : undefined)}
            >
              <option value="">Choose…</option>
              {assets.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.ticker} ({a.status})
                </option>
              ))}
            </NativeSelect>
          )}
        </Field>
      )}
      <Field label="Side">
        {(id) => (
          <NativeSelect id={id} value={side} onChange={(e) => setSide(e.target.value as 'buy' | 'sell')}>
            <option value="buy">Buy</option>
            <option value="sell">Sell</option>
          </NativeSelect>
        )}
      </Field>
      <Field label="Date">
        {(id) => <Input id={id} type="date" value={date} onChange={(e) => setDate(e.target.value)} required />}
      </Field>
      <Field
        label="Quantity"
        hint={held && side === 'sell' ? `Held: ${qty(held.quantity)}` : undefined}
      >
        {(id) => (
          <Input id={id} inputMode="decimal" value={quantityValue}
            onChange={(e) => {
              setFullPending(false)
              setQuantity(e.target.value)
            }}
            placeholder="10"
          />
        )}
      </Field>
      <Field label={`Price (${asset?.currency ?? 'BRL'})`}>
        {(id) => <Input id={id} inputMode="decimal" value={price} onChange={(e) => setPrice(e.target.value)} placeholder="0.00" />}
      </Field>
      <Field label="Fees" hint="Brokerage and B3 fees.">
        {(id) => <Input id={id} inputMode="decimal" value={fees} onChange={(e) => setFees(e.target.value)} placeholder="0" />}
      </Field>
      {usd && (
        <Field label="USD/BRL on trade date">
          {(id) => <Input id={id} inputMode="decimal" value={fx} onChange={(e) => setFx(e.target.value)} placeholder="5.80" />}
        </Field>
      )}
      <div className="col-span-full flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={record.isPending}>
          {record.isPending ? 'Recording…' : (submitLabel ?? `Record ${side}`)}
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}
