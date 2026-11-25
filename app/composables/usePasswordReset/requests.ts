import { asApiErrorBody, type ApiClient, type ApiErrorBody } from '@nebula/api-client'

export type VoidResult = { kind: 'ok' } | { kind: 'failed', body?: ApiErrorBody }

/** 发重置码；未注册与冷却中同样 204，调用方只能提示“如果已注册”。 */
export async function requestResetCode(
  api: ApiClient,
  email: string,
  turnstileToken?: string,
): Promise<VoidResult> {
  try {
    const { error } = await api.POST('/api/auth/password/reset/request', {
      body: { email, turnstile_token: turnstileToken },
    })
    return error ? { kind: 'failed', body: asApiErrorBody(error) } : { kind: 'ok' }
  } catch {
    return { kind: 'failed' }
  }
}

/** 验码并重置；成功会撤销全部会话并清除 cookie。 */
export async function confirmReset(
  api: ApiClient,
  email: string,
  code: string,
  newPassword: string,
): Promise<VoidResult> {
  try {
    const { error } = await api.POST('/api/auth/password/reset/confirm', {
      body: { email, code, new_password: newPassword },
    })
    return error ? { kind: 'failed', body: asApiErrorBody(error) } : { kind: 'ok' }
  } catch {
    return { kind: 'failed' }
  }
}
