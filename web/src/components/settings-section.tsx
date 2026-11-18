import { useState } from 'react'

import { Field, FormError } from '@/components/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { errorMessage } from '@/lib/api'
import { decimalInput, fractionToPercent, percentToFraction } from '@/lib/format'

/**
 * percent: stored as a fraction, edited as a percent. decimal: edited as is.
 * int: whole number. text: free text. bool: checkbox.
 */
export type FieldKind = 'percent' | 'decimal' | 'int' | 'text' | 'bool'

export type FieldDef = {
  key: string
  label: string
  kind: FieldKind
  /** Unit shown next to the input ("R$", "x", "days"). Percent fields show "%". */
  unit?: string
  hint?: string
  /** Starts a new labelled block of fields. */
  group?: string
}

type Values = Record<string, unknown>
type Draft = Record<string, string | boolean>

function toDraft(fields: FieldDef[], values: Values): Draft {
  const out: Draft = {}
  for (const f of fields) {
    const v = values[f.key]
    if (f.kind === 'bool') out[f.key] = v === true
    else if (f.kind === 'percent') out[f.key] = fractionToPercent(String(v ?? '0'))
    else out[f.key] = String(v ?? '')
  }
  return out
}

/** Draft to API body, or per-field errors when an input is not a number. */
function toBody(fields: FieldDef[], draft: Draft): { body: Values; errors: Record<string, string> } {
  const body: Values = {}
  const errors: Record<string, string> = {}
  for (const f of fields) {
    const raw = draft[f.key]
    if (f.kind === 'bool') {
      body[f.key] = raw === true
    } else if (f.kind === 'text') {
      body[f.key] = String(raw).trim()
    } else if (f.kind === 'int') {
      if (/^-?\d+$/.test(String(raw).trim())) body[f.key] = Number(String(raw).trim())
      else errors[f.key] = 'Enter a whole number.'
    } else {
      const v = f.kind === 'percent' ? percentToFraction(String(raw)) : decimalInput(String(raw))
      if (v == null) errors[f.key] = 'Enter a number.'
      else body[f.key] = v
    }
  }
  return { body, errors }
}

/** The API reports "field: reason; field: reason" after a prefix. */
function fieldErrors(message: string, fields: FieldDef[]): { byField: Record<string, string>; rest: string | null } {
  const byField: Record<string, string> = {}
  const rest: string[] = []
  const body = message.includes(': ') ? message.slice(message.indexOf(': ') + 2) : message
  for (const part of body.split('; ')) {
    const i = part.indexOf(': ')
    const key = i > 0 ? part.slice(0, i) : ''
    if (key && fields.some((f) => f.key === key)) byField[key] = part.slice(i + 2)
    else rest.push(part)
  }
  return { byField, rest: rest.length ? rest.join('; ') : null }
}

function defaultLabel(f: FieldDef, defaults: Values): string {
  const v = defaults[f.key]
  if (v == null) return ''
  if (f.kind === 'bool') return v ? 'on' : 'off'
  if (f.kind === 'percent') return `${fractionToPercent(String(v))}%`
  if (f.unit === 'R$' || f.unit === 'US$') return `${f.unit} ${v}`
  return f.unit ? `${v} ${f.unit}` : String(v)
}

export function SettingsSection({
  title,
  description,
  fields,
  values,
  defaults,
  save,
  reset,
  aside,
}: {
  title: string
  description?: string
  fields: FieldDef[]
  values: Values
  defaults: Values
  /** Stores the body and resolves to the stored values. */
  save: (body: Values) => Promise<Values>
  /** Restores the defaults on the server. */
  reset: () => Promise<void>
  aside?: React.ReactNode
}) {
  const [base, setBase] = useState<Draft>(() => toDraft(fields, values))
  const [draft, setDraft] = useState<Draft>(base)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState<string | null>(null)

  const dirty = fields.some((f) => draft[f.key] !== base[f.key])
  const isDefault = JSON.stringify(base) === JSON.stringify(toDraft(fields, defaults))

  function change(key: string, v: string | boolean) {
    setDraft((d) => ({ ...d, [key]: v }))
    setErrors((e) => Object.fromEntries(Object.entries(e).filter(([k]) => k !== key)))
    setSaved(null)
  }

  async function onSave() {
    const { body, errors: local } = toBody(fields, draft)
    setErrors(local)
    setFormError(null)
    setSaved(null)
    if (Object.keys(local).length > 0) return
    setBusy(true)
    try {
      const stored = await save(body)
      const next = toDraft(fields, stored)
      setBase(next)
      setDraft(next)
      setSaved('Saved. The next evaluation and plan use these values.')
    } catch (e) {
      const { byField, rest } = fieldErrors(errorMessage(e), fields)
      setErrors(byField)
      setFormError(rest ?? (Object.keys(byField).length ? 'Fix the highlighted fields.' : null))
    } finally {
      setBusy(false)
    }
  }

  async function onReset() {
    setBusy(true)
    setFormError(null)
    setErrors({})
    try {
      await reset()
      const next = toDraft(fields, defaults)
      setBase(next)
      setDraft(next)
      setSaved('Defaults restored.')
    } catch (e) {
      setFormError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  // Keep original order but break into blocks at each field that starts a group.
  const blocks: { heading?: string; fields: FieldDef[] }[] = []
  for (const f of fields) {
    if (f.group || blocks.length === 0) blocks.push({ heading: f.group, fields: [f] })
    else blocks[blocks.length - 1]?.fields.push(f)
  }

  return (
    <section className="bg-card rounded-lg border p-4">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="eyebrow">{title}</h2>
          {description && <p className="text-muted-foreground mt-1 max-w-prose text-sm">{description}</p>}
        </div>
        {aside}
      </div>
      <form
        className="space-y-5"
        onSubmit={(e) => {
          e.preventDefault()
          void onSave()
        }}
      >
        {blocks.map((b, i) => (
          <div key={b.heading ?? i}>
            {b.heading && <h3 className="text-muted-foreground mb-2 text-xs font-medium tracking-wide uppercase">{b.heading}</h3>}
            <div className="grid grid-cols-1 gap-x-4 gap-y-3 sm:grid-cols-2 lg:grid-cols-3">
              {b.fields.map((f) => (
                <FieldInput
                  key={f.key}
                  def={f}
                  value={draft[f.key] ?? ''}
                  error={errors[f.key]}
                  defaults={defaults}
                  onChange={(v) => change(f.key, v)}
                />
              ))}
            </div>
          </div>
        ))}
        <FormError error={formError} />
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" size="sm" disabled={!dirty || busy}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
          <Button type="button" size="sm" variant="outline" disabled={isDefault || busy} onClick={() => void onReset()}>
            Reset to default
          </Button>
          {dirty && <span className="text-muted-foreground text-xs">Unsaved changes</span>}
          {saved && !dirty && (
            <span role="status" className="text-muted-foreground text-xs">
              {saved}
            </span>
          )}
        </div>
      </form>
    </section>
  )
}

function FieldInput({
  def,
  value,
  error,
  defaults,
  onChange,
}: {
  def: FieldDef
  value: string | boolean
  error?: string
  defaults: Values
  onChange: (v: string | boolean) => void
}) {
  const hint = (
    <>
      {def.hint && <span>{def.hint} </span>}
      <span>Default {defaultLabel(def, defaults)}.</span>
      {error && <span className="text-breach block">{error}</span>}
    </>
  )
  if (def.kind === 'bool') {
    return (
      <Field label={def.label} hint={hint}>
        {(id) => (
          <input
            id={id}
            type="checkbox"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
            className="accent-primary size-4"
          />
        )}
      </Field>
    )
  }
  const unit = def.kind === 'percent' ? '%' : def.unit
  const numeric = def.kind !== 'text'
  return (
    <Field label={def.label} hint={hint}>
      {(id) => (
        <div className="relative">
          <Input
            id={id}
            value={String(value)}
            inputMode={def.kind === 'int' ? 'numeric' : numeric ? 'decimal' : undefined}
            aria-invalid={error ? true : undefined}
            className={numeric ? 'num pr-11 text-right' : undefined}
            onChange={(e) => onChange(e.target.value)}
          />
          {unit && (
            <span className="text-muted-foreground pointer-events-none absolute inset-y-0 right-2.5 flex items-center text-xs">{unit}</span>
          )}
        </div>
      )}
    </Field>
  )
}
