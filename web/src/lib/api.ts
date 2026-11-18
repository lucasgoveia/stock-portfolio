import createClient from 'openapi-fetch'

import type { components, paths } from './api.gen'

// Types generated from api/openapi.json (`pnpm gen:api`).
export type Schemas = components['schemas']

export const api = createClient<paths>({ baseUrl: '' })

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

/** Resolves an openapi-fetch call to its data, throwing ApiError on non-2xx. */
export async function unwrap<T>(call: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await call
  if (!response.ok) {
    const msg = (error as Schemas['Error'] | undefined)?.error ?? response.statusText
    throw new ApiError(response.status, msg)
  }
  return data as T
}

export const fetchHealth = () => unwrap(api.GET('/api/health'))

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
