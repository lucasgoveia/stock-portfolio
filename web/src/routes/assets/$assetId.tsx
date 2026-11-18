import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/assets/$assetId')({ component: AssetPage })

function AssetPage() {
  const { assetId } = Route.useParams()
  return <PageHeader title={`Asset ${assetId}`} description="Asset page (placeholder)." />
}
