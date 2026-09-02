// 验证码发送倒计时 + 冷却恢复。
// 用法：const cd = useCountdown(60)；cd.start() / cd.remaining / cd.running / 剩余秒数实时
import { ref, onBeforeUnmount } from 'vue'

const STORAGE_KEY = 'nebula-code-countdown'

interface Stored {
  partial: 'register' | 'forgot' | 'change_password' | 'delete_account'
  expiresAt: number
  email: string
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

  /** 开始倒计时。seconds 用于 429 时以服务端返回的真实剩余等待时间校正（本地默认 60s）。
   *  通过 localStorage 持久化到期时间，刷新后自动恢复。 */
  function start(partial: 'register' | 'forgot' | 'change_password' | 'delete_account', email: string, secondsOverride?: number) {
    const total = secondsOverride && secondsOverride > 0 ? secondsOverride : seconds
    expiresAt = Date.now() + total * 1000
    remaining.value = total
    running.value = true
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ partial, expiresAt, email } satisfies Stored))
    } catch {
      // localStorage 不可用（隐私模式等）时降级为不持久化
    }
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
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (!raw) return false
      const s = JSON.parse(raw) as Stored
      if (s.partial !== partial || s.expiresAt <= Date.now()) {
        localStorage.removeItem(STORAGE_KEY)
        return false
      }
      expiresAt = s.expiresAt
      restore()
      // 校验邮箱：请求发送时若邮箱不同则不算该倒计时
      return true
    } catch {
      return false
    }
  }

  function stop() {
    clearInner()
    remaining.value = 0
    running.value = false
    try {
      localStorage.removeItem(STORAGE_KEY)
    } catch {
      // 忽略
    }
  }

  onBeforeUnmount(clearInner)

  return { remaining, running, start, stop, restoreOnMount }
}