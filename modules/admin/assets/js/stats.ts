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

const statTotalUsers = document.getElementById('stat-total-users');
const statTodayUsers = document.getElementById('stat-today-users');
const statAdminCount = document.getElementById('stat-admin-count');
const statBannedCount = document.getElementById('stat-banned-count');

// ==================== API ====================

async function getStats(): Promise<StatsResponse | null> {
  const result = await fetchApi<StatsResponse>('/admin/api/stats');
  return result.success ? result.data! : null;
}

// ==================== 公开函数 ====================

export async function loadStats(): Promise<void> {
  console.log('[ADMIN][STATS] loadStats called');
  
  const stats = await getStats();
  if (!stats) {
    console.warn('[ADMIN][STATS] Stats data is null');
    return;
  }
  
  console.log('[ADMIN][STATS] Stats data:', stats);
  
  if (statTotalUsers) {
    statTotalUsers.textContent = String(stats.totalUsers);
  } else {
    console.error('[ADMIN][STATS] statTotalUsers element not found');
  }
  
  if (statTodayUsers) {
    statTodayUsers.textContent = String(stats.todayNewUsers);
  } else {
    console.error('[ADMIN][STATS] statTodayUsers element not found');
  }
  
  if (statAdminCount) {
    statAdminCount.textContent = String(stats.adminCount);
  } else {
    console.error('[ADMIN][STATS] statAdminCount element not found');
  }
  
  if (statBannedCount) {
    statBannedCount.textContent = String(stats.bannedCount);
  } else {
    console.error('[ADMIN][STATS] statBannedCount element not found');
  }
}
