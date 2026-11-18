import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/watchlist')({ component: Page })

function Page() {
  return <PageHeader title="Watchlist" description="Watchlist and buy gate checklist (placeholder)." />
}
