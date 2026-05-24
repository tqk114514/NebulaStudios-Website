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
  renderList
} from './common';

// ==================== 状态 ====================

let currentPage = 1;

// ==================== DOM 元素 ====================

const logsTableBody = document.getElementById('logs-table-body') as HTMLTableSectionElement | null;
const logsPagination = document.getElementById('logs-pagination') as HTMLElement | null;

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

  if (action.startsWith('oauth_client_')) {
    const clientName = escapeHtml(details.client_name as string || '');
    const clientId = escapeHtml(details.client_id as string || '');
    return `${clientName} (${clientId})`;
  }

  if (action.startsWith('email_whitelist_')) {
    const domain = escapeHtml(details.domain as string || '');
    return domain || JSON.stringify(details);
  }

  if (action === 'data_export') {
    const users = details.users_count || details.usersCount || 0;
    const logs = details.logs_count || details.logsCount || 0;
    return `用户 ${users} 条, 日志 ${logs} 条`;
  }

  if (action === 'data_import') {
    const users = details.users_imported || details.usersImported || 0;
    const logs = details.logs_imported || details.logsImported || 0;
    return `用户 ${users} 条, 日志 ${logs} 条`;
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
      <td>${escapeHtml(log.admin_username)}</td>
      <td>${actionName}</td>
      <td>${details}</td>
      <td>${formatDate(log.created_at)}</td>
    </tr>
  `;
}

/**
 * 加载日志列表
 */
export async function loadLogs(): Promise<void> {
  console.log('[ADMIN][LOGS] loadLogs called');

  if (!logsTableBody) {
    console.error('[ADMIN][LOGS] logsTableBody element not found');
    return;
  }

  const data = await getLogs(currentPage);

  if (data === 'forbidden') {
    logsTableBody.innerHTML = '<tr><td colspan="4" class="loading-cell">无权限查看</td></tr>';
    if (logsPagination) logsPagination.innerHTML = '';
    return;
  }

  await renderList({
    tableBody: logsTableBody,
    pagination: logsPagination,
    fetchData: async () => {
      if (!data) return null;
      return { items: data.logs, total: data.total, page: data.page, totalPages: data.totalPages };
    },
    renderRow: renderLogRow,
    colspan: 4,
    emptyMessage: '暂无日志',
    onPageChange: (page) => {
      currentPage = page;
      loadLogs();
    }
  });
}
