import { Skeleton } from '@/components/ui/skeleton'
import { ApiError } from '@/lib/api'

export function LoadingBlock({ rows = 3 }: { rows?: number }) {
  return (
    <div className="space-y-2" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}

export function ErrorBlock({ error }: { error: unknown }) {
  const msg = error instanceof ApiError ? `${error.message} (HTTP ${error.status})` : String(error)
  return (
    <div role="alert" className="border-breach/40 bg-breach/5 text-breach rounded-md border p-3 text-sm">
      Could not load this: {msg}. Check that the server is running, then reload.
    </div>
  )
}
