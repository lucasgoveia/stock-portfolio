import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'

import { Field, FormError, NativeSelect } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap } from '@/lib/api'
import { qk } from '@/lib/queries'

/** Adds an asset to the watchlist and opens it on the thesis tab. */
export function NewAssetForm() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [ticker, setTicker] = useState('')
  const [name, setName] = useState('')
  const [market, setMarket] = useState('B3')
  const [kind, setKind] = useState('stock')
  const [sleeve, setSleeve] = useState('core')
  const [error, setError] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/api/assets', { body: { ticker, name, market, kind, sleeve } })),
    onSuccess: (a) => {
      toast.success(`${a.ticker} added to the watchlist`)
      void qc.invalidateQueries({ queryKey: qk.assets })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
      void navigate({ to: '/assets/$assetId', params: { assetId: String(a.id) }, search: { tab: 'ratings' } })
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (!ticker.trim()) return setError('Enter a ticker, e.g. WEGE3 or MSFT.')
        create.mutate()
      }}
      className="grid grid-cols-2 gap-3 sm:grid-cols-6"
      noValidate
    >
      <Field label="Ticker">
        {(id) => <Input id={id} value={ticker} onChange={(e) => setTicker(e.target.value.toUpperCase())} placeholder="WEGE3" />}
      </Field>
      <Field label="Name" className="sm:col-span-2">
        {(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} placeholder="WEG" />}
      </Field>
      <Field label="Market">
        {(id) => (
          <NativeSelect id={id} value={market} onChange={(e) => setMarket(e.target.value)}>
            <option value="B3">B3</option>
            <option value="US">US</option>
          </NativeSelect>
        )}
      </Field>
      <Field label="Kind">
        {(id) => (
          <NativeSelect id={id} value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="stock">Stock</option>
            <option value="bdr">BDR</option>
            <option value="etf">ETF</option>
          </NativeSelect>
        )}
      </Field>
      <Field label="Sleeve">
        {(id) => (
          <NativeSelect id={id} value={sleeve} onChange={(e) => setSleeve(e.target.value)}>
            <option value="core">Core</option>
            <option value="tactical">Tactical</option>
          </NativeSelect>
        )}
      </Field>
      <div className="col-span-2 flex flex-wrap items-center gap-3 sm:col-span-6">
        <Button type="submit" disabled={create.isPending}>
          Add to watchlist
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}
