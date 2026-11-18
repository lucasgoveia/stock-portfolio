import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'

import { PageHeader } from '@/components/page-header'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { fetchHealth } from '@/lib/api'

export const Route = createFileRoute('/')({ component: Dashboard })

function Dashboard() {
  const { data, isPending, isError } = useQuery({ queryKey: ['health'], queryFn: fetchHealth })
  return (
    <>
      <PageHeader title="Dashboard" description="Portfolio overview (placeholder)." />
      <Card className="max-w-sm">
        <CardHeader>
          <CardTitle>API status</CardTitle>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <Badge variant="secondary">checking...</Badge>
          ) : isError ? (
            <Badge variant="destructive">unreachable</Badge>
          ) : (
            <Badge>{data.status}</Badge>
          )}
        </CardContent>
      </Card>
    </>
  )
}
