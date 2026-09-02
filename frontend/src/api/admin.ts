// 管理后台 API。字段与 Go 端 handlers/admin/*.go 及 models 对应。

import { get, post, put, patch, del } from './client'

/** GET /admin/api/stats 响应（system.go AdminStats） */
export interface AdminStats {
  totalUsers: number
  todayNewUsers: number
  adminCount: number
  bannedCount: number
}

export function fetchAdminStats() {
  return get<AdminStats>('/admin/api/stats')
}

/** GET /admin/api/users/:uid 与列表项（models.UserPublic） */
export interface AdminUser {
  id: number
  uid: string
  username: string
  email: string
  avatar_url: string
  role: number
  microsoft_name?: string
  is_banned?: boolean
  ban_reason?: string
  banned_at?: string
  unban_at?: string
  created_at?: string
}

export interface UserListResponse {
  users: AdminUser[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

/** GET /admin/api/logs 列表项（models.AdminLogPublic，details 为服务端透传 JSON） */
export interface AdminLogEntry {
  id: number
  admin_uid: string
  admin_username: string
  action: string
  target_uid?: string
  details?: Record<string, unknown>
  created_at: string
}

export interface LogListResponse {
  logs: AdminLogEntry[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface OAuthClient {
  id: number
  client_id: string
  name: string
  description: string
  redirect_uri: string
  is_enabled: boolean
  created_at: string
  updated_at: string
}

export interface OAuthClientListResponse {
  clients: OAuthClient[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface CreateClientResponse {
  client: OAuthClient
  client_secret: string
}

export interface EmailWhitelistEntry {
  id: number
  domain: string
  signup_url: string
  logo_url: string
  is_enabled: boolean
  created_at: string
  updated_at: string
}

export interface WhitelistListResponse {
  whitelist: EmailWhitelistEntry[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

/** POST /admin/api/data/export/request 响应（data.go RequestExport） */
export interface ExportRequestResponse {
  requestId: string
  /** OTAC 过期时间（Unix 秒） */
  expiresAt: number
}

/** POST /admin/api/data/import/preview 响应 */
export interface ImportPreviewResponse {
  fileToken: string
  usersCount: number
  logsCount: number
  exportedAt: string
}

/** POST /admin/api/data/import/execute 响应 */
export interface ImportExecuteResponse {
  usersImported: number
  logsImported: number
  usersFailed: number
  logsFailed: number
  usersPasswordSkipped: number
  usersRoleDowngraded: number
}

function pageParams(page: number, search?: string): string {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' })
  if (search) params.set('search', search)
  return params.toString()
}

// ---- 用户管理 ----

export function fetchAdminUsers(page: number, search = '') {
  return get<UserListResponse>(`/admin/api/users?${pageParams(page, search)}`)
}

export function fetchAdminUser(uid: string) {
  return get<AdminUser>(`/admin/api/users/${encodeURIComponent(uid)}`)
}

export function setAdminUserRole(uid: string, role: number) {
  return put(`/admin/api/users/${encodeURIComponent(uid)}/role`, { role })
}

export function deleteAdminUser(uid: string) {
  return del(`/admin/api/users/${encodeURIComponent(uid)}`)
}

export function banAdminUser(uid: string, reason: string, days: number) {
  return patch(`/admin/api/users/${encodeURIComponent(uid)}/ban`, { reason, days })
}

export function unbanAdminUser(uid: string) {
  return patch(`/admin/api/users/${encodeURIComponent(uid)}/unban`)
}

// ---- 操作日志 ----

export function fetchAdminLogs(page: number) {
  return get<LogListResponse>(`/admin/api/logs?${pageParams(page)}`)
}

// ---- OAuth 应用 ----

export function fetchOAuthClients(page: number, search = '') {
  return get<OAuthClientListResponse>(`/admin/api/oauth/clients?${pageParams(page, search)}`)
}

export function fetchOAuthClient(id: number) {
  return get<OAuthClient>(`/admin/api/oauth/clients/${id}`)
}

export function createOAuthClient(name: string, description: string, redirectUri: string) {
  return post<CreateClientResponse>('/admin/api/oauth/clients', {
    name,
    description,
    redirect_uri: redirectUri,
  })
}

export function updateOAuthClient(id: number, name: string, description: string, redirectUri: string) {
  return put(`/admin/api/oauth/clients/${id}`, { name, description, redirect_uri: redirectUri })
}

export function toggleOAuthClient(id: number, enabled: boolean) {
  return patch(`/admin/api/oauth/clients/${id}`, { enabled })
}

export function deleteOAuthClient(id: number) {
  return del(`/admin/api/oauth/clients/${id}`)
}

export function regenerateOAuthClientSecret(id: number) {
  return post<{ client_secret: string }>(`/admin/api/oauth/clients/${id}/secret`)
}

// ---- 邮箱白名单 ----

export function fetchWhitelist(page: number) {
  return get<WhitelistListResponse>(`/admin/api/email-whitelist?${pageParams(page)}`)
}

export function fetchWhitelistEntry(id: number) {
  return get<{ item: EmailWhitelistEntry }>(`/admin/api/email-whitelist/${id}`)
}

export function createWhitelistEntry(domain: string, signupUrl: string, logoUrl: string) {
  return post<{ item: EmailWhitelistEntry }>('/admin/api/email-whitelist', {
    domain,
    signup_url: signupUrl,
    logo_url: logoUrl,
  })
}

export function updateWhitelistEntry(
  id: number,
  payload: { domain: string; signup_url: string; logo_url: string; is_enabled: boolean },
) {
  // 后端"无变更"分支返回 success 且不带 item，同样视为成功
  return put(`/admin/api/email-whitelist/${id}`, payload)
}

export function deleteWhitelistEntry(id: number) {
  return del(`/admin/api/email-whitelist/${id}`)
}
