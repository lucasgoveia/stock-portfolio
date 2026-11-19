import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useDeferredValue, useState } from 'react'
import { toast } from 'sonner'

import { BreakdownTable, InitiativesEditor, BasisTable, type MarginHints } from '@/components/asset/dcf-parts'
import {
  SCENARIOS,
  blankDraft,
  defaultTax,
  draftFrom,
  toInput,
  type DCFDraft,
  type DCFInput,
  type ScenarioDraft,
  type ScenarioName,
} from '@/components/asset/dcf-draft'
import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { decimalInput, fractionToPercent, humanize, money, scaled, today, upside } from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type FairValue = Schemas['FairValue']

const METHOD: Record<string, string> = {
  scenario_dcf: 'Scenario DCF',
  bazin: 'Bazin',
  graham: 'Graham',
  manual: 'Manual',
  target_price: 'Target price',
}

export function ValuationTab({ assetId, currency, price }: { assetId: number; currency: string; price?: string | null }) {
  const versions = useQuery({
    queryKey: [...qk.asset(assetId), 'fair-values'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/fair-values', { params: { path: { id: assetId } } })),
  })
  const [draft, setDraft] = useState<DCFDraft>(() => blankDraft(defaultTax(currency)))
  const [editorKey, setEditorKey] = useState(0)
  const loadIntoEditor = (fv: FairValue) => {
    const dcf = (fv.inputs as { dcf?: DCFInput } | null)?.dcf
    if (!dcf) return
    setDraft(draftFrom(dcf, defaultTax(currency)))
    setEditorKey((k) => k + 1)
    toast.info(`Loaded the ${fv.as_of} model into the editor`)
  }
  return (
    <div className="space-y-4">
      <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <Panel title="Fair value versions">
          {versions.isPending ? (
            <LoadingBlock rows={2} />
          ) : versions.isError ? (
            <ErrorBlock error={versions.error} />
          ) : (versions.data ?? []).length === 0 ? (
            <p className="text-muted-foreground text-sm">No fair value yet. Build a scenario DCF below or enter one by hand.</p>
          ) : (
            <ul className="divide-y">
              {(versions.data ?? []).map((fv, i) => (
                <VersionRow
                  key={fv.id}
                  fv={fv}
                  current={i === 0}
                  assetId={assetId}
                  price={price}
                  onLoad={() => loadIntoEditor(fv)}
                />
              ))}
            </ul>
          )}
        </Panel>
        <ManualFairValue assetId={assetId} currency={currency} />
      </div>
      <DCFEditor key={editorKey} assetId={assetId} currency={currency} price={price} initial={draft} />
    </div>
  )
}

function VersionRow({
  fv,
  current,
  assetId,
  price,
  onLoad,
}: {
  fv: FairValue
  current: boolean
  assetId: number
  price?: string | null
  onLoad: () => void
}) {
  const qc = useQueryClient()
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/fair-values/{id}', { params: { path: { id: fv.id } } })),
    onSuccess: () => {
      toast.success('Fair value version deleted')
      void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const up = current ? upside(fv.value_per_share, price) : null
  return (
    <li className={cn('flex flex-wrap items-center justify-between gap-2 py-2.5', !current && 'text-muted-foreground')}>
      <div>
        <div className="flex items-baseline gap-2">
          <span className={cn('num', current ? 'text-foreground text-lg font-semibold' : '')}>
            {money(fv.value_per_share, fv.currency)}
          </span>
          {up && <span className="num text-sm">{up} vs price</span>}
          {fv.stale && <span className="text-breach text-xs font-medium">stale</span>}
        </div>
        <div className="text-xs">
          {METHOD[fv.method] ?? humanize(fv.method)} · {fv.as_of}
          {fv.note ? ` · ${fv.note}` : ''}
        </div>
      </div>
      <div className="flex gap-1">
        {fv.method === 'scenario_dcf' && (
          <Button size="xs" variant="ghost" onClick={onLoad}>
            Edit a copy
          </Button>
        )}
        <Button size="icon-xs" variant="ghost" aria-label={`Delete fair value of ${fv.as_of}`} disabled={del.isPending} onClick={() => del.mutate()}>
          <Trash2 />
        </Button>
      </div>
    </li>
  )
}

function ManualFairValue({ assetId, currency }: { assetId: number; currency: string }) {
  const qc = useQueryClient()
  const [method, setMethod] = useState('manual')
  const [value, setValue] = useState('')
  const [asOf, setAsOf] = useState(today())
  const [note, setNote] = useState('')
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: (v: string) =>
      unwrap(
        api.POST('/api/assets/{id}/fair-values', {
          params: { path: { id: assetId } },
          body: { method, value_per_share: v, as_of: asOf, note, currency },
        }),
      ),
    onSuccess: (fv) => {
      toast.success(`Fair value set to ${money(fv.value_per_share, fv.currency)}`)
      setValue('')
      setNote('')
      setError(null)
      void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <Panel title="Enter a fair value">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const v = decimalInput(value)
          if (!v || Number(v) <= 0) return setError('Enter a value per share.')
          save.mutate(v)
        }}
        className="grid grid-cols-2 gap-3"
        noValidate
      >
        <Field label="Method">
          {(id) => (
            <NativeSelect id={id} value={method} onChange={(e) => setMethod(e.target.value)}>
              <option value="manual">Manual</option>
              <option value="bazin">Bazin</option>
              <option value="graham">Graham</option>
              <option value="target_price">Target price</option>
            </NativeSelect>
          )}
        </Field>
        <Field label={`Per share (${currency})`}>
          {(id) => <Input id={id} inputMode="decimal" value={value} onChange={(e) => setValue(e.target.value)} />}
        </Field>
        <Field label="As of">{(id) => <Input id={id} type="date" value={asOf} onChange={(e) => setAsOf(e.target.value)} />}</Field>
        <Field label="Note">{(id) => <Input id={id} value={note} onChange={(e) => setNote(e.target.value)} />}</Field>
        <div className="col-span-2 flex flex-wrap items-center gap-3">
          <Button type="submit" variant="outline" disabled={save.isPending}>
            Save fair value
          </Button>
          <FormError error={error} />
        </div>
      </form>
    </Panel>
  )
}

function DCFEditor({
  assetId,
  currency,
  price,
  initial,
}: {
  assetId: number
  currency: string
  price?: string | null
  initial: DCFDraft
}) {
  const qc = useQueryClient()
  const [d, setD] = useState<DCFDraft>(initial)
  const [sources, setSources] = useState('')
  const [note, setNote] = useState('')
  const [saveError, setSaveError] = useState<string | null>(null)
  const { input, error } = toInput(d)
  const fundamentals = useQuery({
    queryKey: [...qk.asset(assetId), 'fundamentals', 'margins'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/fundamentals', { params: { path: { id: assetId } } })),
    staleTime: 5 * 60_000,
  })
  const hints = marginHints(fundamentals.data?.history)
  const deferred = useDeferredValue(input)
  const preview = useQuery({
    queryKey: ['dcf-preview', deferred],
    queryFn: () => unwrap(api.POST('/api/valuation/dcf', { body: deferred as DCFInput })),
    enabled: deferred != null,
    retry: false,
  })
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/assets/{id}/fair-values', {
          params: { path: { id: assetId } },
          body: {
            method: 'scenario_dcf',
            dcf: input,
            currency,
            note,
            sources: sources
              .split('\n')
              .map((s) => s.trim())
              .filter(Boolean),
          },
        }),
      ),
    onSuccess: (fv) => {
      toast.success(`Fair value saved: ${money(fv.value_per_share, fv.currency)}`)
      setSaveError(null)
      void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
      void qc.invalidateQueries({ queryKey: qk.portfolio })
    },
    onError: (e) => setSaveError(errorMessage(e)),
  })
  const setScenario = (n: ScenarioName, patch: Partial<ScenarioDraft>) =>
    setD((prev) => ({ ...prev, scenarios: { ...prev.scenarios, [n]: { ...prev.scenarios[n], ...patch } } }))
  const probTotal = SCENARIOS.reduce((sum, n) => sum + scaled(d.scenarios[n].probability, 2), 0)
  const result = input && preview.data ? preview.data : null

  return (
    <Panel title="Scenario DCF">
      <p className="text-muted-foreground mb-4 max-w-prose text-xs">
        Revenue grows for five years, then earns the terminal margin and is sold at the exit multiple, discounted back
        to today, together with the cash returned along the way. Pick the basis that matches your margin and multiple:
        EBIT with EV/EBIT subtracts net debt; net income with P/E does not. Rates are in percent. Growth can come from
        research and exceed history.
      </p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <Field label={`Revenue, last year (${currency})`}>
          {(id) => (
            <Input id={id} inputMode="decimal" value={d.baseRevenue} onChange={(e) => setD({ ...d, baseRevenue: e.target.value })} />
          )}
        </Field>
        <Field label="Shares outstanding">
          {(id) => <Input id={id} inputMode="decimal" value={d.shares} onChange={(e) => setD({ ...d, shares: e.target.value })} />}
        </Field>
        <Field label={`Net debt (${currency})`} hint="Negative for net cash.">
          {(id) => <Input id={id} inputMode="decimal" value={d.netDebt} onChange={(e) => setD({ ...d, netDebt: e.target.value })} />}
        </Field>
      </div>

      <div className="-mx-4 mt-5 overflow-x-auto px-4">
        <table className="w-full min-w-[34rem] text-sm">
          <thead>
            <tr className="text-muted-foreground text-xs">
              <th className="py-1 text-left font-medium">Scenario</th>
              {[1, 2, 3, 4, 5].map((y) => (
                <th key={y} className="px-1 py-1 font-medium">
                  Y{y} growth %
                </th>
              ))}
              <th className="px-1 py-1 font-medium">Terminal margin %</th>
              <th className="px-1 py-1 font-medium">Exit ×</th>
              <th className="px-1 py-1 font-medium">Discount %</th>
              <th className="px-1 py-1 font-medium">Prob. %</th>
            </tr>
          </thead>
          <tbody>
            {SCENARIOS.map((n) => {
              const s = d.scenarios[n]
              const cell = 'h-8 px-1.5 text-right num'
              return (
                <tr key={n}>
                  <th scope="row" className="py-1 pr-2 text-left font-medium capitalize">
                    {n}
                  </th>
                  {s.growth.map((g, i) => (
                    <td key={i} className="px-0.5 py-1">
                      <Input
                        aria-label={`${n} year ${i + 1} growth`}
                        className={cell}
                        inputMode="decimal"
                        value={g}
                        onChange={(e) => {
                          const growth = [...s.growth]
                          growth[i] = e.target.value
                          setScenario(n, { growth })
                        }}
                      />
                    </td>
                  ))}
                  <td className="px-0.5 py-1">
                    <Input aria-label={`${n} terminal margin`} className={cell} inputMode="decimal" value={s.margin} onChange={(e) => setScenario(n, { margin: e.target.value })} />
                  </td>
                  <td className="px-0.5 py-1">
                    <Input aria-label={`${n} exit multiple`} className={cell} inputMode="decimal" value={s.multiple} onChange={(e) => setScenario(n, { multiple: e.target.value })} />
                  </td>
                  <td className="px-0.5 py-1">
                    <Input aria-label={`${n} discount rate`} className={cell} inputMode="decimal" value={s.discount} onChange={(e) => setScenario(n, { discount: e.target.value })} />
                  </td>
                  <td className="px-0.5 py-1">
                    <Input
                      aria-label={`${n} probability`}
                      aria-invalid={probTotal !== 10000}
                      className={cell}
                      inputMode="decimal"
                      value={s.probability}
                      onChange={(e) => setScenario(n, { probability: e.target.value })}
                    />
                  </td>
                </tr>
              )
            })}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={9} />
              <td className={cn('num px-1.5 pt-1 text-right text-xs', probTotal === 10000 ? 'text-jade' : 'text-breach')}>
                Σ {(probTotal / 100).toString()}%
              </td>
            </tr>
          </tfoot>
        </table>
      </div>

      <BasisTable scenarios={d.scenarios} hints={hints} onChange={setScenario} />
      <InitiativesEditor initiatives={d.initiatives} currency={currency} onChange={(initiatives) => setD((prev) => ({ ...prev, initiatives }))} />

      <div className="mt-5 grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div aria-live="polite">
          {error ? (
            <p className="text-muted-foreground text-sm">{error}</p>
          ) : preview.isError ? (
            <p className="text-breach text-sm">{errorMessage(preview.error)}</p>
          ) : result ? (
            <div>
              <div className="eyebrow">Fair value per share</div>
              <div className="flex items-baseline gap-3">
                <span className="display num text-4xl font-semibold">{money(result.value_per_share, currency)}</span>
                {upside(result.value_per_share, price) && (
                  <span className="num text-muted-foreground text-sm">{upside(result.value_per_share, price)} vs price</span>
                )}
              </div>
              <BreakdownTable result={result} currency={currency} draft={d} />
            </div>
          ) : (
            <p className="text-muted-foreground text-sm">Computing…</p>
          )}
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (!input) return setSaveError(error ?? 'Complete the model first.')
            save.mutate()
          }}
          className="space-y-3"
          noValidate
        >
          <Field label="Sources" hint="One per line: filings, reports, links.">
            {(id) => (
              <textarea
                id={id}
                rows={2}
                value={sources}
                onChange={(e) => setSources(e.target.value)}
                className="border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 w-full rounded-lg border bg-transparent px-2.5 py-1.5 text-sm outline-none focus-visible:ring-3"
              />
            )}
          </Field>
          <Field label="Note">{(id) => <Input id={id} value={note} onChange={(e) => setNote(e.target.value)} />}</Field>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={save.isPending || !input}>
              Save as new fair value
            </Button>
            <FormError error={saveError} />
          </div>
        </form>
      </div>
    </Panel>
  )
}

/** Latest reported EBIT and net margins (percent) from the fundamentals history. */
function marginHints(history: Schemas['HistoryPoint'][] | null | undefined): MarginHints {
  const last = [...(history ?? [])].sort((a, b) => a.period_end.localeCompare(b.period_end)).at(-1)
  return {
    ebit: last?.ebit_margin ? fractionToPercent(last.ebit_margin) : undefined,
    net: last?.net_margin ? fractionToPercent(last.net_margin) : undefined,
  }
}
