import type { ErrorBody, Session } from './types'

/** A non-2xx answer from the Gateway, carrying the service's own error message. */
export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  /**
   * The service has no such route. Every service answers its own errors as JSON, so a 404 without
   * a JSON body is the router's, not "no such record": the endpoint is not built yet.
   */
  readonly notServed: boolean

  constructor(status: number, body: ErrorBody | null) {
    super(body?.error ?? `Request failed with status ${status}`)
    this.status = status
    this.code = body?.code
    this.notServed = status === 404 && body === null
  }
}

/**
 * Every API call goes under /api, so no API path can collide with a page route — a reload of
 * /job-openings/123 must reach the app, not the Hiring Service. The dev proxy (and, later,
 * whatever serves the app) strips the prefix before forwarding to the Gateway, so the Gateway's
 * own routes stay /auth/* and /job-openings/*.
 */
export const API_BASE = '/api'

// ADR-009: the refresh token is kept where scripts cannot read it. The gateway moves it out of
// the Identity Service's answer into an HttpOnly cookie when asked to with this header, so this
// code never sees it; the browser sends the cookie on /auth/refresh by itself. The access token
// lives in memory only, and a reload gets a fresh one with the cookie.
export const COOKIE_TRANSPORT = { 'X-Refresh-Token-Transport': 'cookie' }

let accessToken: string | null = null
let onSessionLost: () => void = () => {}

/** Holds the access token from a sign-in or refresh, or forgets it (null). */
export function setSession(session: Session | null) {
  accessToken = session?.access_token ?? null
}

/** Called when a refresh fails, so the app can return to the sign-in page. */
export function setSessionLostHandler(handler: () => void) {
  onSessionLost = handler
}

let refreshing: Promise<Session | null> | null = null

/**
 * Exchanges the refresh cookie for the next pair. A refresh token is used once, so concurrent
 * callers share one request — two exchanges of the same token look like a stolen copy and end
 * the session.
 */
export function refreshSession(): Promise<Session | null> {
  refreshing ??= fetch(`${API_BASE}/auth/refresh`, { method: 'POST', headers: COOKIE_TRANSPORT })
    .then(async (res) => {
      if (!res.ok) {
        setSession(null)
        return null
      }
      const session = (await res.json()) as Session
      setSession(session)
      return session
    })
    .catch(() => null)
    .finally(() => {
      refreshing = null
    })
  return refreshing
}

interface RequestOptions {
  method?: string
  json?: unknown
  body?: BodyInit
  /** Set on the sign-in and refresh calls themselves, which must not trigger a refresh. */
  anonymous?: boolean
  headers?: Record<string, string>
}

/**
 * Calls the Gateway. An expired access token (401) is renewed once and the request retried,
 * which is where ADR-009's fifteen-minute token is absorbed.
 */
export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const send = () => {
    const headers: Record<string, string> = { ...options.headers }
    if (options.json !== undefined) headers['Content-Type'] = 'application/json'
    if (accessToken && !options.anonymous) headers.Authorization = `Bearer ${accessToken}`
    return fetch(API_BASE + path, {
      method: options.method ?? 'GET',
      headers,
      body: options.json !== undefined ? JSON.stringify(options.json) : options.body,
    })
  }

  let res = await send()
  if (res.status === 401 && !options.anonymous) {
    const session = await refreshSession()
    if (!session) {
      onSessionLost()
      throw new ApiError(401, { error: 'Your session has ended. Please sign in again.' })
    }
    res = await send()
  }

  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as ErrorBody | null
    throw new ApiError(res.status, body)
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}
