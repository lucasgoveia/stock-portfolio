import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'

import { GateDots } from '@/components/asset/gate-checklist'
import { NewAssetForm } from '@/components/asset/new-asset-form'
import { Panel } from '@/components/asset/panel'
import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { humanize, pct } from '@/lib/format'
import { assetsQuery, portfolioQuery } from '@/lib/queries'

export const Route = createFileRoute('/assets/')({ component: Page })

const ORDER: Record<string, number> = { holding: 0, watchlist: 1, exited: 2 }

function Page() {
  const assets = useQuery(assetsQuery)
  const snap = useQuery(portfolioQuery)
  const pos = new Map([...(snap.data?.holdings ?? []), ...(snap.data?.watchlist ?? [])].map((p) => [p.asset_id, p]))
  const rows = [...(assets.data ?? [])].sort(
    (a, b) => (ORDER[a.status] ?? 9) - (ORDER[b.status] ?? 9) || a.ticker.localeCompare(b.ticker),
  )
  return (
    <>
      <PageHeader title="Assets" description="Every name you hold, watch or have exited." />
      {assets.isPending ? (
        <LoadingBlock rows={5} />
      ) : assets.isError ? (
        <ErrorBlock error={assets.error} />
      ) : rows.length === 0 ? (
        <p className="text-muted-foreground mb-6 rounded-lg border border-dashed p-6 text-sm">No assets yet. Add the first below.</p>
      ) : (
        <Panel title={`${rows.length} assets`} className="mb-6">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Ticker</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Sleeve</TableHead>
                <TableHead className="text-right">Score</TableHead>
                <TableHead className="text-right">Weight</TableHead>
                <TableHead>Buy gate</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((a) => {
                const p = pos.get(a.id)
                return (
                  <TableRow key={a.id}>
                    <TableCell>
                      <Link to="/assets/$assetId" params={{ assetId: String(a.id) }} className="font-semibold hover:underline">
                        {a.ticker}
                      </Link>
                      {a.name && <span className="text-muted-foreground ml-2 text-xs">{a.name}</span>}
                    </TableCell>
                    <TableCell>{humanize(a.status)}</TableCell>
                    <TableCell>{humanize(a.sleeve)}</TableCell>
                    <TableCell className="num text-right">{p?.score ? Number(p.score).toFixed(2) : '—'}</TableCell>
                    <TableCell className="num text-right">{a.status === 'holding' && p ? pct(p.weight) : '—'}</TableCell>
                    <TableCell>{a.status === 'exited' ? null : <GateDots gate={p?.gate} />}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </Panel>
      )}
      <Panel title="Add an asset">
        <NewAssetForm />
      </Panel>
    </>
  )
}
