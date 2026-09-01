// 管理后台 API。字段与 Go 端 handlers/admin/*.go 对应。

import { get } from './client'

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
