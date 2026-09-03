// 邮箱白名单校验（迁移自 lib/validators 中的邮箱部分）。
import { ref } from 'vue'

interface Provider {
  logo_url?: string
  signup_url?: string
}

export type EmailProviders = Record<string, Provider>

const EMAIL_REGEX = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

// 模块级单例缓存，避免每次进入页面重复请求
const providers = ref<EmailProviders>({})

export function isValidEmailFormat(email: string): boolean {
  return EMAIL_REGEX.test(email)
}

export function isEmailInWhitelist(email: string): boolean {
  const parts = email.toLowerCase().split('@')
  if (parts.length !== 2) return false
  return Object.prototype.hasOwnProperty.call(providers.value, parts[1])
}

export function getEmailProviders(): EmailProviders {
  return providers.value
}

/** 校验邮箱：格式 + 白名单。返回 i18n key 或 null(有效) */
export function validateEmail(email: string): { valid: boolean; errorKey: string } {
  if (!email || email.trim() === '') return { valid: false, errorKey: 'account.register.emailRequired' }
  if (!isValidEmailFormat(email)) return { valid: false, errorKey: 'error.emailInvalid' }
  if (!isEmailInWhitelist(email)) return { valid: false, errorKey: 'error.emailNotSupported' }
  return { valid: true, errorKey: '' }
}

/**
 * 从 /api/auth/email-whitelist 加载白名单。
 * 返回 true 表示加载成功；失败返回 false（由调用方提示，不阻塞页面）。
 */
export async function loadEmailWhitelist(): Promise<boolean> {
  // 已有缓存则直接复用（页面重进不重复请求；进程内单例）
  if (Object.keys(providers.value).length > 0) return true
  try {
    const res = await fetch('/api/auth/email-whitelist', { credentials: 'same-origin' })
    const data = await res.json()
    if (data?.success && data.data?.domains) {
      providers.value = data.data.domains
      // 会话级缓存：存内存即可，刷新页面会重新拉取
      return true
    }
    return false
  } catch {
    return false
  }
}