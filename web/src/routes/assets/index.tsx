import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/assets/')({ component: Page })

function Page() {
  return <PageHeader title="Assets" description="Asset list (placeholder). Open an asset by id at /assets/:assetId." />
}
