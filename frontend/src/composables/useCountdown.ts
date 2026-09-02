// 验证码发送倒计时 + 冷却恢复。
// 用法：const cd = useCountdown(60)；cd.start() / cd.remaining / cd.running / 剩余秒数实时
// 持久化按 Cookie 政策用 cookie countdown_end（动态有效期，与剩余倒计时一致，到期自动消失）
import { ref, onBeforeUnmount } from 'vue'

const COOKIE_NAME = 'countdown_end'

interface Stored {
  partial: 'register' | 'forgot' | 'change_password' | 'delete_account'
  expiresAt: number
  email: string
}

function setCountdownCookie(value: Stored): void {
  const maxAge = Math.ceil((value.expiresAt - Date.now()) / 1000)
  if (maxAge <= 0) return
  try {
    document.cookie = `${COOKIE_NAME}=${encodeURIComponent(JSON.stringify(value))}; path=/; max-age=${maxAge}; samesite=lax`
  } catch {
    // cookie 不可用时降级为不持久化
  }
}

function readCountdownCookie(): Stored | null {
  try {
    const entry = document.cookie
      .split('; ')
      .find((s) => s.startsWith(COOKIE_NAME + '='))
    if (!entry) return null
    return JSON.parse(decodeURIComponent(entry.slice(COOKIE_NAME.length + 1))) as Stored
  } catch {
    return null
  }
}

function clearCountdownCookie(): void {
  try {
    document.cookie = `${COOKIE_NAME}=; path=/; max-age=0; samesite=lax`
  } catch {
    // 忽略
  }
}

export function useCountdown(seconds = 60) {
  const remaining = ref(0)
  const running = ref(false)
  let timer: ReturnType<typeof setInterval> | null = null
  let expiresAt = 0

  function clearInner() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  /** 从当前剩余时间恢复 */
  function restore() {
    if (expiresAt > Date.now()) {
      remaining.value = Math.ceil((expiresAt - Date.now()) / 1000)
      running.value = true
      clearInner()
      timer = setInterval(() => {
        remaining.value--
        if (remaining.value <= 0) {
          remaining.value = 0
          running.value = false
          clearInner()
        }
      }, 1000)
    }
  }

  /** 开始倒计时。secondsOverride 用于 429 时以服务端返回的限制结束时间校正（本地默认 60s）。
   *  通过 cookie 持久化到期时间（动态有效期），刷新后自动恢复。 */
  function start(partial: 'register' | 'forgot' | 'change_password' | 'delete_account', email: string, secondsOverride?: number) {
    const total = secondsOverride && secondsOverride > 0 ? secondsOverride : seconds
    expiresAt = Date.now() + total * 1000
    remaining.value = total
    running.value = true
    setCountdownCookie({ partial, expiresAt, email })
    clearInner()
    timer = setInterval(() => {
      remaining.value--
      if (remaining.value <= 0) {
        remaining.value = 0
        running.value = false
        clearInner()
      }
    }, 1000)
  }

  /** 组件挂载时尝试恢复之前的倒计时（校验 partial 与 email 匹配） */
  function restoreOnMount(partial: Stored['partial'], email: string): boolean {
    const s = readCountdownCookie()
    if (!s || s.partial !== partial || s.expiresAt <= Date.now()) {
      return false
    }
    expiresAt = s.expiresAt
    restore()
    // 校验邮箱：请求发送时若邮箱不同则不算该倒计时
    return true
  }

  function stop() {
    clearInner()
    remaining.value = 0
    running.value = false
    clearCountdownCookie()
  }

  onBeforeUnmount(clearInner)

  return { remaining, running, start, stop, restoreOnMount }
}
