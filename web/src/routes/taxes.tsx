import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'

import { PageHeader } from '@/components/page-header'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { api, unwrap, type Schemas } from '@/lib/api'
import { assetsQuery } from '@/lib/queries'
import { frac, money, pct } from '@/lib/format'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/taxes')({ component: Page })

type Month = Schemas['TaxMonth']

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const monthLabel = (m: string) => `${MONTHS[Number(m.slice(5, 7)) - 1] ?? m.slice(5, 7)} ${m.slice(0, 4)}`

function Page() {
  const taxes = useQuery({ queryKey: ['taxes', 'b3'], queryFn: () => unwrap(api.GET('/api/taxes/b3')) })
  const months = [...(taxes.data?.months ?? [])].reverse()
  const params = taxes.data?.params
  return (
    <>
      <PageHeader
        title="Taxes"
        description="B3 swing-trade tax by month: stock sales up to the exemption limit are tax free; above it, gains pay 15% by DARF."
      />
      {taxes.isPending ? (
        <LoadingBlock rows={4} />
      ) : taxes.isError ? (
        <ErrorBlock error={taxes.error} />
      ) : months.length === 0 ? (
        <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
          No B3 sales yet. Months appear here once you record a sell.
        </p>
      ) : (
        <div className="space-y-4">
          {months.map((m) => (
            <MonthCard key={m.month} m={m} limit={params?.exemption_limit ?? '20000'} />
          ))}
          <p className="text-muted-foreground text-xs">
            Rates: {pct(params?.swing_rate, 0)} swing, {pct(params?.day_trade_rate, 0)} day trade; DARF code{' '}
            {params?.darf_code}; amounts under {money(params?.darf_min)} roll into the next month. Due dates skip weekends
            but not bank holidays. Day-trade DARF code (6015 vs 6047) is unconfirmed: check in Sicalc.
          </p>
        </div>
      )}
    </>
  )
}

function MonthCard({ m, limit }: { m: Month; limit: string }) {
  const [open, setOpen] = useState(false)
  const assets = useQuery(assetsQuery)
  const tickers = new Map((assets.data ?? []).map((a) => [String(a.id), a.ticker]))
  const used = Math.min(frac(m.stock_sales) / frac(limit), 1.25)
  const due = Number(m.darf) > 0
  const hasDay = Number(m.day_trade_result) !== 0 || Number(m.day_trade_loss_in) > 0
  return (
    <section className="bg-card rounded-lg border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="display text-xl font-semibold">{monthLabel(m.month)}</h2>
          <span
            className={cn(
              'mt-1 inline-block rounded-full border px-2 py-0.5 text-xs font-medium',
              m.exempt ? 'border-jade/50 text-jade' : 'border-brass/50 text-brass',
            )}
          >
            {m.exempt ? 'Stock sales exempt' : 'Over the exemption'}
          </span>
        </div>
        <div className="text-right">
          <div className="eyebrow">DARF {m.darf_code}</div>
          <div className={cn('display num text-2xl font-semibold', due ? 'text-foreground' : 'text-muted-foreground')}>
            {money(m.darf)}
          </div>
          <div className="text-muted-foreground text-xs">
            {due ? `due ${m.due_date}` : Number(m.tax) > 0 ? 'below the minimum, rolls forward' : 'nothing to pay'}
          </div>
        </div>
      </div>

      <div className="mt-4">
        <div className="text-muted-foreground mb-1 flex justify-between text-xs">
          <span>
            Stock sales <span className="num text-foreground">{money(m.stock_sales)}</span>
          </span>
          <span className="num">limit {money(limit)}</span>
        </div>
        <div className="bg-muted relative h-2 overflow-hidden rounded-full" aria-hidden>
          <div
            className={cn('h-full rounded-full', m.exempt ? 'bg-jade' : 'bg-brass')}
            style={{ width: `${(used / 1.25) * 100}%` }}
          />
          <div className="bg-foreground/60 absolute inset-y-0 w-px" style={{ left: '80%' }} />
        </div>
      </div>

      <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        <Item label="Exempt gains" value={money(m.exempt_gain)} />
        <Item label="Swing result" value={money(m.swing_result)} />
        <Item label="Loss carried in" value={money(m.swing_loss_in)} />
        <Item label="Loss carried out" value={money(m.swing_loss_out)} />
        <Item label="Taxable" value={money(m.swing_taxable)} />
        {hasDay && (
          <>
            <Item label="Day-trade result" value={money(m.day_trade_result)} />
            <Item label="Day-trade taxable" value={money(m.day_trade_taxable)} />
          </>
        )}
        <Item label="Tax of the month" value={money(m.tax)} />
        {Number(m.carried_in) > 0 && <Item label="Carried from earlier" value={money(m.carried_in)} />}
      </dl>

      <button
        className="text-muted-foreground hover:text-foreground mt-3 text-xs underline"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {open ? 'Hide sales' : `Show ${m.sales?.length ?? 0} sales`}
      </button>
      {open && (
        <ul className="mt-2 divide-y text-sm">
          {(m.sales ?? []).map((s, i) => (
            <li key={i} className="flex flex-wrap justify-between gap-2 py-1.5">
              <span>
                <span className="num text-muted-foreground">{s.date}</span>{' '}
                <span className="font-medium">{tickers.get(s.asset_id) ?? `#${s.asset_id}`}</span>{' '}
                <span className="text-muted-foreground text-xs">
                  {s.kind}
                  {s.day_trade ? ' · day trade' : ''}
                  {s.tactical ? ' · tactical' : ''}
                </span>
              </span>
              <span className="num">
                {money(s.proceeds)}{' '}
                <span className={Number(s.gain) < 0 ? 'text-breach' : 'text-jade'}>
                  ({Number(s.gain) >= 0 ? '+' : ''}
                  {money(s.gain)})
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Item({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="num">{value}</dd>
    </div>
  )
}
