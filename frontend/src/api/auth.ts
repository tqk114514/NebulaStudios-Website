// 认证相关 API。字段与 Go 端 handlers/auth/*.go 对应。

import { get, post } from './client'

export interface Me {
  id: number
  uid: string
  username: string
  email: string
  avatar_url: string
  role: number
  microsoft_avatar_sync: boolean
  is_banned: boolean
  created_at: string
  // 第三方绑定信息（dashboard 头像管理 / 绑定解绑使用）
  microsoft_id?: number | null
  microsoft_name?: string | null
  microsoft_avatar_url?: string | null
  google_id?: number | null
  google_name?: string | null
  google_avatar_url?: string | null
  // 封禁信息
  ban_reason?: string | null
  banned_at?: string | null
  unban_at?: string | null
  // 两步验证
  totp_enabled?: boolean
}

export interface LoginRequest {
  email: string
  password: string
  captchaToken?: string
}

export function fetchMe() {
  return get<Me>('/api/auth/me')
}

export interface LoginTotpRequired {
  totp_required: true
  pending_token: string
}

export interface LoginTotpRequest {
  pendingToken: string
  code: string
}

export function login(body: LoginRequest) {
  return post<LoginTotpRequired | { message: string }>('/api/auth/login', body)
}

/** 登录二步验证：中转 token + TOTP 码（或恢复码） */
export function loginTotp(body: LoginTotpRequest) {
  return post<{ message: string }>('/api/auth/login/totp', body)
}

export function logout() {
  return post<{ message: string }>('/api/auth/logout')
}