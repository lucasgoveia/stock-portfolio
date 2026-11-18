import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError, NativeSelect } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { humanize, today } from '@/lib/format'
import { qk } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Thesis = Schemas['Thesis']

const STATUS_STYLE: Record<string, string> = {
  active: 'border-jade/50 text-jade',
  under_review: 'border-brass/50 text-brass',
  broken: 'border-breach/50 text-breach',
}

export function ThesisTab({ assetId }: { assetId: number }) {
  const thesis = useQuery({
    queryKey: [...qk.asset(assetId), 'thesis'],
    queryFn: async () => {
      try {
        return await unwrap(api.GET('/api/assets/{id}/thesis', { params: { path: { id: assetId } } }))
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
  })
  if (thesis.isPending) return <LoadingBlock rows={4} />
  if (thesis.isError) return <ErrorBlock error={thesis.error} />
  if (thesis.data == null) {
    return (
      <Panel title="Write the thesis" className="max-w-2xl">
        <p className="text-muted-foreground mb-3 text-sm">
          Why do you own this, and what would make you sell it? The buy gate needs a thesis with at least one break
          condition.
        </p>
        <ThesisEditor assetId={assetId} />
      </Panel>
    )
  }
  const t = thesis.data
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <div className="space-y-4">
        <Panel
          title="Thesis"
          actions={
            <span className={cn('rounded-full border px-2.5 py-0.5 text-xs font-medium', STATUS_STYLE[t.status])}>
              {humanize(t.status)}
            </span>
          }
        >
          <ThesisEditor assetId={assetId} thesis={t} />
        </Panel>
        <Conditions assetId={assetId} thesis={t} />
      </div>
      <div className="space-y-4">
        <ReviewForm assetId={assetId} thesis={t} />
        <Reviews assetId={assetId} />
      </div>
    </div>
  )
}

function useInvalidate(assetId: number) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
    void qc.invalidateQueries({ queryKey: qk.portfolio })
    void qc.invalidateQueries({ queryKey: qk.alerts })
  }
}

function ThesisEditor({ assetId, thesis }: { assetId: number; thesis?: Thesis }) {
  const invalidate = useInvalidate(assetId)
  const [text, setText] = useState(thesis?.text ?? '')
  const [next, setNext] = useState(thesis?.next_review_on ?? '')
  const [error, setError] = useState<string | null>(null)
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PUT('/api/assets/{id}/thesis', {
          params: { path: { id: assetId } },
          body: { text, next_review_on: next || undefined },
        }),
      ),
    onSuccess: () => {
      toast.success(thesis ? 'Thesis saved' : 'Thesis created')
      setError(null)
      invalidate()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  const dirty = !thesis || text !== thesis.text || next !== (thesis.next_review_on ?? '')
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        if (!text.trim()) return setError('Write the thesis first.')
        save.mutate()
      }}
      className="space-y-3"
      noValidate
    >
      <Field label="Thesis">
        {(id) => (
          <Textarea
            id={id}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={6}
            placeholder="The business, why it wins, what the price implies."
          />
        )}
      </Field>
      <Field label="Next review" className="max-w-48">
        {(id) => <Input id={id} type="date" value={next} onChange={(e) => setNext(e.target.value)} />}
      </Field>
      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={save.isPending || !dirty}>
          {thesis ? 'Save thesis' : 'Create thesis'}
        </Button>
        <FormError error={error} />
      </div>
    </form>
  )
}

function Conditions({ assetId, thesis }: { assetId: number; thesis: Thesis }) {
  const invalidate = useInvalidate(assetId)
  const [text, setText] = useState('')
  const add = useMutation({
    mutationFn: () =>
      unwrap(api.POST('/api/assets/{id}/thesis/conditions', { params: { path: { id: assetId } }, body: { text } })),
    onSuccess: () => {
      setText('')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const conditions = thesis.break_conditions ?? []
  return (
    <Panel title="Break conditions">
      <p className="text-muted-foreground mb-2 text-xs">If any of these happens, the thesis is broken and a full exit is allowed.</p>
      {conditions.length === 0 ? (
        <p className="text-breach mb-2 text-sm">None yet: the buy gate needs at least one.</p>
      ) : (
        <ul className="mb-3 divide-y">
          {conditions.map((c) => (
            <ConditionRow key={c.id} assetId={assetId} condition={c} />
          ))}
        </ul>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (text.trim()) add.mutate()
        }}
        className="flex gap-2"
      >
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="e.g. Net debt/EBITDA above 3x"
          aria-label="New break condition"
        />
        <Button type="submit" variant="outline" disabled={add.isPending || !text.trim()}>
          <Plus /> Add
        </Button>
      </form>
    </Panel>
  )
}

function ConditionRow({ assetId, condition: c }: { assetId: number; condition: Schemas['BreakCondition'] }) {
  const invalidate = useInvalidate(assetId)
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(c.text)
  const update = useMutation({
    mutationFn: () =>
      unwrap(api.PATCH('/api/break-conditions/{id}', { params: { path: { id: c.id } }, body: { text } })),
    onSuccess: () => {
      setEditing(false)
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/break-conditions/{id}', { params: { path: { id: c.id } } })),
    onSuccess: () => {
      toast.success('Condition removed')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  if (editing) {
    return (
      <li className="py-2">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate()
          }}
          className="flex gap-2"
        >
          <Input value={text} onChange={(e) => setText(e.target.value)} aria-label="Break condition" autoFocus />
          <Button type="submit" size="sm" disabled={update.isPending}>
            Save
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
        </form>
      </li>
    )
  }
  return (
    <li className="flex items-start justify-between gap-2 py-2 text-sm">
      <span>{c.text}</span>
      <span className="flex shrink-0 gap-1">
        <Button size="icon-xs" variant="ghost" aria-label="Edit condition" onClick={() => setEditing(true)}>
          <Pencil />
        </Button>
        <Button size="icon-xs" variant="ghost" aria-label="Delete condition" disabled={del.isPending} onClick={() => del.mutate()}>
          <Trash2 />
        </Button>
      </span>
    </li>
  )
}

function ReviewForm({ assetId, thesis }: { assetId: number; thesis: Thesis }) {
  const invalidate = useInvalidate(assetId)
  const [on, setOn] = useState(today())
  const [note, setNote] = useState('')
  const [status, setStatus] = useState('')
  const [next, setNext] = useState('')
  const [error, setError] = useState<string | null>(null)
  const review = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/assets/{id}/thesis/review', {
          params: { path: { id: assetId } },
          body: { reviewed_on: on, note, status, next_review_on: next || undefined },
        }),
      ),
    onSuccess: (r) => {
      toast.success(`Review recorded: thesis ${humanize(r.status_after).toLowerCase()}`)
      setNote('')
      setStatus('')
      setNext('')
      setError(null)
      invalidate()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <Panel title="Record a review">
      <p className="text-muted-foreground mb-3 text-xs">
        {thesis.next_review_on ? `Next review due ${thesis.next_review_on}.` : 'No review scheduled.'} Check each break
        condition against the latest results.
      </p>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!note.trim()) return setError('Write what the review found, condition by condition.')
          review.mutate()
        }}
        className="grid grid-cols-2 gap-3"
        noValidate
      >
        <Field label="Reviewed on">{(id) => <Input id={id} type="date" value={on} onChange={(e) => setOn(e.target.value)} />}</Field>
        <Field label="Outcome">
          {(id) => (
            <NativeSelect id={id} value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">Thesis holds</option>
              <option value="under_review">Needs more review</option>
              <option value="broken">Thesis broken</option>
            </NativeSelect>
          )}
        </Field>
        <Field label="Notes" className="col-span-2">
          {(id) => <Textarea id={id} value={note} onChange={(e) => setNote(e.target.value)} rows={3} />}
        </Field>
        <Field label="Next review">{(id) => <Input id={id} type="date" value={next} onChange={(e) => setNext(e.target.value)} />}</Field>
        <div className="col-span-2 flex flex-wrap items-center gap-3">
          <Button type="submit" variant={status === 'broken' ? 'destructive' : 'default'} disabled={review.isPending}>
            {status === 'broken' ? 'Mark thesis broken' : 'Record review'}
          </Button>
          <FormError error={error} />
        </div>
      </form>
    </Panel>
  )
}

function Reviews({ assetId }: { assetId: number }) {
  const reviews = useQuery({
    queryKey: [...qk.asset(assetId), 'reviews'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/thesis/reviews', { params: { path: { id: assetId } } })),
  })
  if (reviews.isPending) return <LoadingBlock rows={2} />
  if (reviews.isError) return <ErrorBlock error={reviews.error} />
  const rows = reviews.data ?? []
  if (rows.length === 0) return null
  return (
    <Panel title="Reviews">
      <ol className="space-y-3">
        {rows.map((r) => (
          <li key={r.id} className="border-l-2 pl-3 text-sm">
            <div className="flex flex-wrap gap-x-2">
              <span className="num">{r.reviewed_on}</span>
              <span className="text-muted-foreground">thesis {humanize(r.status_after).toLowerCase()}</span>
            </div>
            {r.note && <p className="mt-0.5 whitespace-pre-wrap">{r.note}</p>}
          </li>
        ))}
      </ol>
    </Panel>
  )
}
