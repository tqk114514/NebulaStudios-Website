/**
 * modules/admin/assets/js/logs.ts
 * 管理后台操作日志模块
 *
 * 功能：
 * - 操作日志列表（分页）
 * - 日志详情展示
 */

import {
  fetchApi,
  AdminLog,
  LogListResponse,
  ACTION_NAMES,
  ROLE_NAMES,
  formatDate,
  escapeHtml,
  renderPagination
} from './common';

// ==================== 状态 ====================

let currentPage = 1;

// ==================== DOM 元素 ====================

const logsTableBody = document.getElementById('logs-table-body') as HTMLTableSectionElement;
const logsPagination = document.getElementById('logs-pagination') as HTMLElement;

// ==================== API ====================

async function getLogs(page: number): Promise<LogListResponse | null | 'forbidden'> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  const result = await fetchApi<LogListResponse>(`/admin/api/logs?${params}`);
  if (!result.success) {
    return result.errorCode === 'FORBIDDEN' ? 'forbidden' : null;
  }
  return result.data!;
}

// ==================== 日志列表 ====================

/**
 * 格式化日志详情
 */
function formatDetails(action: string, details?: Record<string, unknown>): string {
  if (!details) return '-';

  if (action === 'set_role') {
    const oldRole = ROLE_NAMES[details.old_role as number] || '未知';
    const newRole = ROLE_NAMES[details.new_role as number] || '未知';
    const username = escapeHtml(details.target_username as string || '');
    return `${username}: ${oldRole} → ${newRole}`;
  }

  if (action === 'delete_user') {
    const username = escapeHtml(details.target_username as string || '');
    const email = escapeHtml(details.target_email as string || '');
    return `${username} (${email})`;
  }

  if (action === 'ban_user') {
    const username = escapeHtml(details.target_username as string || '');
    const reason = escapeHtml(details.reason as string || '');
    return `${username}: ${reason}`;
  }

  if (action === 'unban_user') {
    const username = escapeHtml(details.target_username as string || '');
    return username;
  }

  return JSON.stringify(details);
}

/**
 * 渲染日志表格行 HTML
 */
function renderLogRow(log: AdminLog): string {
  const actionName = ACTION_NAMES[log.action] || log.action;
  const details = formatDetails(log.action, log.details);

  return `
    <tr>
      <td>${log.id}</td>
      <td>${escapeHtml(log.admin_username)}</td>
      <td><span class="action-badge ${log.action}">${actionName}</span></td>
      <td>${details}</td>
      <td>${formatDate(log.created_at)}</td>
    </tr>
  `;
}

/**
 * 加载日志列表
 */
export async function loadLogs(): Promise<void> {
  if (!logsTableBody) return;

  logsTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载中...</td></tr>';

  const data = await getLogs(currentPage);

  if (data === 'forbidden') {
    logsTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">无权限查看</td></tr>';
    logsPagination.innerHTML = '';
    return;
  }

  if (!data) {
    logsTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载失败</td></tr>';
    return;
  }

  if (data.logs.length === 0) {
    logsTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">暂无日志</td></tr>';
    logsPagination.innerHTML = '';
    return;
  }

  logsTableBody.innerHTML = data.logs.map(log => renderLogRow(log)).join('');

  if (logsPagination) {
    renderPagination({
      container: logsPagination,
      current: data.page,
      total: data.totalPages,
      onPageChange: (page) => {
        currentPage = page;
        loadLogs();
      }
    });
  }
}
