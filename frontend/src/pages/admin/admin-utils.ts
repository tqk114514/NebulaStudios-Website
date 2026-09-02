// 管理后台子页面共享工具（迁移自旧版 common.ts 的格式化函数，行为不变）。

export function formatDate(dateStr?: string | null): string {
  if (!dateStr) return '-'
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function formatRelativeTime(timestamp: number): string {
  const seconds = Math.floor((Date.now() - timestamp) / 1000)
  if (seconds < 5) return '刚刚'
  if (seconds < 60) return `${seconds}秒前`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}分钟前`
  return '超过1小时前'
}

/** 封禁是否仍生效（考虑解封时间） */
export function isUserBanned(user: { is_banned?: boolean; unban_at?: string }): boolean {
  if (!user.is_banned) return false
  if (user.unban_at && new Date(user.unban_at) < new Date()) return false
  return true
}
