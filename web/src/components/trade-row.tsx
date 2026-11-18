import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { TableCell, TableRow } from '@/components/ui/table'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { money, qty } from '@/lib/format'

/** A trade table row with a two-step delete. Pass ticker to show the asset column. */
export function TradeRow({ trade: t, ticker }: { trade: Schemas['Trade']; ticker?: string }) {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/trades/{id}', { params: { path: { id: t.id } } })),
    onSuccess: () => {
      toast.success('Trade deleted')
      void qc.invalidateQueries()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <TableRow>
      <TableCell className="num">{t.date}</TableCell>
      {ticker && (
        <TableCell>
          <Link to="/assets/$assetId" params={{ assetId: String(t.asset_id) }} className="font-medium hover:underline">
            {ticker}
          </Link>
        </TableCell>
      )}
      <TableCell className={t.side === 'sell' ? 'text-brass' : 'text-jade'}>{t.side}</TableCell>
      <TableCell className="num text-right">{qty(t.quantity)}</TableCell>
      <TableCell className="num text-right">
        {money(t.price, t.currency)}
        {t.currency === 'USD' && <div className="text-muted-foreground text-xs">FX {t.fx_rate}</div>}
      </TableCell>
      <TableCell className="num text-right">{money(t.fees, t.currency)}</TableCell>
      <TableCell className="text-right">
        {confirming ? (
          <span className="inline-flex gap-1">
            <Button size="xs" variant="destructive" disabled={del.isPending} onClick={() => del.mutate()}>
              Delete
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirming(false)}>
              Keep
            </Button>
          </span>
        ) : (
          <Button size="icon-xs" variant="ghost" aria-label={`Delete trade of ${t.date}`} onClick={() => setConfirming(true)}>
            <Trash2 />
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}
