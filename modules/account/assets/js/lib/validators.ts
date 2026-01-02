/**
 * 前端表单验证模块
 *
 * 功能：
 * - 邮箱格式和白名单验证
 * - 用户名长度验证
 * - 登录/注册表单验证
 */

import type { ValidationResult, LoadResult, EmailProviders, RegisterFormData } from '../../../../../shared/js/types/auth.js';

// ==================== 邮箱验证 ====================

/** 邮箱服务商白名单 */
let EMAIL_PROVIDERS: EmailProviders = {};

/**
 * 加载邮箱白名单
 */
export async function loadEmailWhitelist(): Promise<LoadResult> {
  try {
    const response = await fetch('/account/data/email.json');
    if (!response.ok) {throw new Error('Failed to load email whitelist');}
    EMAIL_PROVIDERS = await response.json();
    return { success: true };
  } catch (error) {
    console.error('[VALIDATOR] ERROR: Failed to load email whitelist:', (error as Error).message);
    return { success: false, error: (error as Error).message };
  }
}

/**
 * 验证邮箱格式
 */
export function isValidEmailFormat(email: string): boolean {
  if (!email || typeof email !== 'string') {return false;}
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return emailRegex.test(email);
}

/**
 * 检查邮箱是否在白名单中
 */
export function isEmailInWhitelist(email: string): boolean {
  if (!email || typeof email !== 'string') {return false;}
  const emailParts = email.toLowerCase().split('@');
  if (emailParts.length !== 2) {return false;}
  return Object.prototype.hasOwnProperty.call(EMAIL_PROVIDERS, emailParts[1]);
}

/**
 * 获取支持的邮箱域名列表
 */
export function getSupportedEmailDomains(): string[] {
  return Object.keys(EMAIL_PROVIDERS);
}

/**
 * 获取邮箱服务商信息（域名和注册链接）
 */
export function getEmailProviders(): EmailProviders {
  return EMAIL_PROVIDERS;
}

/**
 * 验证邮箱（格式 + 白名单）
 */
export function validateEmail(email: string): ValidationResult {
  if (!email || email.trim() === '') {
    return { valid: false, errorKey: 'register.emailRequired' };
  }
  if (!isValidEmailFormat(email)) {
    return { valid: false, errorKey: 'register.emailInvalid' };
  }
  if (!isEmailInWhitelist(email)) {
    return { valid: false, errorKey: 'register.emailNotSupported' };
  }
  return { valid: true };
}

// ==================== 用户名验证 ====================

/**
 * 验证用户名长度（1-15 字符）
 */
export function validateUsername(username: string): boolean {
  if (!username || typeof username !== 'string') {return false;}
  const length = username.trim().length;
  return length >= 1 && length <= 15;
}

/**
 * 检查用户名是否过长
 */
export function isUsernameTooLong(username: string): boolean {
  if (!username || typeof username !== 'string') {return false;}
  return username.trim().length > 15;
}

/**
 * 显示/隐藏用户名错误提示
 */
export function toggleUsernameError(show: boolean, usernameError: HTMLElement | null): void {
  if (!usernameError) {return;}
  usernameError.classList.toggle('is-hidden', !show);
}

/**
 * 处理用户名输入事件
 */
export function onUsernameInput(usernameInput: HTMLInputElement | null, usernameError: HTMLElement | null): void {
  if (!usernameInput) {return;}
  const username = (usernameInput.value || '').trim();
  const usernameErrorText = document.getElementById('username-error-text');
  const complianceLink = document.getElementById('check-username-compliance') as HTMLElement | null;

  if (username.length === 0) {
    toggleUsernameError(false, usernameError);
    if (complianceLink) {complianceLink.style.display = 'none';}
    return;
  }

  const isTooLong = isUsernameTooLong(username);

  if (isTooLong) {
    if (usernameErrorText) {
      usernameErrorText.setAttribute('data-i18n', 'register.usernameTooLong');
      usernameErrorText.textContent = window.t ? window.t('register.usernameTooLong') : '用户名过长';
    }
    toggleUsernameError(true, usernameError);
    if (complianceLink) {complianceLink.style.display = 'none';}
  } else {
    toggleUsernameError(false, usernameError);
    if (usernameErrorText) {
      usernameErrorText.textContent = '';
      usernameErrorText.removeAttribute('data-i18n');
    }
    if (complianceLink) {complianceLink.style.display = 'none';}
  }
}

/**
 * 处理用户名失去焦点事件
 */
export function onUsernameBlur(usernameInput: HTMLInputElement | null, usernameError: HTMLElement | null): void {
  if (!usernameInput) {return;}
  const username = (usernameInput.value || '').trim();
  const usernameErrorText = document.getElementById('username-error-text');
  const complianceLink = document.getElementById('check-username-compliance') as HTMLElement | null;

  if (username.length === 0) {
    toggleUsernameError(false, usernameError);
    if (complianceLink) {complianceLink.style.display = 'none';}
    return;
  }

  const isTooLong = isUsernameTooLong(username);

  if (isTooLong) {
    if (usernameErrorText) {
      usernameErrorText.setAttribute('data-i18n', 'register.usernameTooLong');
      usernameErrorText.textContent = window.t ? window.t('register.usernameTooLong') : '用户名过长';
    }
    toggleUsernameError(true, usernameError);
    if (complianceLink) {complianceLink.style.display = 'none';}
  } else {
    const isVerified = usernameInput.classList.contains('verified');

    if (isVerified) {
      toggleUsernameError(false, usernameError);
      if (complianceLink) {complianceLink.style.display = 'none';}
    } else {
      if (usernameErrorText) {
        usernameErrorText.textContent = '';
        usernameErrorText.removeAttribute('data-i18n');
      }
      if (complianceLink && usernameError) {
        usernameError.classList.remove('is-hidden');
        complianceLink.style.display = 'inline-block';
      }
    }
  }
}

// ==================== 表单验证 ====================

/**
 * 验证登录表单
 */
export function validateLoginForm(email: string, password: string): ValidationResult {
  if (!email || !password) {
    return { valid: false, errorKey: 'login.fillAllFields' };
  }
  return { valid: true };
}

/**
 * 验证密码强度
 */
export function validatePassword(password: string): ValidationResult {
  if (!password || typeof password !== 'string') {
    return { valid: false, errorKey: 'register.passwordRequired' };
  }
  if (password.length < 16 || password.length > 64) {
    return { valid: false, errorKey: 'register.passwordLength' };
  }
  if (!/\d/.test(password)) {
    return { valid: false, errorKey: 'register.passwordNumber' };
  }
  if (!/[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/.test(password)) {
    return { valid: false, errorKey: 'register.passwordSpecial' };
  }
  if (!/[A-Z]/.test(password) || !/[a-z]/.test(password)) {
    return { valid: false, errorKey: 'register.passwordCase' };
  }
  return { valid: true };
}

/**
 * 验证注册表单
 */
export function validateRegisterForm(formData: RegisterFormData): ValidationResult {
  // 必填字段检查
  if (!formData.username || !formData.email || !formData.verificationCode ||
      !formData.password || !formData.passwordConfirm) {
    return { valid: false, errorKey: 'register.fillAllFields' };
  }

  // 用户名长度
  if (!validateUsername(formData.username)) {
    return { valid: false, errorKey: 'register.usernameLength' };
  }

  // 密码一致性
  if (formData.password !== formData.passwordConfirm) {
    return { valid: false, errorKey: 'register.passwordMismatch' };
  }

  // 密码强度验证
  const passwordValidation = validatePassword(formData.password);
  if (!passwordValidation.valid) {
    return passwordValidation;
  }

  return { valid: true };
}

// ==================== 头像 URL 验证 ====================

/** 允许的图片后缀 */
const ALLOWED_IMAGE_EXTENSIONS = ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.ico'];

/** 特殊允许的域名（微软头像 API 没有后缀） */
const SPECIAL_ALLOWED_DOMAINS = ['graph.microsoft.com'];

/** 禁止的内网地址模式 */
const BLOCKED_HOST_PATTERNS = [
  /^localhost$/i,
  /^127\.\d+\.\d+\.\d+$/,
  /^10\.\d+\.\d+\.\d+$/,
  /^172\.(1[6-9]|2\d|3[01])\.\d+\.\d+$/,
  /^192\.168\.\d+\.\d+$/,
  /^0\.0\.0\.0$/,
];

/**
 * 验证头像 URL（前端验证）
 */
export function validateAvatarUrl(url: string): ValidationResult {
  if (!url || typeof url !== 'string' || url.trim() === '') {
    return { valid: false, errorKey: 'dashboard.invalidUrl' };
  }

  const trimmed = url.trim();

  // data URL 验证
  if (trimmed.startsWith('data:')) {
    if (trimmed.length > 500000) {
      return { valid: false, errorKey: 'dashboard.invalidUrl' };
    }
    if (!/^data:image\/(jpeg|jpg|png|gif|webp);base64,/.test(trimmed)) {
      return { valid: false, errorKey: 'dashboard.invalidUrl' };
    }
    return { valid: true };
  }

  // URL 长度限制
  if (trimmed.length > 2048) {
    return { valid: false, errorKey: 'dashboard.invalidUrl' };
  }

  // URL 格式验证
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return { valid: false, errorKey: 'dashboard.invalidUrl' };
  }

  // 只允许 http/https
  if (!['http:', 'https:'].includes(parsed.protocol)) {
    return { valid: false, errorKey: 'dashboard.invalidUrl' };
  }

  // 禁止内网地址
  const hostname = parsed.hostname.toLowerCase();
  for (const pattern of BLOCKED_HOST_PATTERNS) {
    if (pattern.test(hostname)) {
      return { valid: false, errorKey: 'dashboard.invalidUrl' };
    }
  }

  // 检查特殊域名
  const isSpecialDomain = SPECIAL_ALLOWED_DOMAINS.some(domain =>
    hostname === domain || hostname.endsWith('.' + domain)
  );

  if (!isSpecialDomain) {
    // 普通 URL 必须以图片后缀结尾
    const pathname = parsed.pathname.toLowerCase();
    const hasImageExtension = ALLOWED_IMAGE_EXTENSIONS.some(ext => pathname.endsWith(ext));

    if (!hasImageExtension) {
      return { valid: false, errorKey: 'dashboard.invalidImageUrl' };
    }
  }

  return { valid: true };
}
