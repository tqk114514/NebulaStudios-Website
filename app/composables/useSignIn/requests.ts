import {
  asApiErrorBody,
  type ApiClient,
  type ApiErrorBody,
  type components,
} from '@nebula/api-client'

export type ApiUser = components['schemas']['User']

export type LoginOutcome =
  | { kind: 'ok', user: ApiUser }
  | { kind: 'totp', challengeToken: string }
  | { kind: 'failed', body?: ApiErrorBody }

export type TotpOutcome =
  | { kind: 'ok', user: ApiUser }
  | { kind: 'failed', body?: ApiErrorBody }

/** 口令步骤；401 `totp_required` 是流程分支，不是失败。 */
export async function requestLogin(
  api: ApiClient,
  identifier: string,
  password: string,
): Promise<LoginOutcome> {
  try {
    const { data, error } = await api.POST('/api/auth/login', {
      body: { identifier, password },
    })
    if (data) return { kind: 'ok', user: data }
    const body = asApiErrorBody(error)
    if (body?.code === 'totp_required') {
      const challengeToken = body.details?.challenge_token
      if (challengeToken) return { kind: 'totp', challengeToken }
    }
    return { kind: 'failed', body }
  } catch {
    // 网络中断没有 ErrorBody；由调用方落到通用失败文案
    return { kind: 'failed' }
  }
}

/** 二段提交；挑战令牌由密码步骤或社交回传，单次有效。 */
export async function requestLoginTotp(
  api: ApiClient,
  challengeToken: string,
  code: string,
): Promise<TotpOutcome> {
  try {
    const { data, error } = await api.POST('/api/auth/login/totp', {
      body: { challenge_token: challengeToken, code },
    })
    if (data) return { kind: 'ok', user: data }
    return { kind: 'failed', body: asApiErrorBody(error) }
  } catch {
    return { kind: 'failed' }
  }
}
