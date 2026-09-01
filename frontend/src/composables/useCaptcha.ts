// Turnstile 人机验证 composable（迁移自 lib/captcha.ts，保留原有契约）。
// 关键逻辑：
// - loadCaptchaConfig() 从 /api/config/captcha 读取 siteKey；有 key 才加载 SDK
// - initCaptcha(el) 在容器内渲染 widget；回调写入 token
// - 返回 token 与"是否启用"标记，供表单提交使用

import { ref } from 'vue'

const SDK_URL = import.meta.env.VITE_TURNSTILE_SDK_URL || 'https://challenges.cloudflare.com/turnstile/v0/api.js'

// ---- 类型（对齐全局 window.turnstile）----
interface TurnstileAPI {
  render: (target: HTMLElement | string, options: TurnstileOptions) => string
  remove: (widgetId: string) => void
}
interface TurnstileOptions {
  sitekey: string
  theme?: string
  size?: string
  callback?: (token: string) => void
  'error-callback'?: () => void
  'expired-callback'?: () => void
}
declare global {
  interface Window {
    turnstile?: TurnstileAPI
  }
}

// ---- 全局单例状态 ----
export const captchaEnabled = ref(false) // 系统是否启用（来自后端配置）
const siteKey = ref('')
const token = ref('')

let sdkLoadPromise: Promise<void> | null = null

/** 加载开关配置并（若有 key）后台预载 SDK。 */
export async function loadCaptchaConfig(): Promise<boolean> {
  try {
    const res = await fetch('/api/config/captcha', { credentials: 'same-origin' })
    const data = await res.json()
    siteKey.value = (data?.data?.siteKey as string) || ''
    captchaEnabled.value = !!siteKey.value
    if (captchaEnabled.value) {
      loadSDK().catch(() => {
        // 后台预载失败不致命：首次渲染时重试
        sdkLoadPromise = null
      })
    }
    return true
  } catch {
    captchaEnabled.value = false
    siteKey.value = ''
    return false
  }
}

function loadSDK(): Promise<void> {
  if (!sdkLoadPromise) {
    sdkLoadPromise = injectSDKScript().catch((err: Error) => {
      sdkLoadPromise = null
      throw err
    })
  }
  return sdkLoadPromise
}

function injectSDKScript(): Promise<void> {
  return new Promise((resolve, reject) => {
    document.querySelectorAll<HTMLScriptElement>(`script[src^="${SDK_URL.split('?')[0]}"]`).forEach((s) => s.remove())
    const script = document.createElement('script')
    script.src = SDK_URL
    script.async = true
    script.defer = true
    script.onload = () => resolve()
    script.onerror = () => {
      script.remove()
      reject(new Error('Failed to load captcha SDK'))
    }
    document.head.appendChild(script)
  })
}

function waitForAPI(timeout = 8000): Promise<boolean> {
  return new Promise((resolve) => {
    if (window.turnstile) return resolve(true)
    const start = Date.now()
    const timer = setInterval(() => {
      if (window.turnstile) {
        clearInterval(timer)
        resolve(true)
      } else if (Date.now() - start > timeout) {
        clearInterval(timer)
        resolve(false)
      }
    }, 100)
  })
}

/** 在当前容器渲染 Turnstile widget。回调写入全局 token。 */
export async function initCaptcha(container: HTMLElement): Promise<void> {
  if (!captchaEnabled.value) return
  // 确保 SDK 就绪（预载失败时重试）
  if (!window.turnstile) {
    try {
      await loadSDK()
      await waitForAPI()
    } catch {
      return
    }
  }
  if (!window.turnstile) return

  container.innerHTML = ''
  const instanceToken = ref('')
  // 直接传 DOM 元素渲染（旧前端依赖容器 id 选择器；组件化后容器无固定 id）
  window.turnstile.render(container, {
    sitekey: siteKey.value,
    theme: 'dark',
    size: 'normal',
    callback: (t: string) => {
      token.value = t
      instanceToken.value = t
    },
    'error-callback': () => {
      token.value = ''
    },
    'expired-callback': () => {
      token.value = ''
    },
  })
}

/** 已启用时返回 token，未启用返回空（与"关闭时直接提交"一致）。 */
export function getCaptchaToken(): string {
  return token.value
}

/** 提交后清空 token，下次需重新验证。 */
export function resetCaptchaToken(): void {
  token.value = ''
}

export function isCaptchaEnabled(): boolean {
  return captchaEnabled.value
}