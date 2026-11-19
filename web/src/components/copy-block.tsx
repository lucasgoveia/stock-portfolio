import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'

/** A read-only snippet with a copy button. */
export function CopyBlock({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard access can be refused; the text stays selectable.
    }
  }
  return (
    <div className="bg-muted/50 mt-1 flex w-0 min-w-full items-start gap-2 rounded-md border p-2">
      <pre className="num min-w-0 flex-1 overflow-x-auto text-xs whitespace-pre" aria-label={label} tabIndex={0}>
        {text}
      </pre>
      <Button type="button" variant="outline" size="sm" onClick={copy} aria-label={`Copy ${label}`} className="shrink-0">
        {copied ? <Check /> : <Copy />}
        {copied ? 'Copied' : 'Copy'}
      </Button>
    </div>
  )
}
