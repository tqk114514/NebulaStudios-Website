/**
 * modules/admin/assets/js/stats.ts
 * 管理后台统计模块
 *
 * 功能：
 * - 加载统计数据
 * - 渲染统计卡片
 */

import { fetchApi, StatsResponse } from './common';

// ==================== DOM 元素 ====================

const statTotalUsers = document.getElementById('stat-total-users') as HTMLElement;
const statTodayUsers = document.getElementById('stat-today-users') as HTMLElement;
const statAdminCount = document.getElementById('stat-admin-count') as HTMLElement;
const statBannedCount = document.getElementById('stat-banned-count') as HTMLElement;

// ==================== API ====================

async function getStats(): Promise<StatsResponse | null> {
  const result = await fetchApi<StatsResponse>('/admin/api/stats');
  return result.success ? result.data! : null;
}

// ==================== 公开函数 ====================

export async function loadStats(): Promise<void> {
  const stats = await getStats();
  if (stats) {
    statTotalUsers.textContent = String(stats.totalUsers);
    statTodayUsers.textContent = String(stats.todayNewUsers);
    statAdminCount.textContent = String(stats.adminCount);
    statBannedCount.textContent = String(stats.bannedCount);
  }
}
