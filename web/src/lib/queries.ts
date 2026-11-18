import { queryOptions } from '@tanstack/react-query'

import { api, unwrap } from './api'

// Query keys are grouped so a mutation can invalidate a whole area.
export const qk = {
  portfolio: ['portfolio'] as const,
  alerts: ['alerts'] as const,
  jobs: ['jobs'] as const,
  assets: ['assets'] as const,
  asset: (id: number) => ['asset', id] as const,
  cash: ['cash'] as const,
  trims: ['trims'] as const,
}

export const portfolioQuery = queryOptions({
  queryKey: qk.portfolio,
  queryFn: () => unwrap(api.GET('/api/portfolio')),
})

export const openAlertsQuery = queryOptions({
  queryKey: [...qk.alerts, 'open'],
  queryFn: () => unwrap(api.GET('/api/alerts', { params: { query: { state: 'open' } } })),
})

export const lastJobQuery = queryOptions({
  queryKey: [...qk.jobs, 'last'],
  // 404 means the job never ran; report that as null instead of an error.
  queryFn: async () => {
    const { data, response } = await api.GET('/api/jobs/daily/last')
    if (response.status === 404) return null
    if (!response.ok || !data) throw new Error(`last job: ${response.status}`)
    return data
  },
})

export const assetsQuery = queryOptions({
  queryKey: qk.assets,
  queryFn: () => unwrap(api.GET('/api/assets')),
})

export const cashSummaryQuery = queryOptions({
  queryKey: [...qk.cash, 'summary'],
  queryFn: () => unwrap(api.GET('/api/cash/summary')),
})

export const instrumentsQuery = queryOptions({
  queryKey: [...qk.cash, 'instruments'],
  queryFn: () => unwrap(api.GET('/api/cash/instruments')),
})

export const trimsQuery = queryOptions({
  queryKey: qk.trims,
  queryFn: () => unwrap(api.GET('/api/trims')),
})
