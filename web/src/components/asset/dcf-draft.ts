import { decimalInput, fractionToPercent, humanize, percentToFraction, scaled } from '@/lib/format'
import type { Schemas } from '@/lib/api'

export type DCFInput = Schemas['DCFInput']
export type Basis = 'ebit_ev' | 'net_pe'

export const SCENARIOS = ['bear', 'base', 'bull'] as const
export type ScenarioName = (typeof SCENARIOS)[number]

export const BASIS_LABEL: Record<Basis, string> = {
  ebit_ev: 'EBIT × EV/EBIT (subtracts net debt)',
  net_pe: 'Net income × P/E',
}

/** Default tax rate in percent: 34% in Brazil (BRL), 21% in the US (USD). */
export const defaultTax = (currency: string): string => (currency === 'USD' ? '21' : '34')

/** Editor state: every rate as a percent string, as typed. */
export interface ScenarioDraft {
  growth: string[]
  margin: string
  multiple: string
  discount: string
  probability: string
  basis: Basis
  /** Empty means the margin stays at the terminal margin. */
  startMargin: string
  cashConversion: string
  tax: string
}

export interface InitiativeDraft {
  key: number
  name: string
  rationale: string
  /** One URL or reference per line. */
  sources: string
  startYear: string
  revenue5: string
  margin: string
  bear: string
  base: string
  bull: string
}

export interface DCFDraft {
  baseRevenue: string
  netDebt: string
  shares: string
  scenarios: Record<ScenarioName, ScenarioDraft>
  initiatives: InitiativeDraft[]
}

let nextKey = 1
export const blankInitiative = (): InitiativeDraft => ({
  key: nextKey++,
  name: '',
  rationale: '',
  sources: '',
  startYear: '3',
  revenue5: '',
  margin: '',
  bear: '10',
  base: '30',
  bull: '60',
})

const blankScenario = (probability: string, growth: string, tax: string): ScenarioDraft => ({
  growth: Array(5).fill(growth),
  margin: '',
  multiple: '',
  discount: '12',
  probability,
  basis: 'ebit_ev',
  startMargin: '',
  cashConversion: '0',
  tax,
})

export const blankDraft = (tax: string): DCFDraft => ({
  baseRevenue: '',
  netDebt: '0',
  shares: '',
  scenarios: { bear: blankScenario('25', '0', tax), base: blankScenario('50', '5', tax), bull: blankScenario('25', '10', tax) },
  initiatives: [],
})

/** Stored versions without the new fields read as ebit_ev, no interim cash, no initiatives. */
export function draftFrom(dcf: DCFInput, tax: string): DCFDraft {
  const d = blankDraft(tax)
  d.baseRevenue = dcf.base_revenue
  d.netDebt = dcf.net_debt ?? '0'
  d.shares = dcf.shares
  for (const s of dcf.scenarios ?? []) {
    if (!SCENARIOS.includes(s.name as ScenarioName)) continue
    d.scenarios[s.name as ScenarioName] = {
      growth: (s.revenue_growth ?? []).map(fractionToPercent),
      margin: fractionToPercent(s.terminal_margin),
      multiple: s.exit_multiple,
      discount: fractionToPercent(s.discount_rate),
      probability: fractionToPercent(s.probability),
      basis: s.basis === 'net_pe' ? 'net_pe' : 'ebit_ev',
      startMargin: s.start_margin != null ? fractionToPercent(s.start_margin) : '',
      cashConversion: fractionToPercent(s.cash_conversion ?? '0'),
      tax: s.tax_rate != null && s.cash_conversion != null ? fractionToPercent(s.tax_rate) : tax,
    }
  }
  d.initiatives = (dcf.initiatives ?? []).map((i) => ({
    key: nextKey++,
    name: i.name,
    rationale: i.rationale ?? '',
    sources: (i.sources ?? []).join('\n'),
    startYear: String(i.start_year),
    revenue5: i.revenue_year5,
    margin: fractionToPercent(i.margin),
    bear: fractionToPercent(i.success_probability.bear),
    base: fractionToPercent(i.success_probability.base),
    bull: fractionToPercent(i.success_probability.bull),
  }))
  return d
}

/** Converts the draft to API input, or returns the first problem in plain words. */
export function toInput(d: DCFDraft): { input?: DCFInput; error?: string } {
  const rev = decimalInput(d.baseRevenue)
  const shares = decimalInput(d.shares)
  const debt = decimalInput(d.netDebt || '0')
  if (!rev || Number(rev) <= 0) return { error: 'Enter the latest annual revenue.' }
  if (!shares || Number(shares) <= 0) return { error: 'Enter shares outstanding.' }
  if (debt == null) return { error: 'Net debt must be a number (negative for net cash).' }
  const probTotal = SCENARIOS.reduce((sum, n) => sum + scaled(d.scenarios[n].probability, 2), 0)
  if (probTotal !== 10000) {
    return { error: `Probabilities add up to ${(probTotal / 100).toString()}%; they must total 100%.` }
  }
  const scenarios = []
  for (const n of SCENARIOS) {
    const s = d.scenarios[n]
    const growth = s.growth.map(percentToFraction)
    if (growth.some((g) => g == null)) return { error: `${humanize(n)}: every year needs a growth rate.` }
    const margin = percentToFraction(s.margin)
    const discount = percentToFraction(s.discount)
    const multiple = decimalInput(s.multiple)
    const probability = percentToFraction(s.probability)
    if (margin == null) return { error: `${humanize(n)}: enter the terminal margin.` }
    if (!multiple || Number(multiple) <= 0) return { error: `${humanize(n)}: enter the exit multiple.` }
    if (!discount || Number(discount) <= 0) return { error: `${humanize(n)}: enter the discount rate.` }
    if (probability == null) return { error: `${humanize(n)}: enter the probability.` }
    const start = s.startMargin.trim() === '' ? undefined : percentToFraction(s.startMargin)
    if (start === null) return { error: `${humanize(n)}: the start margin must be a number.` }
    const cash = percentToFraction(s.cashConversion.trim() === '' ? '0' : s.cashConversion)
    if (cash == null) return { error: `${humanize(n)}: the cash conversion must be a number.` }
    const tax = percentToFraction(s.tax.trim() === '' ? '0' : s.tax)
    if (tax == null) return { error: `${humanize(n)}: the tax rate must be a number.` }
    scenarios.push({
      name: n,
      revenue_growth: growth as string[],
      terminal_margin: margin,
      exit_multiple: multiple,
      discount_rate: discount,
      probability,
      basis: s.basis,
      ...(start ? { start_margin: start } : {}),
      cash_conversion: cash,
      tax_rate: tax,
    })
  }
  const initiatives = []
  for (const [idx, i] of d.initiatives.entries()) {
    const label = i.name.trim() || `Initiative ${idx + 1}`
    const revenue5 = decimalInput(i.revenue5)
    const margin = percentToFraction(i.margin)
    const probs = [i.bear, i.base, i.bull].map(percentToFraction)
    if (!i.name.trim()) return { error: `Initiative ${idx + 1}: enter a name.` }
    if (!revenue5 || Number(revenue5) <= 0) return { error: `${label}: enter its year-5 revenue.` }
    if (margin == null) return { error: `${label}: enter its margin.` }
    if (probs.some((p) => p == null)) return { error: `${label}: enter a success probability for each scenario.` }
    initiatives.push({
      name: i.name.trim(),
      rationale: i.rationale.trim(),
      sources: i.sources
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean),
      start_year: Number(i.startYear),
      revenue_year5: revenue5,
      margin,
      success_probability: { bear: probs[0] as string, base: probs[1] as string, bull: probs[2] as string },
    })
  }
  return { input: { base_revenue: rev, net_debt: debt, shares, scenarios, initiatives } }
}
