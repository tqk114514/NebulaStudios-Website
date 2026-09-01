// 统一的 API 响应类型。与 Go 端 response.go 对应。
export interface ApiSuccess<T = Record<string, never>> {
  success: true
  message?: string
  data: T
}

export interface ApiError {
  success: false
  errorCode: string
  waitTime?: number
}

export type ApiResponse<T = Record<string, never>> = ApiSuccess<T> | ApiError

/** 业务错误（HTTP 200 但 success=false，或 HTTP 4xx 带 errorCode） */
export class ApiClientError extends Error {
  constructor(
    public readonly errorCode: string,
    public readonly httpStatus: number,
    public readonly waitTime?: number,
  ) {
    super(errorCode)
    this.name = 'ApiClientError'
  }
}

const API_BODY_JSON = 'application/json'

async function parseResponse<T>(res: Response): Promise<ApiResponse<T>> {
  // 204 / 空响应
  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return { success: true } as ApiSuccess<T>
  }
  let body: unknown
  try {
    body = await res.json()
  } catch {
    body = null
  }
  return body as ApiResponse<T>
}

/**
 * 带 CSRF 防护的通用请求封装。
 * - 状态变更请求（POST/PATCH/PUT/DELETE）自动携带 X-CSRF-Token（同源时读 token cookie）
 * - 非 2xx 统一映射为 ApiClientError
 */
export async function request<T = Record<string, never>>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ApiSuccess<T>['data']> {
  const headers: Record<string, string> = { Accept: API_BODY_JSON }

  const isBodyMethod = method !== 'GET' && method !== 'HEAD'
  if (body !== undefined) {
    headers['Content-Type'] = API_BODY_JSON
    // 状态变更请求附带 CSRF Double-Submit Cookie 令牌
    if (isBodyMethod) {
      const token = document.cookie
        .split(';')
        .map((s) => s.trim())
        .find((s) => s.startsWith('csrf_token='))
        ?.split('=')[1]
      if (token) headers['X-CSRF-Token'] = decodeURIComponent(token)
    }
  }

  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  const payload = await parseResponse<T>(res)

  if (!res.ok || !payload.success) {
    const code = payload.success === false ? payload.errorCode : 'HTTP_' + res.status
    const waitTime = payload.success === false ? payload.waitTime : undefined
    throw new ApiClientError(code, res.status, waitTime)
  }

  // success=true 时 data 可能缺失（如 login 返回 {message} 无 data）
  return payload.data ?? ({} as T)
}

export const get = <T>(path: string) => request<T>('GET', path)
export const post = <T>(path: string, body?: unknown) => request<T>('POST', path, body)
export const patch = <T>(path: string, body?: unknown) => request<T>('PATCH', path, body)
export const del = <T>(path: string) => request<T>('DELETE', path)

/** 取 CSRF 令牌（供个别需要显式传值的场景） */
export function getCsrfToken(): string {
  const token = document.cookie
    .split(';')
    .map((s) => s.trim())
    .find((s) => s.startsWith('csrf_token='))
    ?.split('=')[1]
  return token ? decodeURIComponent(token) : ''
}