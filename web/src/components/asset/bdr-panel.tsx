import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput } from '@/lib/format'
import { assetsQuery } from '@/lib/queries'

/**
 * Links a BDR to the US asset it represents. Rating, thesis, fair value and the
 * buy gate then come from the underlying; the fair value is converted with the
 * ratio and USD/BRL.
 */
export function BdrPanel({ asset: a }: { asset: Schemas['Asset'] }) {
  const qc = useQueryClient()
  const assets = useQuery(assetsQuery)
  const [under, setUnder] = useState(a.underlying_asset_id ? String(a.underlying_asset_id) : '')
  const [ratio, setRatio] = useState(a.bdr_ratio)
  const [error, setError] = useState<string | null>(null)
  const candidates = (assets.data ?? []).filter((x) => x.market === 'US')
  const save = useMutation({
    mutationFn: (body: Schemas['AssetUpdate']) =>
      unwrap(api.PATCH('/api/assets/{id}', { params: { path: { id: a.id } }, body })),
    onSuccess: () => {
      setError(null)
      toast.success('BDR link saved')
      void qc.invalidateQueries()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <Panel title="Underlying US asset">
      <p className="text-muted-foreground mb-3 text-sm">
        Rating, thesis, fair value and the buy gate come from the underlying. Targets and caps count both as one position.
      </p>
      <form
        className="flex flex-wrap items-end gap-3"
        noValidate
        onSubmit={(e) => {
          e.preventDefault()
          if (!under) return save.mutate({ underlying_asset_id: 0 })
          const r = decimalInput(ratio)
          if (!r || Number(r) <= 0) return setError('Enter how many BDRs equal one underlying share.')
          save.mutate({ underlying_asset_id: Number(under), bdr_ratio: r })
        }}
      >
        <Field label="Underlying" className="w-48">
          {(id) => (
            <NativeSelect id={id} value={under} onChange={(e) => setUnder(e.target.value)}>
              <option value="">None</option>
              {candidates.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.ticker}
                </option>
              ))}
            </NativeSelect>
          )}
        </Field>
        <Field label="BDRs per share" className="w-36">
          {(id) => <Input id={id} inputMode="decimal" value={ratio} onChange={(e) => setRatio(e.target.value)} placeholder="20" />}
        </Field>
        <Button type="submit" size="sm" disabled={save.isPending}>
          Save
        </Button>
        <FormError error={error} />
      </form>
    </Panel>
  )
}
