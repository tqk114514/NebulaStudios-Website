import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema'

/** ADR-0004 错误信封；`details` 的形状由各错误码自行约定。 */
export type ApiErrorBody = components['schemas']['ErrorBody'] & {
  details?: {
    fields?: Array<{ field: string; code: string }>
    retry_after?: number
    challenge_token?: string
    reason?: string
  }
}

const CSRF_COOKIE = 'nebula_csrf'
const CSRF_HEADER = 'X-CSRF-Token'
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

/** 浏览器端读取非 HttpOnly 的 CSRF cookie；SSR 阶段没有 document，自动跳过。 */
function readCookie(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined
  const entry = document.cookie.split('; ').find((item) => item.startsWith(`${name}=`))
  return entry?.slice(name.length + 1)
}

/**
 * Double Submit 回显：cookie 鉴权的写请求自动带 `X-CSRF-Token`（ADR-0005）。
 * 同源 `fetch` 默认会带 cookie；跨源开发时由调用方把 baseUrl 指向后端并确认 CORS。
 */
const csrf: Middleware = {
  onRequest({ request }) {
    if (typeof document === 'undefined' || SAFE_METHODS.has(request.method)) return
    const token = readCookie(CSRF_COOKIE)
    if (token) request.headers.set(CSRF_HEADER, decodeURIComponent(token))
  },
}

/** 创建同源 API client；`baseUrl` 只在前后端分离部署时指向后端来源。 */
export function createApiClient(options: { baseUrl?: string } = {}) {
  const client = createClient<paths>({
    baseUrl: options.baseUrl ?? '',
    credentials: 'include',
  })
  client.use(csrf)
  return client
}

export type ApiClient = ReturnType<typeof createApiClient>

/**
 * 把 openapi-fetch 的 `error` 收窄成 ADR-0004 信封。
 * 生成的 `details` 是 `unknown`，这里只做结构校验后收窄字段类型。
 */
export function asApiErrorBody(error: unknown): ApiErrorBody | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const candidate = error as { code?: unknown; message?: unknown }
  if (typeof candidate.code !== 'string' || typeof candidate.message !== 'string') return undefined
  return error as ApiErrorBody
}

export type { components, paths }
