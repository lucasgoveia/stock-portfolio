import { cn } from '@/lib/utils'

export type GaugeState = 'ok' | 'watch' | 'breach' | 'neutral'

const markerColor: Record<GaugeState, string> = {
  ok: 'bg-jade',
  watch: 'bg-brass',
  breach: 'bg-breach',
  neutral: 'bg-foreground',
}

interface Props {
  /** Scale end (fraction); values above it pin to the right edge. */
  max: number
  actual: number
  target?: number
  lower?: number
  upper?: number
  /** Hairlines for hard limits, e.g. the 2% floor and 12% cap. */
  floor?: number
  cap?: number
  state: GaugeState
  label: string
  className?: string
}

// Positions are inset 4px each side so a pinned marker stays inside the track.
const at = (v: number, max: number) => `calc(4px + (100% - 8px) * ${Math.min(Math.max(v / max, 0), 1)})`

/**
 * A level gauge: the shaded zone is the allowed band, the notch is the target,
 * the bar is where the position actually sits.
 */
export function BandGauge({ max, actual, target, lower, upper, floor, cap, state, label, className }: Props) {
  return (
    <div role="img" aria-label={label} className={cn('relative h-7 w-full', className)}>
      <div className="bg-muted absolute inset-x-0 top-1/2 h-2 -translate-y-1/2 rounded-full" />
      {lower != null && upper != null && (
        <div
          className="bg-band absolute top-1/2 h-4 -translate-y-1/2 rounded-sm"
          style={{ left: at(lower, max), width: `calc(${at(upper, max)} - ${at(lower, max)})` }}
        />
      )}
      {floor != null && (
        <div className="border-muted-foreground/50 absolute inset-y-0.5 border-l border-dashed" style={{ left: at(floor, max) }} />
      )}
      {cap != null && <div className="border-breach/70 absolute inset-y-0 border-l" style={{ left: at(cap, max) }} />}
      {target != null && (
        <div
          className="border-t-foreground absolute top-0 size-0 -translate-x-1/2 border-x-[5px] border-t-[6px] border-x-transparent"
          style={{ left: at(target, max) }}
        />
      )}
      <div
        className={cn('absolute top-1/2 h-5 w-1 -translate-x-1/2 -translate-y-1/2 rounded-full', markerColor[state])}
        style={{ left: at(actual, max) }}
      />
    </div>
  )
}
