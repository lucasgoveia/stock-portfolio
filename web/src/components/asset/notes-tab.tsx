import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Panel } from '@/components/asset/panel'
import { Field, FormError } from '@/components/form'
import { ErrorBlock, LoadingBlock } from '@/components/query-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api, errorMessage, unwrap, type Schemas } from '@/lib/api'
import { dateTime } from '@/lib/format'
import { qk } from '@/lib/queries'

export function NotesTab({ assetId }: { assetId: number }) {
  const notes = useQuery({
    queryKey: [...qk.asset(assetId), 'notes'],
    queryFn: () => unwrap(api.GET('/api/assets/{id}/notes', { params: { path: { id: assetId } } })),
  })
  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
      <NewNote assetId={assetId} />
      <div className="space-y-3">
        {notes.isPending ? (
          <LoadingBlock rows={3} />
        ) : notes.isError ? (
          <ErrorBlock error={notes.error} />
        ) : (notes.data ?? []).length === 0 ? (
          <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
            No research notes yet. Capture what you read and why it matters.
          </p>
        ) : (
          (notes.data ?? []).map((n) => <NoteCard key={n.id} note={n} assetId={assetId} />)
        )}
      </div>
    </div>
  )
}

function useInvalidate(assetId: number) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: qk.asset(assetId) })
    void qc.invalidateQueries({ queryKey: qk.portfolio })
  }
}

function NewNote({ assetId }: { assetId: number }) {
  const invalidate = useInvalidate(assetId)
  const [text, setText] = useState('')
  const [sources, setSources] = useState('')
  const [opportunity, setOpportunity] = useState(false)
  const [expires, setExpires] = useState('')
  const [error, setError] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/api/assets/{id}/notes', {
          params: { path: { id: assetId } },
          body: {
            text,
            sources: sources
              .split('\n')
              .map((s) => s.trim())
              .filter(Boolean),
            opportunity,
            opportunity_expires_on: opportunity ? expires : undefined,
          },
        }),
      ),
    onSuccess: () => {
      toast.success('Note saved')
      setText('')
      setSources('')
      setOpportunity(false)
      setExpires('')
      setError(null)
      invalidate()
    },
    onError: (e) => setError(errorMessage(e)),
  })
  return (
    <Panel title="New note">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!text.trim()) return setError('Write the note first.')
          if (opportunity && !expires) return setError('An opportunity flag needs an expiry date.')
          create.mutate()
        }}
        className="space-y-3"
        noValidate
      >
        <Field label="Note">{(id) => <Textarea id={id} rows={5} value={text} onChange={(e) => setText(e.target.value)} />}</Field>
        <Field label="Sources" hint="One per line.">
          {(id) => <Textarea id={id} rows={2} value={sources} onChange={(e) => setSources(e.target.value)} />}
        </Field>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" className="accent-jade mt-0.5 size-4" checked={opportunity} onChange={(e) => setOpportunity(e.target.checked)} />
          <span>
            Flag as an opportunity catalyst
            <span className="text-muted-foreground block text-xs">
              Makes the asset an opportunity in the contribution planner until the expiry date.
            </span>
          </span>
        </label>
        {opportunity && (
          <Field label="Catalyst expires on" className="max-w-48">
            {(id) => <Input id={id} type="date" value={expires} onChange={(e) => setExpires(e.target.value)} />}
          </Field>
        )}
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={create.isPending}>
            Save note
          </Button>
          <FormError error={error} />
        </div>
      </form>
    </Panel>
  )
}

function NoteCard({ note: n, assetId }: { note: Schemas['Note']; assetId: number }) {
  const invalidate = useInvalidate(assetId)
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/api/notes/{id}', { params: { path: { id: n.id } } })),
    onSuccess: () => {
      toast.success('Note deleted')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  const clearFlag = useMutation({
    mutationFn: () => unwrap(api.PATCH('/api/notes/{id}', { params: { path: { id: n.id } }, body: { opportunity: false } })),
    onSuccess: () => {
      toast.success('Opportunity flag removed')
      invalidate()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })
  return (
    <article className="bg-card rounded-lg border p-4">
      <header className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="text-muted-foreground flex flex-wrap items-center gap-2 text-xs">
          <span className="num">{dateTime(n.created_at)}</span>
          {n.author === 'ai_approved' && <span className="rounded border px-1.5">AI draft, approved</span>}
          {n.opportunity && (
            <span
              className={
                n.opportunity_active
                  ? 'border-brass/50 text-brass rounded-full border px-2 font-medium'
                  : 'rounded-full border px-2'
              }
            >
              {n.opportunity_active ? 'Catalyst' : 'Catalyst expired'} · until {n.opportunity_expires_on}
            </span>
          )}
        </div>
        <div className="flex gap-1">
          {n.opportunity && (
            <Button size="xs" variant="ghost" disabled={clearFlag.isPending} onClick={() => clearFlag.mutate()}>
              Remove flag
            </Button>
          )}
          <Button size="icon-xs" variant="ghost" aria-label="Delete note" disabled={del.isPending} onClick={() => del.mutate()}>
            <Trash2 />
          </Button>
        </div>
      </header>
      <p className="text-sm whitespace-pre-wrap">{n.text}</p>
      {(n.sources ?? []).length > 0 && (
        <ul className="text-muted-foreground mt-2 space-y-0.5 text-xs">
          {(n.sources ?? []).map((s) => (
            <li key={s} className="truncate">
              {/^https?:\/\//.test(s) ? (
                <a href={s} target="_blank" rel="noreferrer" className="underline">
                  {s}
                </a>
              ) : (
                s
              )}
            </li>
          ))}
        </ul>
      )}
    </article>
  )
}
