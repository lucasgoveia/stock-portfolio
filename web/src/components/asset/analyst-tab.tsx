import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { FormError } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime, humanize, money, multiple, pct } from '@/lib/format'
import { analystUsageQuery, qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Asset = Schemas['Asset']
type Run = Schemas['AnalystRun']

const STATUS_STYLE: Record<string, string> = {
  draft: 'border-brass/50 text-brass',
  approved: 'border-jade/50 text-jade',
  discarded: 'text-muted-foreground',
  failed: 'border-breach/50 text-breach',
}

const CHECK_STYLE: Record<string, string> = {
  holds: 'border-jade/50 text-jade',
  at_risk: 'border-brass/50 text-brass',
  broken: 'border-breach/50 text-breach',
}

export function AnalystTab({ asset }: { asset: Asset }) {
  const runs = useQuery({
    queryKey: [...qk.asset(asset.id), 'analyst-runs'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/analyst/runs', { params: { path: { id: asset.id } } })),
  })
  const [picked, setPicked] = useState<number | null>(null)
  const list = runs.data ?? []
  const selected = list.find((r) => r.id === picked) ?? list[0]
  return (
    <div className="space-y-4">
      <RunPanel asset={asset} onStarted={(id) => setPicked(id)} />
      {runs.isPending ? (
        <LoadingBlock rows={3} />
      ) : runs.isError ? (
        <ErrorBlock error={runs.error} />
      ) : list.length === 0 ? (
        <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
          No analyst runs yet. A run reads the thesis, fair value, fundamentals and latest filings and proposes a draft
          for you to review. Nothing is saved until you approve it.
        </p>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
          <RunList runs={list} selectedId={selected?.id} onPick={setPicked} />
          {selected && <RunView key={selected.id} run={selected} asset={asset} />}
        </div>
      )}
    </div>
  )
}

function RunPanel({ asset, onStarted }: { asset: Asset; onStarted: (id: number) => void }) {
  const qc = useQueryClient()
  const usage = useQuery(analystUsageQuery)
  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const start = useMutation({
    mutationFn: () => unwrap(api.POST('/api/assets/{id}/analyst/runs', { params: { path: { id: asset.id } } })),
    onSuccess: (r) => {
      setConfirm(false)
      setError(null)
      toast.success('Analyst draft ready for review')
      onStarted(r.id)
    },
    onError: (e) => {
      setConfirm(false)
      setError(errorMessage(e))
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: qk.asset(asset.id) })
      void qc.invalidateQueries({ queryKey: ['analyst'] })
      void qc.invalidateQueries({ queryKey: ['settings', 'status'] })
    },
  })
  const u = usage.data
  return (
    <Panel title="AI analyst">
      {usage.isPending ? (
        <LoadingBlock rows={1} />
      ) : usage.isError ? (
        <ErrorBlock error={usage.error} />
      ) : (
        u && (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0 text-sm">
              <p>
                <span className="num">{money(u.spent_usd, 'USD')}</span> of{' '}
                <span className="num">{money(u.cap_usd, 'USD')}</span> used in {u.month}
                <span className="text-muted-foreground"> · {u.runs} runs · {u.model}</span>
              </p>
              <p className="text-muted-foreground mt-0.5 text-xs">
                On demand only. A run drafts catalysts, scenarios and a thesis check from filings; nothing is applied until
                you approve it.
              </p>
            </div>
            <Button disabled={!u.can_run || start.isPending} onClick={() => setConfirm(true)}>
              {start.isPending ? 'Analysing…' : 'Run analysis'}
            </Button>
          </div>
        )
      )}
      {u && !u.can_run && (
        <p role="status" className="text-breach mt-3 text-sm">
          {u.blocked_reason}
        </p>
      )}
      {start.isPending && (
        <p role="status" className="text-muted-foreground mt-3 text-sm">
          Reading filings and asking the model. This usually takes one to two minutes; you can leave this page open.
        </p>
      )}
      <FormError error={error} />
      <Dialog open={confirm} onOpenChange={(o) => !start.isPending && setConfirm(o)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Run the analyst on {asset.ticker}?</DialogTitle>
            <DialogDescription>
              This calls the Anthropic API with {u?.model ?? 'the configured model'} and spends money, typically a few
              cents to a few tens of cents. Month to date: {money(u?.spent_usd, 'USD')} of {money(u?.cap_usd, 'USD')}.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" disabled={start.isPending} onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button disabled={start.isPending} onClick={() => start.mutate()}>
              {start.isPending ? 'Analysing…' : 'Run and spend'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Panel>
  )
}

function RunList({ runs, selectedId, onPick }: { runs: Run[]; selectedId?: number; onPick: (id: number) => void }) {
  return (
    <nav aria-label="Analyst runs" className="flex gap-2 overflow-x-auto pb-1 lg:flex-col lg:overflow-visible">
      {runs.map((r) => (
        <button
          key={r.id}
          onClick={() => onPick(r.id)}
          aria-current={r.id === selectedId}
          className={cn(
            'bg-card shrink-0 rounded-lg border p-3 text-left text-sm transition-colors',
            r.id === selectedId ? 'border-foreground' : 'hover:border-foreground/40',
          )}
        >
          <div className="flex items-center justify-between gap-2">
            <span className="num text-xs">{dateTime(r.created_at)}</span>
            <span className={cn('rounded-full border px-2 text-xs', STATUS_STYLE[r.status])}>{humanize(r.status)}</span>
          </div>
          <div className="text-muted-foreground num mt-1 text-xs">
            {money(r.cost_usd, 'USD')} · {Number(r.input_tokens + r.output_tokens).toLocaleString('pt-BR')} tokens
          </div>
        </button>
      ))}
    </nav>
  )
}

function Check({
  checked,
  disabled,
  onChange,
  children,
}: {
  checked: boolean
  disabled?: boolean
  onChange: (v: boolean) => void
  children: React.ReactNode
}) {
  return (
    <label className={cn('flex items-start gap-2 text-sm', disabled && 'text-muted-foreground')}>
      <input
        type="checkbox"
        className="accent-jade mt-0.5 size-4 shrink-0"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="min-w-0">{children}</span>
    </label>
  )
}

function Sources({ urls }: { urls: string[] }) {
  if (urls.length === 0) return null
  return (
    <ul className="text-muted-foreground mt-1 space-y-0.5 text-xs">
      {urls.map((u) => (
        <li key={u} className="truncate">
          <a href={u} target="_blank" rel="noreferrer" className="underline">
            {u}
          </a>
        </li>
      ))}
    </ul>
  )
}

function RunView({ run, asset }: { run: Run; asset: Asset }) {
  const qc = useQueryClient()
  const o = run.output
  const ap = run.approvals
  const open = run.status === 'draft' || run.status === 'approved'
  const [catalysts, setCatalysts] = useState<number[]>([])
  const [market, setMarket] = useState(false)
  const [thesis, setThesis] = useState(false)
  const [dcf, setDcf] = useState(false)
  const [range, setRange] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const count = catalysts.length + [market, thesis, dcf, range].filter(Boolean).length
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: qk.asset(asset.id) })
    void qc.invalidateQueries({ queryKey: qk.portfolio })
    void qc.invalidateQueries({ queryKey: ['analyst'] })
  }
  const approve = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/analyst/runs/{id}/approve', {
          params: { path: { id: run.id } },
          body: { catalysts, market_size: market, thesis_check: thesis, scenarios: dcf, fair_value_range: range },
        }),
      ),
    onSuccess: () => {
      toast.success('Approved parts saved')
      setError(null)
      setCatalysts([])
      setMarket(false)
      setThesis(false)
      setDcf(false)
      setRange(false)
      refresh()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const discard = useMutation({
    mutationFn: () => unwrap(api.POST('/api/analyst/runs/{id}/discard', { params: { path: { id: run.id } } })),
    onSuccess: () => {
      toast.success('Draft discarded')
      refresh()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const busy = approve.isPending || discard.isPending

  if (run.status === 'failed' || !o) {
    return (
      <Panel title={`Run #${run.id}`}>
        <p role="alert" className="text-breach text-sm">
          {run.error || 'This run produced no draft.'}
        </p>
        <p className="text-muted-foreground num mt-2 text-xs">
          {dateTime(run.created_at)} · {run.model} · {money(run.cost_usd, 'USD')} spent
        </p>
      </Panel>
    )
  }

  const cur = o.fair_value_range.currency
  const mid = (Number(o.fair_value_range.low) + Number(o.fair_value_range.high)) / 2
  const docs = run.inputs.documents ?? []
  const checks = o.thesis_check ?? []
  const cats = o.catalysts ?? []
  const savedCats = ap.catalysts ?? []
  const sources = o.sources ?? []
  return (
    <div className="min-w-0 space-y-4">
      <Panel
        title={`Run #${run.id}`}
        actions={
          <span className={cn('rounded-full border px-2.5 py-0.5 text-xs font-medium', STATUS_STYLE[run.status])}>
            {humanize(run.status)}
          </span>
        }
      >
        <p className="text-muted-foreground num text-xs">
          {dateTime(run.created_at)} · {run.model} · {money(run.cost_usd, 'USD')} ·{' '}
          {Number(run.input_tokens).toLocaleString('pt-BR')} in / {Number(run.output_tokens).toLocaleString('pt-BR')} out
        </p>
        <details className="mt-2 text-sm">
          <summary className="cursor-pointer">
            {docs.length} document{docs.length === 1 ? '' : 's'} given to the model
          </summary>
          <ul className="mt-2 space-y-1 text-xs">
            {docs.map((d) => (
              <li key={d.url} className="min-w-0">
                <span className="num text-muted-foreground">{d.date}</span> {d.kind}{' '}
                <a href={d.url} target="_blank" rel="noreferrer" className="break-words underline">
                  {d.title}
                </a>
                {d.chars === 0 && <span className="text-muted-foreground"> (link only)</span>}
              </li>
            ))}
            {(run.inputs.skipped ?? []).map((s) => (
              <li key={s} className="text-brass">
                Skipped: {s}
              </li>
            ))}
          </ul>
        </details>
        {run.inputs.dropped_sources > 0 && (
          <p className="text-brass mt-2 text-xs">
            {run.inputs.dropped_sources} citation{run.inputs.dropped_sources === 1 ? '' : 's'} of unprovided documents were removed.
          </p>
        )}
      </Panel>

      <Panel title="Thesis check">
        {checks.length === 0 ? (
          <p className="text-muted-foreground text-sm">No break conditions to check.</p>
        ) : (
          <ul className="space-y-3">
            {checks.map((c, i) => (
              <li key={i} className="text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <span className={cn('rounded-full border px-2 text-xs font-medium', CHECK_STYLE[c.status])}>
                    {humanize(c.status)}
                  </span>
                  <span className="min-w-0 font-medium">{c.condition}</span>
                </div>
                <p className="text-muted-foreground mt-1">{c.evidence}</p>
              </li>
            ))}
          </ul>
        )}
        {checks.length > 0 && (
          <div className="mt-3 border-t pt-3">
            <Check checked={thesis || ap.thesis_check} disabled={!open || ap.thesis_check} onChange={setThesis}>
              Save the thesis check as a research note{ap.thesis_check && ' (saved)'}
            </Check>
          </div>
        )}
      </Panel>

      <Panel title="Catalysts">
        {cats.length === 0 ? (
          <p className="text-muted-foreground text-sm">None found in the provided documents.</p>
        ) : (
          <ul className="space-y-3">
            {cats.map((c, i) => {
              const saved = savedCats.includes(i)
              return (
                <li key={i}>
                  <Check
                    checked={saved || catalysts.includes(i)}
                    disabled={!open || saved}
                    onChange={(v) => setCatalysts((cur) => (v ? [...cur, i] : cur.filter((x) => x !== i)))}
                  >
                    <span className="font-medium">{c.title}</span>
                    {c.expected_date && <span className="num text-muted-foreground"> · {c.expected_date}</span>}
                    {saved && <span className="text-muted-foreground"> · saved as a note</span>}
                    <span className="text-muted-foreground block">{c.detail}</span>
                    <Sources urls={c.source_urls ?? []} />
                  </Check>
                </li>
              )
            })}
          </ul>
        )}
      </Panel>

      <Panel title="Market size">
        <p className="text-sm whitespace-pre-wrap">{o.market_size_notes || '—'}</p>
        {o.market_size_notes && (
          <div className="mt-3 border-t pt-3">
            <Check checked={market || ap.market_size} disabled={!open || ap.market_size} onChange={setMarket}>
              Save as a research note{ap.market_size && ' (saved)'}
            </Check>
          </div>
        )}
      </Panel>

      <Panel title="Scenarios">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-muted-foreground text-left text-xs">
                <th className="py-1 pr-3 font-medium">Scenario</th>
                <th className="py-1 pr-3 text-right font-medium">Prob.</th>
                <th className="py-1 pr-3 text-right font-medium">Growth, years 1 to 5</th>
                <th className="py-1 pr-3 font-medium">Basis</th>
                <th className="py-1 pr-3 text-right font-medium">Margin start → end</th>
                <th className="py-1 pr-3 text-right font-medium">Exit</th>
                <th className="py-1 pr-3 text-right font-medium">Cash conv.</th>
                <th className="py-1 text-right font-medium">Discount</th>
              </tr>
            </thead>
            <tbody>
              {(['bear', 'base', 'bull'] as const).map((k) => {
                const s = o.scenarios[k]
                return (
                  <tr key={k} className="border-t align-top">
                    <td className="py-1.5 pr-3 font-medium">{humanize(k)}</td>
                    <td className="num py-1.5 pr-3 text-right">{pct(s.probability, 0)}</td>
                    <td className="num py-1.5 pr-3 text-right whitespace-nowrap">
                      {(s.revenue_growth ?? []).map((g) => pct(g, 1)).join(' · ')}
                    </td>
                    <td className="py-1.5 pr-3 whitespace-nowrap">{s.basis === 'net_pe' ? 'Net income × P/E' : 'EBIT × EV/EBIT'}</td>
                    <td className="num py-1.5 pr-3 text-right whitespace-nowrap">
                      {s.start_margin != null ? `${pct(s.start_margin, 1)} → ` : ''}
                      {pct(s.terminal_margin, 1)}
                    </td>
                    <td className="num py-1.5 pr-3 text-right">{multiple(s.exit_multiple)}</td>
                    <td className="num py-1.5 pr-3 text-right">{pct(s.cash_conversion ?? '0', 0)}</td>
                    <td className="num py-1.5 text-right">{pct(s.discount_rate, 1)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
        <dl className="mt-3 space-y-2 text-sm">
          {(['bear', 'base', 'bull'] as const).map((k) => (
            <div key={k}>
              <dt className="eyebrow">{humanize(k)}</dt>
              <dd className="text-muted-foreground">{o.scenarios[k].rationale}</dd>
            </div>
          ))}
        </dl>
        {(o.initiatives ?? []).length > 0 && (
          <div className="mt-3 border-t pt-3">
            <div className="eyebrow">New businesses</div>
            <ul className="mt-2 space-y-2 text-sm">
              {(o.initiatives ?? []).map((i) => (
                <li key={i.name}>
                  <span className="font-medium">{i.name}</span>{' '}
                  <span className="text-muted-foreground num text-xs">
                    from year {i.start_year}, {money(i.revenue_year5, cur)} revenue in year 5 at {pct(i.margin, 0)} margin; success{' '}
                    {pct(i.success_probability.bear, 0)} / {pct(i.success_probability.base, 0)} / {pct(i.success_probability.bull, 0)}
                  </span>
                  <p className="text-muted-foreground">{i.rationale}</p>
                </li>
              ))}
            </ul>
          </div>
        )}
        <div className="mt-3 border-t pt-3">
          <Check
            checked={dcf || ap.dcf_fair_value_id != null}
            disabled={!open || ap.dcf_fair_value_id != null || !run.inputs.dcf_base}
            onChange={setDcf}
          >
            Create a scenario DCF fair value version{ap.dcf_fair_value_id != null && ' (saved)'}
            <span className="text-muted-foreground block text-xs">
              {run.inputs.dcf_base
                ? 'Computed on the latest annual revenue, net debt and share count from fundamentals.'
                : 'Needs fundamentals (revenue and share count); fetch them on the Fundamentals tab and run again.'}
            </span>
          </Check>
        </div>
      </Panel>

      <Panel title="Fair value range">
        <p className="num text-lg">
          {money(o.fair_value_range.low, cur)} – {money(o.fair_value_range.high, cur)}
        </p>
        <p className="text-muted-foreground text-xs">Per share. Midpoint {money(String(mid), cur)}.</p>
        <div className="mt-3 border-t pt-3">
          <Check
            checked={range || ap.range_fair_value_id != null}
            disabled={!open || ap.range_fair_value_id != null}
            onChange={setRange}
          >
            Save the midpoint as a manual fair value version{ap.range_fair_value_id != null && ' (saved)'}
          </Check>
        </div>
      </Panel>

      {sources.length > 0 && (
        <Panel title="Sources">
          <Sources urls={sources.map((s) => s.url)} />
        </Panel>
      )}

      {open && (
        <div className="bg-card sticky bottom-2 flex flex-wrap items-center gap-3 rounded-lg border p-3">
          <Button disabled={busy || count === 0} onClick={() => approve.mutate()}>
            Approve {count > 0 ? `${count} selected` : 'selected'}
          </Button>
          {run.status === 'draft' && (
            <Button variant="outline" disabled={busy} onClick={() => discard.mutate()}>
              Discard draft
            </Button>
          )}
          <span className="text-muted-foreground text-xs">Only ticked parts are saved; the rest stays in this draft.</span>
          <FormError error={error} />
        </div>
      )}
    </div>
  )
}
