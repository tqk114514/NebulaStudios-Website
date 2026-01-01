/**
 * 认证 API 模块
 *
 * 功能：
 * - 发送验证码
 * - 用户注册
 * - 用户登录
 * - 会话验证
 * - 登出
 * - 错误码映射
 */

import type { ApiResponse, User, RegisterFormData, AuthResponse, SendCodeResponse } from '../../../../../../shared/js/types/auth.ts';

// ==================== API 调用 ====================

/**
 * 发送验证码
 */
export async function sendVerificationCode(
  email: string,
  captchaToken: string,
  captchaType: string
): Promise<SendCodeResponse> {
  try {
    const currentLanguage = window.currentLanguage || 'zh-CN';

    const response = await fetch('/api/auth/send-code', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email: email,
        captchaToken: captchaToken,
        captchaType: captchaType,
        language: currentLanguage
      })
    });

    const data: ApiResponse<{ expireTime?: number; email?: string }> = await response.json();

    if (response.ok && data.success) {
      return {
        success: true,
        message: data.message,
        expireTime: data.data?.expireTime,
        email: data.data?.email
      };
    } else {
      return {
        success: false,
        errorCode: data.errorCode || 'UNKNOWN_ERROR'
      };
    }
  } catch (error) {
    console.error('[AUTH] ERROR: Send verification code failed:', (error as Error).message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}

/**
 * 用户注册
 */
export async function register(formData: RegisterFormData): Promise<AuthResponse> {
  try {
    const response = await fetch('/api/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(formData)
    });

    const data: ApiResponse<User> = await response.json();

    if (response.ok && data.success) {
      return { success: true, data: data.data };
    } else {
      return { success: false, errorCode: data.errorCode || 'UNKNOWN_ERROR' };
    }
  } catch (error) {
    console.error('[AUTH] ERROR: Registration failed:', (error as Error).message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}

/**
 * 用户登录
 */
export async function login(
  email: string,
  password: string,
  captchaToken: string,
  captchaType: string
): Promise<AuthResponse> {
  try {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email: email,
        password: password,
        captchaToken: captchaToken,
        captchaType: captchaType
      })
    });

    const data: ApiResponse<User> = await response.json();

    if (response.ok && data.success) {
      return { success: true, data: data.data };
    } else {
      return { success: false, errorCode: data.errorCode || 'UNKNOWN_ERROR' };
    }
  } catch (error) {
    console.error('[AUTH] ERROR: Login failed:', (error as Error).message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}

/**
 * 验证会话有效性（从 cookie 读取 token）
 */
export async function verifySession(): Promise<AuthResponse> {
  try {
    const response = await fetch('/api/auth/verify-session', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' }
    });

    const data: ApiResponse<User> = await response.json();

    if (response.ok && data.success) {
      return { success: true, data: data.data };
    } else {
      return { success: false, errorCode: data.errorCode || 'INVALID_SESSION' };
    }
  } catch (error) {
    console.error('[AUTH] ERROR: Session verification failed:', (error as Error).message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}

/**
 * 登出（调用后端清除 cookie 并跳转登录页）
 */
export async function logout(): Promise<void> {
  try {
    await fetch('/api/auth/logout', {
      method: 'POST',
      credentials: 'include'
    });
  } catch (error) {
    console.error('[AUTH] ERROR: Logout failed:', (error as Error).message);
  }
  window.location.href = '/account/login';
}

// ==================== 错误码映射 ====================

/**
 * 错误码到翻译键的映射
 */
export const errorCodeMap: Record<string, string> = {
  // 通用错误
  'RATE_LIMIT': 'register.waitRetry',
  'LOGIN_RATE_LIMIT': 'login.rateLimitExceeded',
  'CAPTCHA_FAILED': 'login.humanVerifyFailed',
  'NETWORK_ERROR': 'error.networkError',
  'UNKNOWN_ERROR': 'register.sendFailed',
  'MISSING_PARAMETERS': 'register.fillAllFields',
  'CONFIG_FAILED': 'error.configFailed',
  'TOKEN_CREATE_FAILED': 'error.tokenCreateFailed',

  // 邮箱相关
  'INVALID_EMAIL': 'register.emailInvalid',
  'EMAIL_NOT_SUPPORTED': 'register.emailNotSupported',
  'EMAIL_ALREADY_EXISTS': 'register.emailExists',
  'EMAIL_ALREADY_REGISTERED': 'register.emailAlreadyRegistered',
  'EMAIL_NOT_FOUND': 'error.emailNotFound',
  'SEND_FAILED': 'register.sendFailed',

  // 验证码相关
  'VERIFICATION_CODE_INVALID': 'register.codeInvalid',
  'VERIFICATION_CODE_EXPIRED': 'register.codeExpired',
  'INVALID_CODE': 'register.codeInvalid',
  'VERIFY_FAILED': 'error.verifyFailed',
  'CHECK_FAILED': 'error.checkFailed',
  'INVALIDATE_FAILED': 'error.invalidateFailed',

  // 用户名相关
  'USERNAME_ALREADY_EXISTS': 'register.usernameExists',
  'INVALID_USERNAME': 'register.usernameInvalid',
  'USERNAME_TOO_SHORT': 'register.usernameTooShort',
  'USERNAME_TOO_LONG': 'register.usernameTooLong',

  // 密码相关
  'INVALID_PASSWORD': 'register.passwordInvalid',
  'PASSWORD_TOO_SHORT': 'register.passwordLength',
  'PASSWORD_TOO_LONG': 'register.passwordLength',
  'PASSWORD_NO_NUMBER': 'register.passwordNumber',
  'PASSWORD_NO_SPECIAL': 'register.passwordSpecial',
  'PASSWORD_NO_CASE': 'register.passwordCase',
  'WRONG_PASSWORD': 'error.wrongPassword',
  'SAME_PASSWORD': 'error.samePassword',

  // 注册/登录
  'REGISTER_FAILED': 'register.failed',
  'INVALID_CREDENTIALS': 'login.invalidCredentials',
  'LOGIN_FAILED': 'login.failed',

  // 会话相关
  'NO_TOKEN': 'error.sessionExpired',
  'TOKEN_EXPIRED': 'error.sessionExpired',
  'INVALID_TOKEN': 'error.sessionInvalid',
  'TOKEN_ERROR': 'error.sessionError',
  'INVALID_SESSION': 'error.sessionInvalid',
  'SESSION_VERIFY_FAILED': 'error.sessionVerifyFailed',
  'GET_USER_FAILED': 'error.getUserFailed',
  'LOGOUT_FAILED': 'error.logoutFailed',
  'TOKEN_GENERATION_FAILED': 'error.tokenGenerationFailed',

  // 用户相关
  'USER_NOT_FOUND': 'error.userNotFound',
  'UPDATE_FAILED': 'error.updateFailed',
  'DELETE_FAILED': 'error.deleteFailed',
  'RESET_FAILED': 'error.resetFailed',
  'INVALID_AVATAR_URL': 'error.invalidAvatarUrl',

  // OAuth 相关
  'OAUTH_NOT_CONFIGURED': 'error.oauthNotConfigured',
  'NOT_LINKED': 'error.notLinked',
  'UNLINK_FAILED': 'error.unlinkFailed',
  'MICROSOFT_ALREADY_LINKED': 'error.microsoftAlreadyLinked',
  'FETCH_FAILED': 'error.fetchFailed',
  'LINK_FAILED': 'error.linkFailed'
};
