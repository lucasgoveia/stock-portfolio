import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'
import { parseTradeSearch } from '@/lib/search'

export const Route = createFileRoute('/trades')({ validateSearch: parseTradeSearch, component: Page })

function Page() {
  return <PageHeader title="Trades & cash" description="Trades, cash movements, dividends (placeholder)." />
}
