/**
 * 社交登录回调的状态解析：后端以 302 回 `/account/login` 并携带
 * `social=ok`、`social=totp_required&challenge_token=…` 或 `social_error=<slug>`
 * （ADR-0012、ADR-0016 决策 4）。这里只解析协议，不碰路由与文案。
 */

export type SocialCallback =
  | { kind: 'ok' }
  | { kind: 'totp', challengeToken: string }
  | { kind: 'error', slug: string }

const SOCIAL_ERROR_KEYS: Record<string, string> = {
  unknown_provider: 'login.socialErrorUnknownProvider',
  provider_disabled: 'login.socialErrorProviderDisabled',
  invalid_request: 'login.socialErrorInvalidRequest',
  invalid_state: 'login.socialErrorInvalidState',
  expired: 'login.socialErrorExpired',
  provider_error: 'login.socialErrorProviderError',
  not_linked: 'login.socialErrorNotLinked',
  account_unavailable: 'login.socialErrorAccountUnavailable',
  server_error: 'login.socialErrorServerError',
  already_linked: 'login.socialErrorAlreadyLinked',
}

function firstString(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value) && typeof value[0] === 'string') return value[0]
  return ''
}

/**
 * 解析回调查询参数；非社交回调返回 `null`。
 * `totp_required` 缺少挑战令牌时按无效状态处理，不进入二段步骤。
 */
export function parseSocialCallback(query: Record<string, unknown>): SocialCallback | null {
  const error = firstString(query.social_error)
  if (error) return { kind: 'error', slug: error }

  const status = firstString(query.social)
  if (status === 'ok') return { kind: 'ok' }
  if (status === 'totp_required') {
    const challengeToken = firstString(query.challenge_token).trim()
    return challengeToken
      ? { kind: 'totp', challengeToken }
      : { kind: 'error', slug: 'invalid_state' }
  }
  return null
}

/** 把后端 slug 映射到静态 i18n 键；未知 slug 落到通用失败文案。 */
export function socialErrorKey(slug: string): string {
  return SOCIAL_ERROR_KEYS[slug] ?? 'login.socialErrorGeneric'
}
