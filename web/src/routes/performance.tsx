import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/performance')({ component: Page })

function Page() {
  return <PageHeader title="Performance" description="TWR charts vs benchmarks (placeholder)." />
}
