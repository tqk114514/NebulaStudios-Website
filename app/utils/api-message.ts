import type { ApiErrorBody } from '@nebula/api-client'

/** 跨端点通用的错误码到文案键；端点特有字段错误由调用方优先处理。 */
const MESSAGE_KEYS: Record<string, string> = {
  invalid_credentials: 'api.invalidCredentials',
  banned: 'api.banned',
  rate_limited: 'api.rateLimited',
  turnstile_invalid: 'api.turnstileInvalid',
  turnstile_unavailable: 'api.turnstileUnavailable',
  email_taken: 'api.emailTaken',
  username_taken: 'api.usernameTaken',
}

/** 未登记的错误码一律落到通用失败文案，不把后端 message 直接展示给用户。 */
export function apiMessageKey(body: ApiErrorBody | undefined): string {
  return body ? (MESSAGE_KEYS[body.code] ?? 'api.genericError') : 'api.genericError'
}

/** `details.fields` 的首项；服务端一次返回多项时由前端一次只展示一项。 */
export function firstFieldError(
  body: ApiErrorBody | undefined,
): { field: string; code: string } | undefined {
  const fields = body?.details?.fields
  return Array.isArray(fields) ? fields[0] : undefined
}
