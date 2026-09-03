// 个人中心 Dashboard 相关 API（迁移自 modules/account/assets/js/dashboard.ts）。

import { get, post, patch, del } from './client'

/** 用户操作日志项 */
export interface UserLogItem {
  id: number
  action: string
  details?: {
    old_username?: string
    new_username?: string
    microsoft_name?: string
    google_name?: string
  }
  created_at: string
}

export interface UserLogsResult {
  logs: UserLogItem[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface OAuthGrant {
  client_id: string
  client_name: string
  client_description?: string
  scope: string
  created_at: string
  updated_at: string
}

export interface AvatarResult {
  avatar_url: string
}

export interface ChangePasswordRequest {
  currentPassword: string
  newPassword: string
  captchaToken?: string
}

export interface SendDeleteCodeRequest {
  captchaToken?: string
  language?: string
}

export interface DeleteAccountRequest {
  code: string
  password: string
}

export interface UpdateUsernameRequest {
  username: string
  captchaToken?: string
}

/** 更新头像（值可为 http(s) 图片 URL / data: base64 / "microsoft" / "google" / 空串表示移除） */
export function updateAvatar(avatarUrl: string) {
  return patch<AvatarResult>('/api/user/avatar', { avatar_url: avatarUrl })
}

/** 修改用户名（后端校验验证码，captchaToken 必传——人机验证启用时） */
export function updateUsername(body: UpdateUsernameRequest) {
  return patch<{ username: string }>('/api/user/username', body)
}

export function unlinkMicrosoft() {
  return post<{ message: string }>('/api/auth/microsoft/unlink')
}

export function unlinkGoogle() {
  return post<{ message: string }>('/api/auth/google/unlink')
}

export function changePassword(body: ChangePasswordRequest) {
  return post<{ message: string }>('/api/auth/change-password', body)
}

export function sendDeleteCode(body: SendDeleteCodeRequest) {
  return post<{ message: string }>('/api/auth/send-delete-code', body)
}

export function deleteAccount(body: DeleteAccountRequest) {
  return post<{ message: string }>('/api/auth/delete-account', body)
}

export function requestDataExport() {
  return post<{ token: string }>('/api/user/export/request')
}

export function fetchUserLogs(page: number, pageSize: number) {
  return get<UserLogsResult>(`/api/user/logs?page=${page}&pageSize=${pageSize}`)
}

export function fetchOAuthGrants() {
  return get<{ grants: OAuthGrant[] }>('/api/user/oauth/grants')
}

export function revokeOAuthGrant(clientId: string) {
  return del<{ message: string }>(`/api/user/oauth/grants/${encodeURIComponent(clientId)}`)
}