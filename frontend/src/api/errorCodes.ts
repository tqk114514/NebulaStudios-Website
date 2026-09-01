// 后端错误码 → i18n key 映射（对齐原 shared 前端 errorCodeMap）。
// 注：业务 key 带 account. 前缀（登录/注册等归属 account 模块）。

import { ApiClientError } from '@/api/client'

export const errorCodeMap: Record<string, string> = {
  // 通用
  RATE_LIMIT: 'account.register.waitRetry',
  LOGIN_RATE_LIMIT: 'account.login.rateLimitExceeded',
  CAPTCHA_FAILED: 'account.login.humanVerifyFailed',
  NETWORK_ERROR: 'error.networkError',
  SERVER_ERROR: 'error.serverError',
  MISSING_PARAMETERS: 'account.register.fillAllFields',
  CONFIG_FAILED: 'error.configFailed',
  TOKEN_CREATE_FAILED: 'error.tokenCreateFailed',

  // 邮箱
  INVALID_EMAIL: 'account.register.emailInvalid',
  EMAIL_NOT_SUPPORTED: 'account.register.emailNotSupported',
  EMAIL_ALREADY_EXISTS: 'account.register.emailExists',
  EMAIL_NOT_FOUND: 'error.emailNotFound',
  SEND_FAILED: 'account.register.sendFailed',

  // 验证码
  VERIFICATION_CODE_INVALID: 'account.register.codeInvalid',
  VERIFICATION_CODE_EXPIRED: 'account.register.codeExpired',
  INVALID_CODE: 'account.register.codeInvalid',
  VERIFY_FAILED: 'error.verifyFailed',
  CHECK_FAILED: 'error.checkFailed',
  INVALIDATE_FAILED: 'error.invalidateFailed',

  // 用户名
  USERNAME_ALREADY_EXISTS: 'account.register.usernameExists',
  INVALID_USERNAME: 'account.register.usernameInvalid',
  USERNAME_TOO_SHORT: 'account.register.usernameTooShort',
  USERNAME_TOO_LONG: 'account.register.usernameTooLong',

  // 密码
  INVALID_PASSWORD: 'account.register.passwordInvalid',
  PASSWORD_TOO_SHORT: 'account.register.passwordLength',
  PASSWORD_TOO_LONG: 'account.register.passwordLength',
  PASSWORD_NO_NUMBER: 'account.register.passwordNumber',
  PASSWORD_NO_SPECIAL: 'account.register.passwordSpecial',
  PASSWORD_NO_CASE: 'account.register.passwordCase',
  WRONG_PASSWORD: 'error.wrongPassword',
  SAME_PASSWORD: 'error.samePassword',

  // 注册/登录
  REGISTER_FAILED: 'account.register.failed',
  INVALID_CREDENTIALS: 'account.login.invalidCredentials',
  LOGIN_FAILED: 'account.login.failed',

  // 会话
  NO_TOKEN: 'error.sessionExpired',
  TOKEN_EXPIRED: 'error.sessionExpired',
  INVALID_TOKEN: 'error.sessionInvalid',
  TOKEN_ERROR: 'error.sessionError',
  INVALID_SESSION: 'error.sessionInvalid',
  SESSION_EXPIRED: 'error.sessionExpired',
  SESSION_VERIFY_FAILED: 'error.sessionVerifyFailed',
  GET_USER_FAILED: 'error.getUserFailed',
  LOGOUT_FAILED: 'error.logoutFailed',
  TOKEN_GENERATION_FAILED: 'error.tokenGenerationFailed',

  // 用户
  USER_NOT_FOUND: 'error.userNotFound',
  UPDATE_FAILED: 'error.updateFailed',
  DELETE_FAILED: 'error.deleteFailed',
  RESET_FAILED: 'error.resetFailed',
  INVALID_AVATAR_URL: 'error.invalidAvatarUrl',

  // OAuth
  OAUTH_NOT_CONFIGURED: 'error.oauthNotConfigured',
  NOT_LINKED: 'error.notLinked',
  UNLINK_FAILED: 'error.unlinkFailed',
  MICROSOFT_ALREADY_LINKED: 'error.microsoftAlreadyLinked',
  FETCH_FAILED: 'error.fetchFailed',
  LINK_FAILED: 'error.linkFailed',
}

const FALLBACK = 'account.login.failed'

/** 根据 API 错误映射到 i18n key（带兜底）。未登录(401/403)视作登录失败。 */
export function errorKey(e: unknown): string {
  if (e instanceof ApiClientError) {
    return errorCodeMap[e.errorCode] ?? FALLBACK
  }
  return 'error.networkError'
}