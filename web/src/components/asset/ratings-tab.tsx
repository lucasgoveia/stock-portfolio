import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, ApiError, errorMessage, unwrap } from '@/lib/api'
import { today } from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

const FACTORS = [
  { key: 'volatility', label: 'Volatility', help: 'Low beta scores high. Auto follows the weekly beta.' },
  { key: 'understanding', label: 'Understanding', help: 'How well you understand the business.' },
  { key: 'moat', label: 'Moat', help: 'How hard it is to take market share away.' },
  { key: 'predictability', label: 'Predictability', help: 'Recurring revenue scores high; commodity-driven scores low.' },
  { key: 'horizon', label: 'Horizon', help: 'How much it makes sense to hold for the long term.' },
] as const

type FactorKey = (typeof FACTORS)[number]['key']

export function RatingsTab({ assetId }: { assetId: number }) {
  const qc = useQueryClient()
  const history = useQuery({
    queryKey: [...qk.asset(assetId), 'ratings'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/ratings', { params: { path: { id: assetId } } })),
  })
  const beta = useQuery({
    queryKey: [...qk.asset(assetId), 'beta'],
    queryFn: async () => {
      try {
        return await unwrap(api.GET('/api/assets/{id}/beta', { params: { path: { id: assetId } } }))
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
  const latest = history.data?.[0]
  const [values, setValues] = useState<Record<FactorKey, number>>(() => ({
    volatility: 0,
    understanding: 0,
    moat: 0,
    predictability: 0,
    horizon: 0,
  }))
  const [seeded, setSeeded] = useState(false)
  if (latest && !seeded) {
    // Start the form from the current rating so a change is one click.
    setSeeded(true)
    setValues({
      volatility: latest.volatility_auto ? 0 : latest.volatility,
      understanding: latest.understanding,
      moat: latest.moat,
      predictability: latest.predictability,
      horizon: latest.horizon,
    })
  }
  const [ratedOn, setRatedOn] = useState(today())
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const add = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/assets/{id}/ratings', {
          params: { path: { id: assetId } },
          body: { ...values, rated_on: ratedOn, reason },
        }),
      ),
    onSuccess: (r) => {
      toast.success(`Rating saved: score ${Number(r.score).toFixed(2)}`)
      setReason('')
      setError(null)
      void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    const missing = FACTORS.filter((f) => f.key !== 'volatility' && !values[f.key])
    if (missing.length) return setError(`Rate ${missing.map((f) => f.label.toLowerCase()).join(', ')} from 1 to 5.`)
    if (!values.volatility && !beta.data) return setError('No beta yet (needs a year of history): rate volatility by hand.')
    if (!reason.trim()) return setError('Say why, e.g. "initial rating" or "moat weaker after new entrant".')
    add.mutate()
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <Panel title={latest ? 'Change rating' : 'Rate this asset'}>
        <form onSubmit={submit} className="space-y-4" noValidate>
          {FACTORS.map((f) => (
            <fieldset key={f.key}>
              <legend className="text-sm font-medium">{f.label}</legend>
              <p className="text-muted-foreground mb-1.5 text-xs">{f.help}</p>
              <div className="flex flex-wrap gap-1">
                {f.key === 'volatility' && (
                  <ScoreButton
                    active={values.volatility === 0}
                    onClick={() => setValues((v) => ({ ...v, volatility: 0 }))}
                    label={beta.data ? `Auto (${beta.data.volatility_score})` : 'Auto'}
                  />
                )}
                {[1, 2, 3, 4, 5].map((n) => (
                  <ScoreButton
                    key={n}
                    active={values[f.key] === n}
                    onClick={() => setValues((v) => ({ ...v, [f.key]: n }))}
                    label={String(n)}
                  />
                ))}
              </div>
            </fieldset>
          ))}
          <div className="grid grid-cols-2 gap-3">
            <Field label="Rated on">
              {(id) => <Input id={id} type="date" value={ratedOn} onChange={(e) => setRatedOn(e.target.value)} />}
            </Field>
            <Field label="Reason" className="col-span-2 sm:col-span-1">
              {(id) => (
                <Input id={id} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="What changed?" />
              )}
            </Field>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={add.isPending}>
              Save rating
            </Button>
            <FormError error={error} />
          </div>
        </form>
      </Panel>
      <div className="space-y-4">
        <Panel title="Beta">
          {beta.isPending ? (
            <LoadingBlock rows={1} />
          ) : beta.data == null ? (
            <p className="text-muted-foreground text-sm">
              Not computed yet. Beta needs a year of weekly prices; the daily job computes it.
            </p>
          ) : (
            <dl className="grid grid-cols-2 gap-2 text-sm">
              <dt className="text-muted-foreground">Beta vs {beta.data.index_code}</dt>
              <dd className="num text-right">{Number(beta.data.beta).toFixed(2)}</dd>
              <dt className="text-muted-foreground">Volatility score</dt>
              <dd className="num text-right">{beta.data.volatility_score}</dd>
              <dt className="text-muted-foreground">Weeks</dt>
              <dd className="num text-right">{beta.data.weeks}</dd>
              <dt className="text-muted-foreground">Computed</dt>
              <dd className="num text-right">{beta.data.computed_on}</dd>
            </dl>
          )}
        </Panel>
        <Panel title="History">
          {history.isPending ? (
            <LoadingBlock rows={2} />
          ) : history.isError ? (
            <ErrorBlock error={history.error} />
          ) : (history.data ?? []).length === 0 ? (
            <p className="text-muted-foreground text-sm">No ratings yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Date</TableHead>
                  <TableHead className="text-right" title="Volatility, understanding, moat, predictability, horizon">
                    V U M P H
                  </TableHead>
                  <TableHead className="text-right">Score</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(history.data ?? []).map((r) => (
                  <TableRow key={r.id}>
                    <TableCell>
                      <div className="num">{r.rated_on}</div>
                      {r.reason && <div className="text-muted-foreground max-w-48 truncate text-xs">{r.reason}</div>}
                    </TableCell>
                    <TableCell className="num text-right">
                      {r.volatility}
                      {r.volatility_auto ? '*' : ''} {r.understanding} {r.moat} {r.predictability} {r.horizon}
                    </TableCell>
                    <TableCell className="num text-right font-medium">{Number(r.score).toFixed(2)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {(history.data ?? []).some((r) => r.volatility_auto) && (
            <p className="text-muted-foreground mt-2 text-xs">* volatility set from beta.</p>
          )}
        </Panel>
      </div>
    </div>
  )
}

function ScoreButton({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        'num focus-visible:ring-ring/50 h-8 min-w-8 rounded-md border px-2 text-sm transition-colors outline-none focus-visible:ring-3',
        active ? 'bg-primary text-primary-foreground border-primary' : 'hover:bg-accent',
      )}
    >
      {label}
    </button>
  )
}
