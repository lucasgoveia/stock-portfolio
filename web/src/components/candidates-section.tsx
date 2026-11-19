import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { FormError } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime, humanize, money, pct } from '@/lib/format'
import { candidatesQuery, mcpDraftsQuery, qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Candidate = Schemas['Candidate']

const STATUS_STYLE: Record<string, string> = {
  pending: 'border-brass/50 text-brass',
  accepted: 'border-jade/50 text-jade',
  rejected: 'text-muted-foreground',
}

/** Candidates proposed by Claude over MCP, and the MCP drafts awaiting review. */
export function CandidatesSection() {
  const list = useQuery(candidatesQuery)
  const drafts = useQuery(mcpDraftsQuery)
  const [open, setOpen] = useState<Candidate | null>(null)
  const items = list.data ?? []
  const pending = items.filter((c) => c.status === 'pending')
  const decided = items.filter((c) => c.status !== 'pending')
  return (
    <Panel title="Candidates from Claude (MCP)" className="mb-6">
      {list.isPending ? (
        <LoadingBlock rows={2} />
      ) : list.isError ? (
        <ErrorBlock error={list.error} />
      ) : items.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No candidates. In Claude, use the <code>research_ticker</code> prompt to research a ticker that is not here and
          propose it; it lands in this list for you to accept or reject. Set it up in{' '}
          <Link to="/settings" className="underline">
            Settings
          </Link>
          .
        </p>
      ) : (
        <ul className="divide-y">
          {[...pending, ...decided].map((c) => (
            <CandidateRow key={c.id} c={c} onOpen={() => setOpen(c)} />
          ))}
        </ul>
      )}
      {(drafts.data ?? []).length > 0 && (
        <div className="mt-4 border-t pt-3">
          <div className="eyebrow mb-2">MCP analyses awaiting review</div>
          <ul className="space-y-1 text-sm">
            {(drafts.data ?? []).map((r) => (
              <li key={r.id} className="flex flex-wrap items-baseline gap-2">
                <Link to="/assets/$assetId" params={{ assetId: String(r.asset_id) }} search={{ tab: 'analyst' }} className="font-medium underline">
                  {r.inputs.ticker}
                </Link>
                <span className="text-muted-foreground num text-xs">
                  draft #{r.id} · {r.model} · {dateTime(r.created_at)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Dialog open={open != null} onOpenChange={(o) => !o && setOpen(null)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          {open && <CandidateDetail c={open} onDone={() => setOpen(null)} />}
        </DialogContent>
      </Dialog>
    </Panel>
  )
}

function useDecide(onDone?: () => void) {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['review'] })
    void qc.invalidateQueries({ queryKey: qk.portfolio })
    void qc.invalidateQueries({ queryKey: qk.assets })
  }
  const accept = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/api/candidates/{id}/accept', { params: { path: { id } } })),
    onSuccess: (c) => {
      toast.success(`${c.ticker} added to the watchlist, with its draft ready to review`)
      setError(null)
      refresh()
      onDone?.()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const reject = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/api/candidates/{id}/reject', { params: { path: { id } } })),
    onSuccess: (c) => {
      toast.success(`${c.ticker} rejected`)
      setError(null)
      refresh()
      onDone?.()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return { accept, reject, error, busy: accept.isPending || reject.isPending }
}

function CandidateRow({ c, onOpen }: { c: Candidate; onOpen: () => void }) {
  const d = useDecide()
  const range = c.output.fair_value_range
  const hasRange = Number(range?.high ?? 0) > 0
  return (
    <li className="py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <span className="display text-lg font-semibold">{c.ticker}</span>{' '}
          <span className="text-muted-foreground text-sm">
            {c.name || c.market} · {c.market}
          </span>
          <span className={cn('ml-2 rounded-full border px-2 text-xs', STATUS_STYLE[c.status])}>{humanize(c.status)}</span>
          <p className="text-muted-foreground num text-xs">
            via MCP · {c.provenance.mcp.client} · {c.provenance.mcp.model} · {dateTime(c.created_at)}
            {hasRange ? ` · FV ${money(range.low, range.currency)} – ${money(range.high, range.currency)}` : ''}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={onOpen}>
            View details
          </Button>
          {c.status === 'pending' && (
            <>
              <Button size="sm" disabled={d.busy} onClick={() => d.accept.mutate(c.id)}>
                Accept
              </Button>
              <Button variant="outline" size="sm" disabled={d.busy} onClick={() => d.reject.mutate(c.id)}>
                Reject
              </Button>
            </>
          )}
          {c.status === 'accepted' && c.asset_id != null && (
            <Button variant="outline" size="sm" render={<Link to="/assets/$assetId" params={{ assetId: String(c.asset_id) }} search={{ tab: 'analyst' }} />}>
              Review draft
            </Button>
          )}
        </div>
      </div>
      {c.note && <p className="text-muted-foreground mt-1 line-clamp-2 max-w-prose text-sm">{c.note}</p>}
      <FormError error={d.error} />
    </li>
  )
}

function CandidateDetail({ c, onDone }: { c: Candidate; onDone: () => void }) {
  const d = useDecide(onDone)
  const o = c.output
  const external = c.provenance.mcp.external_urls ?? []
  const hasScenarios = (o.scenarios?.base?.revenue_growth ?? []).length > 0
  return (
    <>
      <DialogHeader>
        <DialogTitle>
          {c.ticker} <span className="text-muted-foreground text-base font-normal">{c.name}</span>
        </DialogTitle>
        <DialogDescription>
          {c.market} · {c.market === 'B3' ? `CNPJ ${c.cnpj}` : c.cik ? `CIK ${c.cik}` : 'CIK resolved on accept'} · via MCP ·{' '}
          {c.provenance.mcp.client} · {c.provenance.mcp.model}
        </DialogDescription>
      </DialogHeader>
      <div className="min-w-0 space-y-4 text-sm">
        {c.note && (
          <section>
            <div className="eyebrow mb-1">Research note</div>
            <p className="whitespace-pre-wrap">{c.note}</p>
          </section>
        )}
        {hasScenarios && (
          <section>
            <div className="eyebrow mb-1">Scenarios</div>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-muted-foreground text-left text-xs">
                    <th className="py-1 pr-3 font-medium">Scenario</th>
                    <th className="py-1 pr-3 text-right font-medium">Prob.</th>
                    <th className="py-1 pr-3 text-right font-medium">Growth, years 1 to 5</th>
                    <th className="py-1 text-right font-medium">Exit</th>
                  </tr>
                </thead>
                <tbody>
                  {(['bear', 'base', 'bull'] as const).map((k) => (
                    <tr key={k} className="border-t">
                      <td className="py-1.5 pr-3 font-medium">{humanize(k)}</td>
                      <td className="num py-1.5 pr-3 text-right">{pct(o.scenarios[k].probability, 0)}</td>
                      <td className="num py-1.5 pr-3 text-right whitespace-nowrap">
                        {(o.scenarios[k].revenue_growth ?? []).map((g) => pct(g, 1)).join(' · ')}
                      </td>
                      <td className="num py-1.5 text-right">{o.scenarios[k].exit_multiple}×</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}
        {Number(o.fair_value_range?.high ?? 0) > 0 && (
          <section>
            <div className="eyebrow mb-1">Fair value range</div>
            <p className="num text-base">
              {money(o.fair_value_range.low, o.fair_value_range.currency)} –{' '}
              {money(o.fair_value_range.high, o.fair_value_range.currency)}
            </p>
          </section>
        )}
        {(o.sources ?? []).length > 0 && (
          <section>
            <div className="eyebrow mb-1">Sources</div>
            <ul className="text-muted-foreground space-y-0.5 text-xs">
              {(o.sources ?? []).map((s) => (
                <li key={s.url} className="flex min-w-0 items-baseline gap-2">
                  <a href={s.url} target="_blank" rel="noreferrer" className="min-w-0 truncate underline">
                    {s.url}
                  </a>
                  {external.includes(s.url) && (
                    <span className="border-brass/50 text-brass shrink-0 rounded-full border px-1.5 text-[0.65rem] font-medium">
                      external, unverified
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </section>
        )}
        <p className="text-muted-foreground text-xs">
          {(c.provenance.documents ?? []).length} app-served document{(c.provenance.documents ?? []).length === 1 ? '' : 's'} read in the
          session. Accepting creates the watchlist asset and a draft of the note, scenarios and fair value range for you to
          approve on its AI analyst tab; nothing is applied automatically.
        </p>
        {c.status === 'pending' && (
          <div className="flex flex-wrap gap-2">
            <Button disabled={d.busy} onClick={() => d.accept.mutate(c.id)}>
              Accept
            </Button>
            <Button variant="outline" disabled={d.busy} onClick={() => d.reject.mutate(c.id)}>
              Reject
            </Button>
          </div>
        )}
        <FormError error={d.error} />
      </div>
    </>
  )
}
