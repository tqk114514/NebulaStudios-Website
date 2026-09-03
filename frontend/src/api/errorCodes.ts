// 后端错误码 → i18n key 的统一注册处。
// 约定：
// - 所有错误文案一律挂在 error.* 命名空间（src/i18n/sources/general/），五语言同步维护；
// - 后端新增错误码时在此登记一行并补充文案；未登记的码回退 GENERIC_ERROR_KEY 通用文案；
// - 页面本地映射仅保留"同一错误码在不同流程需要更具体文案"的上下文覆盖
//   （如 LinkPage 的绑定链接过期、VerifyPage 的验证链接状态），最终展示走覆盖优先。

import { ApiClientError } from '@/api/client'

export const errorCodeMap: Record<string, string> = {
  // ---- 通用 ----
  RATE_LIMIT: 'error.rateLimitExceeded',
  LOGIN_RATE_LIMIT: 'error.rateLimitExceeded',
  CAPTCHA_FAILED: 'error.humanVerifyFailed',
  NETWORK_ERROR: 'error.networkError',
  SERVER_ERROR: 'error.serverError',
  MISSING_PARAMETERS: 'error.missingParameters',
  CONFIG_FAILED: 'error.configFailed',
  TOKEN_CREATE_FAILED: 'error.tokenCreateFailed',
  INTERNAL_ERROR: 'error.serverError',
  SERVICE_UNAVAILABLE: 'error.serverError',
  REQUEST_TOO_LARGE: 'error.invalidRequest',
  INVALID_REQUEST: 'error.invalidRequest',

  // ---- 邮箱 ----
  INVALID_EMAIL: 'error.emailInvalid',
  EMAIL_NOT_SUPPORTED: 'error.emailNotSupported',
  EMAIL_DOMAIN_NOT_ALLOWED: 'error.emailDomainNotAllowed',
  EMAIL_ALREADY_EXISTS: 'error.emailExists',
  EMAIL_NOT_FOUND: 'error.emailNotFound',
  SEND_FAILED: 'error.sendFailed',

  // ---- 验证码与令牌 ----
  VERIFICATION_CODE_INVALID: 'error.codeInvalid',
  VERIFICATION_CODE_EXPIRED: 'error.codeExpired',
  INVALID_CODE: 'error.codeInvalid',
  CODE_EXPIRED: 'error.codeExpired',
  CODE_USED: 'error.codeInvalid',
  TOKEN_USED: 'error.tokenUsed',
  INVALID_TOKEN: 'error.sessionInvalid',
  TOKEN_EXPIRED: 'error.sessionExpired',
  TOKEN_INVALID: 'error.sessionInvalid',
  VERIFY_FAILED: 'error.verifyFailed',
  CHECK_FAILED: 'error.checkFailed',
  INVALIDATE_FAILED: 'error.invalidateFailed',
  INVALID_REFRESH_TOKEN: 'error.sessionInvalid',
  REFRESH_TOKEN_EXPIRED: 'error.sessionExpired',
  REFRESH_TOKEN_REUSED: 'error.sessionInvalid',
  NO_REFRESH_TOKEN: 'error.sessionExpired',
  NO_TOKEN: 'error.sessionExpired',
  TOKEN_ERROR: 'error.sessionError',
  INVALID_SESSION: 'error.sessionInvalid',
  SESSION_EXPIRED: 'error.sessionExpired',
  SESSION_VERIFY_FAILED: 'error.sessionVerifyFailed',
  GET_USER_FAILED: 'error.getUserFailed',
  LOGOUT_FAILED: 'error.logoutFailed',
  TOKEN_GENERATION_FAILED: 'error.tokenGenerationFailed',
  TOKEN_GENERATE_FAILED: 'error.tokenGenerationFailed',

  // ---- 用户名与密码 ----
  USERNAME_ALREADY_EXISTS: 'error.usernameExists',
  INVALID_USERNAME: 'error.usernameInvalid',
  USERNAME_TOO_SHORT: 'error.usernameTooShort',
  USERNAME_TOO_LONG: 'error.usernameTooLong',
  INVALID_PASSWORD: 'error.passwordInvalid',
  PASSWORD_TOO_SHORT: 'error.passwordLength',
  PASSWORD_TOO_LONG: 'error.passwordLength',
  PASSWORD_NO_NUMBER: 'error.passwordNumber',
  PASSWORD_NO_SPECIAL: 'error.passwordSpecial',
  PASSWORD_NO_CASE: 'error.passwordCase',
  WRONG_PASSWORD: 'error.wrongPassword',
  SAME_PASSWORD: 'error.samePassword',

  // ---- 注册 / 登录 ----
  REGISTER_FAILED: 'error.registerFailed',
  INVALID_CREDENTIALS: 'error.invalidCredentials',
  LOGIN_FAILED: 'error.loginFailed',

  // ---- 用户 ----
  USER_NOT_FOUND: 'error.userNotFound',
  USER_BANNED: 'error.userBanned',
  UPDATE_FAILED: 'error.updateFailed',
  DELETE_FAILED: 'error.deleteFailed',
  RESET_FAILED: 'error.resetFailed',
  INVALID_AVATAR_URL: 'error.invalidAvatarUrl',

  // ---- OAuth 绑定 ----
  OAUTH_NOT_CONFIGURED: 'error.oauthNotConfigured',
  NOT_LINKED: 'error.notLinked',
  UNLINK_FAILED: 'error.unlinkFailed',
  MICROSOFT_ALREADY_LINKED: 'error.microsoftAlreadyLinked',
  GOOGLE_ALREADY_LINKED: 'error.googleAlreadyLinked',
  FETCH_FAILED: 'error.fetchFailed',
  LINK_FAILED: 'error.linkFailed',
  INVALID_LINK_TOKEN: 'error.linkTokenInvalid',
  LINK_TOKEN_EXPIRED: 'error.linkTokenExpired',

  // ---- OAuth 协议错误（snake_case，/oauth 端点返回） ----
  invalid_request: 'error.invalidRequest',
  invalid_client: 'error.oauthClientInvalid',
  invalid_scope: 'error.oauthScopeInvalid',
  access_denied: 'error.oauthAccessDenied',
  unauthorized: 'error.oauthUnauthorized',
  server_error: 'error.serverError',
  unsupported_response_type: 'error.oauthResponseTypeUnsupported',
}

/** 通用兜底文案：未登记的错误码统一显示"操作失败" */
export const GENERIC_ERROR_KEY = 'error.operationFailed'

const FALLBACK = GENERIC_ERROR_KEY

/** 根据 API 错误映射到 i18n key（带兜底）。非 ApiClientError（fetch 抛错等）视为网络错误。 */
export function errorKey(e: unknown): string {
  if (e instanceof ApiClientError) {
    return errorCodeMap[e.errorCode] ?? FALLBACK
  }
  return 'error.networkError'
}
