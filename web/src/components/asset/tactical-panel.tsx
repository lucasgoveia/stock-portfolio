import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, humanize, money, pct } from '@/lib/format'
import { qk } from '@/lib/queries'

type Setup = Schemas['Setup']

const SIGNAL_TEXT: Record<string, string> = {
  tactical_target_hit: 'Target reached',
  tactical_stop_hit: 'Stop reached',
  tactical_expired: 'Expired without the catalyst',
}

/** Setup, sizing and exits of a tactical position (spec §10). */
export function TacticalPanel({
  asset,
  view,
}: {
  asset: Schemas['Asset']
  view?: Schemas['TacticalView']
}) {
  const [editing, setEditing] = useState(false)
  const setup = view?.setup
  return (
    <Panel
      title="Tactical setup"
      actions={
        setup && !editing ? (
          <Button size="xs" variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
        ) : undefined
      }
      className="lg:col-span-2"
    >
      {setup && !editing ? (
        <SetupSummary asset={asset} view={view} />
      ) : (
        <SetupForm asset={asset} setup={setup} onDone={() => setEditing(false)} />
      )}
    </Panel>
  )
}

function SetupSummary({ asset, view }: { asset: Schemas['Asset']; view?: Schemas['TacticalView'] }) {
  const qc = useQueryClient()
  const s = view!.setup
  const promote = useMutation({
    mutationFn: () => unwrap(api.POST('/api/assets/{id}/promote', { params: { path: { id: asset.id } } })),
    onSuccess: () => {
      toast.success(`${asset.ticker} promoted to core`)
      void qc.invalidateQueries()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const signals = view?.signals ?? []
  return (
    <div className="space-y-4">
      {signals.length > 0 && (
        <ul className="flex flex-wrap gap-2">
          {signals.map((sig) => (
            <li key={sig} className="border-brass/50 text-brass rounded-full border px-2.5 py-0.5 text-xs font-medium">
              {SIGNAL_TEXT[sig] ?? humanize(sig)}
            </li>
          ))}
        </ul>
      )}
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        {(
          [
            ['Entry', money(s.entry_price, asset.currency)],
            ['Target', money(s.target_price, asset.currency)],
            ['Stop', money(s.stop_price, asset.currency)],
            ['Expires', s.expires_on],
            ['Size', pct(s.sizing.size)],
            ['Size today', money(view?.suggested_value_brl)],
            ['Stop distance', pct(s.sizing.stop_distance, 1)],
            ['Loss at stop', `${pct(s.sizing.loss_at_stop)} of portfolio`],
          ] as const
        ).map(([k, v]) => (
          <div key={k}>
            <dt className="text-muted-foreground text-xs">{k}</dt>
            <dd className="num">{v}</dd>
          </div>
        ))}
      </dl>
      <p className="text-muted-foreground text-xs">
        No contributions after entry; only the 12% cap applies. You decide every exit.
      </p>
      <div className="flex flex-wrap items-center gap-2 border-t pt-3">
        <Button size="sm" variant="outline" disabled={promote.isPending} onClick={() => promote.mutate()}>
          Promote to core
        </Button>
        <span className="text-muted-foreground text-xs">
          Needs the full core rating, a fresh fair value above the price and a thesis that is not broken.
        </span>
      </div>
    </div>
  )
}

function SetupForm({ asset, setup, onDone }: { asset: Schemas['Asset']; setup?: Setup; onDone: () => void }) {
  const qc = useQueryClient()
  const notes = useQuery({
    queryKey: [...qk.asset(asset.id), 'notes'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/notes', { params: { path: { id: asset.id } } })),
  })
  const [noteId, setNoteId] = useState<number | ''>(setup?.note_id ?? '')
  const [entry, setEntry] = useState(setup?.entry_price ?? '')
  const [target, setTarget] = useState(setup?.target_price ?? '')
  const [stop, setStop] = useState(setup?.stop_price ?? '')
  const [expires, setExpires] = useState(setup?.expires_on ?? '')
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (body: Schemas['TacticalPut']) =>
      unwrap(api.PUT('/api/assets/{id}/tactical', { params: { path: { id: asset.id } }, body })),
    onSuccess: (s) => {
      toast.success(`Setup saved: size ${pct(s.sizing.size)} of the portfolio`)
      setError(null)
      void qc.invalidateQueries({ queryKey: qk.portfolio })
      onDone()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const list = notes.data ?? []
  if (notes.isSuccess && list.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        A tactical position starts from a catalyst. Write it as a{' '}
        <Link to="." search={{ tab: 'notes' }} className="text-foreground underline">
          research note
        </Link>{' '}
        first, then come back to set entry, target, stop and expiry.
      </p>
    )
  }
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        const [en, ta, st] = [decimalInput(entry), decimalInput(target), decimalInput(stop)]
        if (noteId === '') return setError('Choose the catalyst note.')
        if (!en || !ta || !st) return setError('Enter entry, target and stop prices.')
        if (!(Number(st) < Number(en) && Number(en) < Number(ta))) return setError('Prices must satisfy stop < entry < target.')
        if (!expires) return setError('Set the date the catalyst should have played out by.')
        save.mutate({ note_id: noteId, entry_price: en, target_price: ta, stop_price: st, expires_on: expires })
      }}
      className="grid grid-cols-2 gap-3 sm:grid-cols-5"
      noValidate
    >
      <Field label="Catalyst note" className="col-span-2 sm:col-span-5">
        {(id) => (
          <NativeSelect id={id} value={noteId} onChange={(e) => setNoteId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">Choose…</option>
            {list.map((n) => (
              <option key={n.id} value={n.id}>
                {n.text.length > 80 ? `${n.text.slice(0, 80)}…` : n.text}
              </option>
            ))}
          </NativeSelect>
        )}
      </Field>
      <Field label={`Entry (${asset.currency})`}>
        {(id) => <Input id={id} inputMode="decimal" value={entry} onChange={(e) => setEntry(e.target.value)} />}
      </Field>
      <Field label="Target">{(id) => <Input id={id} inputMode="decimal" value={target} onChange={(e) => setTarget(e.target.value)} />}</Field>
      <Field label="Stop">{(id) => <Input id={id} inputMode="decimal" value={stop} onChange={(e) => setStop(e.target.value)} />}</Field>
      <Field label="Expires on" className="col-span-2 sm:col-span-2">
        {(id) => <Input id={id} type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />}
      </Field>
      <div className="col-span-full flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending}>
          Save setup
        </Button>
        {setup && (
          <Button type="button" variant="ghost" onClick={onDone}>
            Cancel
          </Button>
        )}
        <FormError error={error} />
      </div>
      <p className="text-muted-foreground col-span-full text-xs">
        Size = 1% ÷ stop distance, between 1% and 2% of the portfolio, so a stop-out costs at most 1%.
      </p>
    </form>
  )
}
