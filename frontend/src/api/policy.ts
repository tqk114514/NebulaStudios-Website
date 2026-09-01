// 政策 API。

import { post } from './client'

export interface PendingConsentPolicy {
  policy_type: string
  version: string
  effective_date: string
}

export interface PendingConsent {
  policies: PendingConsentPolicy[]
}

/** 登录态：获取待同意的政策。401 表示未登录（调用方应视为无需处理） */
export async function fetchPendingConsent(): Promise<PendingConsent | null> {
  const res = await fetch('/api/policy/pending-consent', { credentials: 'same-origin' })
  if (res.status === 401) return null
  if (!res.ok) return null // 网络/服务错误不阻断流程
  const data = await res.json()
  if (!data.success || !data.data || !Array.isArray(data.data.policies)) return null
  return data.data as PendingConsent
}

export interface ConsentRecord {
  policy_type: string
  policy_version: string
}

/** 记录政策同意 */
export async function recordConsent(policies: ConsentRecord[]): Promise<void> {
  await post<{ message: string }>('/api/policy/consent', { policies })
}

export function logoutRaw() {
  return fetch('/api/auth/logout', { method: 'POST', credentials: 'same-origin' })
}