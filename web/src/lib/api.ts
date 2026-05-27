// Minimal fetch wrapper that:
//   - attaches the Bearer access token to every request
//   - on 401, attempts a one-shot refresh via /v1/auth/refresh
//   - clears auth state and surfaces the error if refresh fails
//
// Tiny on purpose — TanStack Query handles caching, deduplication, and
// retries above this. This wrapper is the transport, not the data layer.
import { useAuthStore } from '@/stores/auth-store'

const REFRESH_PATH = '/v1/auth/refresh'

let inflightRefresh: Promise<string | null> | null = null

async function refreshAccessToken(): Promise<string | null> {
  const refreshToken = useAuthStore.getState().auth.refreshToken
  if (!refreshToken) return null

  const res = await fetch(REFRESH_PATH, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })

  if (!res.ok) return null

  const data = (await res.json()) as {
    access_token: string
    refresh_token: string
    expires_at: string
  }
  useAuthStore
    .getState()
    .auth.setTokens(data.access_token, data.refresh_token, data.expires_at)
  return data.access_token
}

async function ensureRefresh(): Promise<string | null> {
  if (!inflightRefresh) {
    inflightRefresh = refreshAccessToken().finally(() => {
      inflightRefresh = null
    })
  }
  return inflightRefresh
}

export type ApiError = {
  status: number
  code: string
  message: string
}

function isApiError(value: unknown): value is ApiError {
  if (!value || typeof value !== 'object') return false
  const v = value as Record<string, unknown>
  return (
    typeof v.status === 'number' &&
    typeof v.code === 'string' &&
    typeof v.message === 'string'
  )
}

export { isApiError }

async function parseError(res: Response): Promise<ApiError> {
  let code = `http_${res.status}`
  try {
    const body = (await res.json()) as { error?: string }
    if (body.error) code = body.error
  } catch {
    // body wasn't JSON; keep the http_<status> fallback
  }
  return { status: res.status, code, message: res.statusText }
}

export async function apiFetch<T>(
  input: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }

  const token = useAuthStore.getState().auth.accessToken
  if (token) headers.set('Authorization', `Bearer ${token}`)

  let res = await fetch(input, { ...init, headers })

  // On 401, try to refresh once and replay.
  if (res.status === 401 && useAuthStore.getState().auth.refreshToken) {
    const newToken = await ensureRefresh()
    if (newToken) {
      headers.set('Authorization', `Bearer ${newToken}`)
      res = await fetch(input, { ...init, headers })
    } else {
      useAuthStore.getState().auth.reset()
    }
  }

  if (!res.ok) {
    throw await parseError(res)
  }

  // 204 No Content
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}
