import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/contributions')({ component: Page })

function Page() {
  return <PageHeader title="Contributions" description="Contribution planner (placeholder)." />
}
