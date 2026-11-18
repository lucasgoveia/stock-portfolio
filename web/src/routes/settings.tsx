import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/settings')({ component: Page })

function Page() {
  return <PageHeader title="Settings" description="Weights, thresholds, API keys, AI cap (placeholder)." />
}
