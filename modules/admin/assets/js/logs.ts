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
  escapeHtml
} from './common';

// ==================== 状态 ====================

let currentPage = 1;

// ==================== DOM 元素 ====================

const logsTableBody = document.getElementById('logs-table-body') as HTMLTableSectionElement;
const logsPagination = document.getElementById('logs-pagination') as HTMLElement;

// ==================== API ====================

async function getLogs(page: number): Promise<LogListResponse | null> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  const result = await fetchApi<LogListResponse>(`/admin/api/logs?${params}`);
  return result.success ? result.data! : null;
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
  renderPagination(data.page, data.totalPages);
}

/**
 * 渲染分页控件
 */
function renderPagination(current: number, total: number): void {
  if (!logsPagination) return;

  if (total <= 1) {
    logsPagination.innerHTML = '';
    return;
  }

  let html = '';
  html += `<button ${current === 1 ? 'disabled' : ''} data-page="${current - 1}">上一页</button>`;

  const start = Math.max(1, current - 2);
  const end = Math.min(total, current + 2);

  if (start > 1) {
    html += `<button data-page="1">1</button>`;
    if (start > 2) html += `<button disabled>...</button>`;
  }

  for (let i = start; i <= end; i++) {
    html += `<button ${i === current ? 'class="active"' : ''} data-page="${i}">${i}</button>`;
  }

  if (end < total) {
    if (end < total - 1) html += `<button disabled>...</button>`;
    html += `<button data-page="${total}">${total}</button>`;
  }

  html += `<button ${current === total ? 'disabled' : ''} data-page="${current + 1}">下一页</button>`;

  logsPagination.innerHTML = html;

  logsPagination.querySelectorAll('button[data-page]').forEach(btn => {
    btn.addEventListener('click', () => {
      const page = Number((btn as HTMLElement).dataset.page);
      if (page && page !== currentPage) {
        currentPage = page;
        loadLogs();
      }
    });
  });
}
