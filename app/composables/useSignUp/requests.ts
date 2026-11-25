import {
  asApiErrorBody,
  type ApiClient,
  type ApiErrorBody,
  type components,
} from '@nebula/api-client'

type ApiUser = components['schemas']['User']

export type SignUpResult =
  | { kind: 'ok', user: ApiUser }
  | { kind: 'failed', body?: ApiErrorBody }

export type VoidResult = { kind: 'ok' } | { kind: 'failed', body?: ApiErrorBody }

/** 注册；成功返回未验证用户，验证码随后由邮件发出。 */
export async function requestRegister(
  api: ApiClient,
  body: { email: string, username: string, password: string },
  turnstileToken?: string,
): Promise<SignUpResult> {
  try {
    const { data, error } = await api.POST('/api/auth/register', {
      body: { ...body, turnstile_token: turnstileToken },
    })
    return data ? { kind: 'ok', user: data } : { kind: 'failed', body: asApiErrorBody(error) }
  } catch {
    return { kind: 'failed' }
  }
}

/** 邮箱验证码校验；204 无响应体。 */
export async function requestVerify(
  api: ApiClient,
  email: string,
  code: string,
): Promise<VoidResult> {
  try {
    const { error } = await api.POST('/api/auth/register/verify', { body: { email, code } })
    return error ? { kind: 'failed', body: asApiErrorBody(error) } : { kind: 'ok' }
  } catch {
    return { kind: 'failed' }
  }
}

/** 重发验证码；未注册与冷却中同样 204，不能据此判断账户是否存在。 */
export async function requestResend(
  api: ApiClient,
  email: string,
  turnstileToken?: string,
): Promise<VoidResult> {
  try {
    const { error } = await api.POST('/api/auth/register/resend', {
      body: { email, turnstile_token: turnstileToken },
    })
    return error ? { kind: 'failed', body: asApiErrorBody(error) } : { kind: 'ok' }
  } catch {
    return { kind: 'failed' }
  }
}
