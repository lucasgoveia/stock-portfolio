// Display-only formatting. Money arrives as decimal strings and is never
// summed or rounded here; Number() is used only to render it.

const brlFmt = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' })
const usdFmt = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' })
const numFmt = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 8 })

export function money(v: string | null | undefined, currency = 'BRL'): string {
  if (v == null || v === '') return '—'
  const n = Number(v)
  if (!Number.isFinite(n)) return v
  return currency === 'USD' ? usdFmt.format(n) : brlFmt.format(n)
}

export function qty(v: string | null | undefined): string {
  if (v == null || v === '') return '—'
  const n = Number(v)
  return Number.isFinite(n) ? numFmt.format(n) : v
}

/** Fraction ("0.0432") to percent ("4.32%"). */
export function pct(v: string | number | null | undefined, digits = 2): string {
  if (v == null || v === '') return '—'
  const n = typeof v === 'number' ? v : Number(v)
  if (!Number.isFinite(n)) return String(v)
  return `${(n * 100).toFixed(digits)}%`
}

export function frac(v: string | null | undefined): number {
  const n = Number(v ?? 0)
  return Number.isFinite(n) ? n : 0
}

export function today(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function dateTime(v: string | null | undefined): string {
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
}

/** Snake case identifiers ("raise_cash") to sentence case ("Raise cash"). */
export function humanize(v: string): string {
  const s = v.replace(/_/g, ' ')
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** Accepts "1.234,56" or "1234.56" and returns a plain decimal string, or null. */
export function decimalInput(v: string): string | null {
  const s = v.trim().replace(/\s/g, '')
  if (!s) return null
  const normalized = s.includes(',') ? s.replace(/\./g, '').replace(',', '.') : s
  return /^-?\d+(\.\d+)?$/.test(normalized) ? normalized : null
}

/** Percent input ("12.5") to a fraction string ("0.125") without floats. */
export function percentToFraction(v: string): string | null {
  const d = decimalInput(v)
  if (d == null) return null
  const neg = d.startsWith('-')
  const [int = '0', dec = ''] = (neg ? d.slice(1) : d).split('.')
  const digits = int.padStart(3, '0') + dec
  const point = digits.length - dec.length - 2
  const out = `${digits.slice(0, point)}.${digits.slice(point)}`.replace(/^0+(?=\d)/, '').replace(/\.?0+$/, '')
  return (neg ? '-' : '') + (out === '' ? '0' : out)
}

/** Fraction string ("0.125") to a percent input ("12.5") without floats. */
export function fractionToPercent(v: string): string {
  const neg = v.startsWith('-')
  const [int = '0', dec = ''] = (neg ? v.slice(1) : v).split('.')
  const d = dec.padEnd(2, '0')
  const out = `${int}${d.slice(0, 2)}.${d.slice(2)}`.replace(/^0+(?=\d)/, '').replace(/\.?0+$/, '').replace(/\.$/, '')
  return (neg ? '-' : '') + (out === '' ? '0' : out)
}

/** A decimal string scaled to an integer with `places` decimals (truncated), for exact sums. */
export function scaled(v: string, places: number): number {
  const d = decimalInput(v) ?? '0'
  const neg = d.startsWith('-')
  const [int = '0', dec = ''] = (neg ? d.slice(1) : d).split('.')
  const n = Number(int + dec.padEnd(places, '0').slice(0, places))
  return neg ? -n : n
}
