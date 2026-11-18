import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'
import { parseAssetSearch } from '@/lib/search'

export const Route = createFileRoute('/assets/$assetId')({ validateSearch: parseAssetSearch, component: AssetPage })

function AssetPage() {
  const { assetId } = Route.useParams()
  return <PageHeader title={`Asset ${assetId}`} description="Asset page (placeholder)." />
}
