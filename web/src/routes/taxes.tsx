import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/taxes')({ component: Page })

function Page() {
  return <PageHeader title="Taxes" description="Monthly and annual tax views (placeholder)." />
}
