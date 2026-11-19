import { Plus, Trash2 } from 'lucide-react'

import {
  BASIS_LABEL,
  SCENARIOS,
  blankInitiative,
  type Basis,
  type DCFDraft,
  type InitiativeDraft,
  type ScenarioDraft,
  type ScenarioName,
} from '@/components/asset/dcf-draft'
import { Field, NativeSelect } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Schemas } from '@/lib/api'
import { money } from '@/lib/format'
import { cn } from '@/lib/utils'

/** Latest reported margins, in percent, offered as placeholders for the start margin. */
export interface MarginHints {
  ebit?: string
  net?: string
}

/** Basis, start margin, cash conversion and tax rate of each scenario. */
export function BasisTable({
  scenarios,
  hints,
  onChange,
}: {
  scenarios: Record<ScenarioName, ScenarioDraft>
  hints: MarginHints
  onChange: (n: ScenarioName, patch: Partial<ScenarioDraft>) => void
}) {
  const cell = 'h-8 px-1.5 text-right num'
  return (
    <div className="-mx-4 mt-5 overflow-x-auto px-4">
      <table className="w-full min-w-[34rem] text-sm">
        <thead>
          <tr className="text-muted-foreground text-xs">
            <th className="py-1 text-left font-medium">Basis</th>
            <th className="px-1 py-1 text-left font-medium">Margin and multiple mean</th>
            <th className="px-1 py-1 font-medium">Start margin %</th>
            <th className="px-1 py-1 font-medium">Cash conv. %</th>
            <th className="px-1 py-1 font-medium">Tax %</th>
          </tr>
        </thead>
        <tbody>
          {SCENARIOS.map((n) => {
            const s = scenarios[n]
            const hint = s.basis === 'net_pe' ? hints.net : hints.ebit
            return (
              <tr key={n}>
                <th scope="row" className="py-1 pr-2 text-left font-medium capitalize">
                  {n}
                </th>
                <td className="px-0.5 py-1">
                  <NativeSelect
                    aria-label={`${n} basis`}
                    value={s.basis}
                    onChange={(e) => onChange(n, { basis: e.target.value as Basis })}
                  >
                    {(Object.keys(BASIS_LABEL) as Basis[]).map((b) => (
                      <option key={b} value={b}>
                        {BASIS_LABEL[b]}
                      </option>
                    ))}
                  </NativeSelect>
                </td>
                <td className="px-0.5 py-1">
                  <Input
                    aria-label={`${n} start margin`}
                    className={cell}
                    inputMode="decimal"
                    value={s.startMargin}
                    placeholder={hint ?? 'terminal'}
                    onChange={(e) => onChange(n, { startMargin: e.target.value })}
                  />
                </td>
                <td className="px-0.5 py-1">
                  <Input
                    aria-label={`${n} cash conversion`}
                    className={cell}
                    inputMode="decimal"
                    value={s.cashConversion}
                    onChange={(e) => onChange(n, { cashConversion: e.target.value })}
                  />
                </td>
                <td className="px-0.5 py-1">
                  <Input
                    aria-label={`${n} tax rate`}
                    className={cell}
                    inputMode="decimal"
                    value={s.tax}
                    disabled={s.basis === 'net_pe'}
                    onChange={(e) => onChange(n, { tax: e.target.value })}
                  />
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <p className="text-muted-foreground mt-1 max-w-prose text-xs">
        The margin moves in a straight line from the start margin (empty: the terminal margin) to the terminal margin.
        Cash conversion is the share of each year&apos;s earnings returned to shareholders, discounted at the scenario
        rate; for EBIT it is taken after tax. Net income is already after tax.
      </p>
    </div>
  )
}

/** The new-business list: add and remove rows, per-scenario success probability. */
export function InitiativesEditor({
  initiatives,
  currency,
  onChange,
}: {
  initiatives: InitiativeDraft[]
  currency: string
  onChange: (next: InitiativeDraft[]) => void
}) {
  const patch = (key: number, p: Partial<InitiativeDraft>) =>
    onChange(initiatives.map((i) => (i.key === key ? { ...i, ...p } : i)))
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="eyebrow">New businesses</h3>
        <Button size="xs" variant="outline" onClick={() => onChange([...initiatives, blankInitiative()])}>
          <Plus /> Add initiative
        </Button>
      </div>
      <p className="text-muted-foreground mt-1 max-w-prose text-xs">
        A product, segment, capex program or market entry that past revenue does not show. Its revenue ramps in a straight
        line from the start year to the year-5 figure and counts in each scenario at that scenario&apos;s success
        probability.
      </p>
      {initiatives.length === 0 ? (
        <p className="text-muted-foreground mt-3 text-sm">None. The model rests on the existing business only.</p>
      ) : (
        <ul className="mt-3 space-y-3">
          {initiatives.map((i, idx) => (
            <li key={i.key} className="rounded-lg border p-3">
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <Field label="Name" className="col-span-2">
                  {(id) => <Input id={id} value={i.name} onChange={(e) => patch(i.key, { name: e.target.value })} />}
                </Field>
                <Field label="Start year">
                  {(id) => (
                    <NativeSelect id={id} value={i.startYear} onChange={(e) => patch(i.key, { startYear: e.target.value })}>
                      {[1, 2, 3, 4, 5].map((y) => (
                        <option key={y} value={y}>
                          Year {y}
                        </option>
                      ))}
                    </NativeSelect>
                  )}
                </Field>
                <Field label={`Year-5 revenue (${currency})`}>
                  {(id) => (
                    <Input id={id} inputMode="decimal" className="num" value={i.revenue5} onChange={(e) => patch(i.key, { revenue5: e.target.value })} />
                  )}
                </Field>
                <Field label="Margin %">
                  {(id) => (
                    <Input id={id} inputMode="decimal" className="num" value={i.margin} onChange={(e) => patch(i.key, { margin: e.target.value })} />
                  )}
                </Field>
                {SCENARIOS.map((n) => (
                  <Field key={n} label={`Success % (${n})`}>
                    {(id) => (
                      <Input id={id} inputMode="decimal" className="num" value={i[n]} onChange={(e) => patch(i.key, { [n]: e.target.value })} />
                    )}
                  </Field>
                ))}
                <Field label="Rationale" className="col-span-2 sm:col-span-3">
                  {(id) => <Input id={id} value={i.rationale} onChange={(e) => patch(i.key, { rationale: e.target.value })} />}
                </Field>
                <Field label="Sources" hint="One per line." className="col-span-2 sm:col-span-3">
                  {(id) => (
                    <textarea
                      id={id}
                      rows={2}
                      value={i.sources}
                      onChange={(e) => patch(i.key, { sources: e.target.value })}
                      className="border-input focus-visible:border-ring focus-visible:ring-ring/50 dark:bg-input/30 w-full rounded-lg border bg-transparent px-2.5 py-1.5 text-sm outline-none focus-visible:ring-3"
                    />
                  )}
                </Field>
                <div className="flex items-end justify-end">
                  <Button
                    size="xs"
                    variant="ghost"
                    aria-label={`Remove initiative ${idx + 1}`}
                    onClick={() => onChange(initiatives.filter((x) => x.key !== i.key))}
                  >
                    <Trash2 /> Remove
                  </Button>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** Value per share of each scenario, split into the existing business and the initiatives. */
export function BreakdownTable({
  result,
  currency,
  draft,
}: {
  result: Schemas['DCFResult']
  currency: string
  draft: DCFDraft
}) {
  const byName = new Map((result.scenarios ?? []).map((s) => [s.name, s]))
  const th = 'px-2 py-1 text-right font-medium'
  return (
    <div className="mt-3 overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-muted-foreground text-xs">
            <th className="py-1 text-left font-medium">Per share</th>
            <th className={th}>Core</th>
            <th className={th}>Initiatives</th>
            <th className={th}>Value</th>
          </tr>
        </thead>
        <tbody>
          {SCENARIOS.map((n) => {
            const s = byName.get(n)
            return (
              <tr key={n} className="border-t">
                <th scope="row" className="py-1 pr-2 text-left font-normal capitalize">
                  {n} <span className="text-muted-foreground text-xs">{draft.scenarios[n].probability}%</span>
                </th>
                <td className="num px-2 py-1 text-right">{money(s?.core_value_per_share, currency)}</td>
                <td className="num px-2 py-1 text-right">{money(s?.initiatives_value_per_share, currency)}</td>
                <td className="num px-2 py-1 text-right">{money(s?.value_per_share, currency)}</td>
              </tr>
            )
          })}
        </tbody>
        <tfoot>
          <tr className={cn('border-t font-medium')}>
            <th scope="row" className="py-1 pr-2 text-left">
              Weighted
            </th>
            <td className="num px-2 py-1 text-right">{money(result.core_value_per_share, currency)}</td>
            <td className="num px-2 py-1 text-right">{money(result.initiatives_value_per_share, currency)}</td>
            <td className="num px-2 py-1 text-right">{money(result.value_per_share, currency)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  )
}
