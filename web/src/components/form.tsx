import { useId } from 'react'

import { cn } from '@/lib/utils'

/** A labelled form control with an optional hint. The child receives the id. */
export function Field({
  label,
  hint,
  className,
  children,
}: {
  label: string
  hint?: React.ReactNode
  className?: string
  children: (id: string) => React.ReactNode
}) {
  const id = useId()
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', className)}>
      <label htmlFor={id} className="text-muted-foreground text-xs font-medium">
        {label}
      </label>
      {children(id)}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  )
}

/** Native select styled like Input: reliable on phones and with keyboards. */
export function NativeSelect({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      className={cn(
        'border-input bg-transparent focus-visible:border-ring focus-visible:ring-ring/50 h-8 w-full min-w-0 rounded-lg border px-2 text-base outline-none focus-visible:ring-3 md:text-sm dark:bg-input/30',
        className,
      )}
      {...props}
    />
  )
}

export function FormError({ error }: { error: string | null }) {
  if (!error) return null
  return (
    <p role="alert" className="text-breach text-sm">
      {error}
    </p>
  )
}
