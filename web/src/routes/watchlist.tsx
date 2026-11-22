import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'

import { CandidatesSection } from '@/components/candidates-section'
import { GateChecklist } from '@/components/asset/gate-checklist'
import { NewAssetForm } from '@/components/asset/new-asset-form'
import { Panel } from '@/components/asset/panel'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { money } from '@/lib/format'
import { portfolioQuery } from '@/lib/queries'

export const Route = createFileRoute('/watchlist')({ component: Page })

function Page() {
  const snap = useQuery(portfolioQuery)
  const list = [...(snap.data?.watchlist ?? [])].sort(
    (a, b) => Number(b.gate?.passed ?? false) - Number(a.gate?.passed ?? false) || a.ticker.localeCompare(b.ticker),
  )
  return (
    <>
      <PageHeader
        title="Watchlist"
        description="Names you might buy. A name can only receive money once every buy gate check passes."
      />
      <CandidatesSection />
      <Panel title="Add a name" className="mb-6">
        <NewAssetForm />
      </Panel>
      {snap.isPending ? (
        <LoadingBlock rows={4} />
      ) : snap.isError ? (
        <ErrorBlock error={snap.error} />
      ) : list.length === 0 ? (
        <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">The watchlist is empty.</p>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {list.map((p) => (
            <article key={p.asset_id} className="bg-card rounded-lg border p-4">
              <header className="mb-3 flex items-baseline justify-between gap-2">
                <Link
                  to="/assets/$assetId"
                  params={{ assetId: String(p.asset_id) }}
                  className="display text-xl font-semibold hover:underline"
                >
                  {p.ticker}
                </Link>
                <span className="num text-muted-foreground text-sm">
                  {p.price ? money(p.price, p.currency) : 'no price'}
                  {p.fair_value ? ` / FV ${money(p.fair_value, p.currency)}` : ''}
                </span>
              </header>
              <GateChecklist gate={p.gate} error={p.gate_error} thesisStatus={p.thesis_status} />
            </article>
          ))}
        </div>
      )}
    </>
  )
}
