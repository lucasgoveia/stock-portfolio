import { Check, X } from 'lucide-react'

import type { Schemas } from '@/lib/api'
import { pct } from '@/lib/format'
import { cn } from '@/lib/utils'

type Gate = Schemas['GateResult']

/** The buy gate (spec 8.1): every check must pass before a buy is suggested. */
function gateChecks(g: Gate): { label: string; ok: boolean; detail?: string }[] {
  return [
    { label: 'Score at least 2.5', ok: g.score_ok, detail: Number(g.score) ? Number(g.score).toFixed(2) : 'not rated' },
    { label: 'Thesis with a break condition', ok: g.thesis_ok },
    { label: 'Fair value set', ok: g.fair_value_set },
    { label: 'Fair value under 12 months old', ok: g.fair_value_fresh },
    {
      label: 'Price at or below fair value',
      ok: g.price_ok,
      detail: !g.fair_value_set
        ? undefined
        : g.discount.startsWith('-')
          ? `${pct(g.discount.slice(1), 1)} above fair value`
          : `${pct(g.discount, 1)} below fair value`,
    },
  ]
}

export function GateChecklist({ gate, error }: { gate?: Gate; error?: string }) {
  if (!gate) {
    return <p className="text-muted-foreground text-sm">{error ? `Buy gate unavailable: ${error}` : 'No price yet: run the daily job.'}</p>
  }
  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'rounded-full border px-2.5 py-0.5 text-xs font-medium',
            gate.passed ? 'border-jade/50 text-jade' : 'border-breach/50 text-breach',
          )}
        >
          {gate.passed ? 'Buy gate passed' : 'Buy gate blocked'}
        </span>
        {gate.opportunity && (
          <span className="border-brass/50 text-brass rounded-full border px-2.5 py-0.5 text-xs font-medium">
            {gate.catalyst_only ? 'Opportunity: catalyst' : `Opportunity: ${pct(gate.discount, 0)} below fair value`}
          </span>
        )}
      </div>
      <ul className="space-y-1.5 text-sm">
        {gateChecks(gate).map((c) => (
          <li key={c.label} className="flex items-center gap-2">
            {c.ok ? (
              <Check className="text-jade size-4 shrink-0" aria-label="passes" />
            ) : (
              <X className="text-breach size-4 shrink-0" aria-label="fails" />
            )}
            <span className={c.ok ? '' : 'text-foreground'}>{c.label}</span>
            {c.detail && <span className="num text-muted-foreground text-xs">{c.detail}</span>}
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Compact one-line version for lists: a dot per check. */
export function GateDots({ gate }: { gate?: Gate }) {
  if (!gate) return <span className="text-muted-foreground text-xs">no price</span>
  return (
    <span className="inline-flex items-center gap-1" aria-label={gate.passed ? 'Buy gate passed' : 'Buy gate blocked'}>
      {gateChecks(gate).map((c) => (
        <span
          key={c.label}
          title={`${c.label}: ${c.ok ? 'passes' : 'fails'}`}
          className={cn('size-2 rounded-full', c.ok ? 'bg-jade' : 'bg-breach')}
        />
      ))}
    </span>
  )
}
