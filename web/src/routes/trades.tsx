import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'

export const Route = createFileRoute('/trades')({ component: Page })

function Page() {
  return <PageHeader title="Trades & cash" description="Trades, cash movements, dividends (placeholder)." />
}
