import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/alerts')({ component: Page })

function Page() {
  return <PageHeader title="Alerts" description="Alerts inbox (placeholder)." />
}
