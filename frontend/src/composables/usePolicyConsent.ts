// 政策同意逻辑（登录成功/OAuth 授权页/dashboard 共用）。
// 有未同意的已生效政策 → 弹出强制同意弹窗（由 PolicyConsentModal 组件渲染）；
// 同意 → 写记录并 resolve(true)；拒绝 → 登出并 resolve(false)。
// in-flight 去重：多入口并发调用共享同一 promise。

import { ref, type Ref } from 'vue'
import { fetchPendingConsent, recordConsent, logoutRaw, type PendingConsentPolicy } from '@/api/policy'

// ---- 单例状态：由 PolicyConsentModal 组件读取/驱动，usePolicyConsent 提供命令式入口 ----
export const consentState = {
  visible: ref(false),
  policies: ref<PendingConsentPolicy[]>([]),
  loading: ref(false),
  error: ref(''),
  // 当前待解决的 promise resolver
  resolver: null as null | ((result: boolean) => void),
}

let inFlight: Promise<boolean> | null = null

export function usePolicyConsent() {
  async function check(): Promise<boolean> {
    if (inFlight) return inFlight
    inFlight = (async () => {
      const list = await fetchPending()
      if (list === null) return true // 未登录 / 服务错误：不阻断
      if (list.length === 0) return true
      return showConsent(list)
    })().finally(() => {
      inFlight = null
    })
    return inFlight
  }

  async function aback(records: { policy_type: string; policy_version: string }[]) {
    consentState.loading.value = true
    consentState.error.value = ''
    try {
      await recordConsent(records)
      consentState.visible.value = false
      consentState.resolver?.(true)
      consentState.resolver = null
      return true
    } catch {
      consentState.error.value = 'consentFailed'
      return false
    } finally {
      consentState.loading.value = false
    }
  }

  async function decline() {
    try {
      await logoutRaw()
    } catch {
      // 忽略登出错误，强制跳转登录页
    }
    consentState.visible.value = false
    consentState.resolver?.(false)
    consentState.resolver = null
    return false
  }

  return { state: consentState, check, accept: aback, decline }
}

async function fetchPending() {
  try {
    const res = await fetch('/api/policy/pending-consent', { credentials: 'same-origin' })
    if (res.status === 401) return null
    if (!res.ok) return null
    const data = await res.json()
    if (!data.success || !data.data || !Array.isArray(data.data.policies)) return null
    return data.data.policies as PendingConsentPolicy[]
  } catch {
    return null
  }
}

function showConsent(list: PendingConsentPolicy[]): Promise<boolean> {
  consentState.policies.value = list
  consentState.visible.value = true
  consentState.error.value = ''
  return new Promise((resolve) => {
    consentState.resolver = resolve
  })
}

// 供组件渲染用（避免每次从 hook 解构的引用不稳定）
export const consentRefs: { list: Ref<PendingConsentPolicy[]>; visible: Ref<boolean>; loading: Ref<boolean>; error: Ref<string> } = {
  list: consentState.policies,
  visible: consentState.visible,
  loading: consentState.loading,
  error: consentState.error,
}