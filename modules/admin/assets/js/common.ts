/**
 * modules/admin/assets/js/common.ts
 * 管理后台公共模块
 *
 * 包含：
 * - 类型定义
 * - 常量
 * - API 请求函数
 * - UI 工具函数（Toast、Modal、格式化）
 */

// ==================== 类型定义 ====================

export interface UserPublic {
  id: number;
  uid: string;
  username: string;
  email: string;
  avatar_url: string;
  role: number;
  microsoft_id?: string;
  microsoft_name?: string;
  microsoft_avatar_url?: string;
  is_banned?: boolean;
  ban_reason?: string;
  banned_at?: string;
  unban_at?: string;
  created_at?: string;
}

export interface StatsResponse {
  totalUsers: number;
  todayNewUsers: number;
  adminCount: number;
  bannedCount: number;
}

export interface UserListResponse {
  users: UserPublic[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

/** 操作日志 */
export interface AdminLog {
  id: number;
  admin_id: number;
  admin_username: string;
  action: string;
  target_id?: number;
  details?: Record<string, unknown>;
  created_at: string;
}

/** 日志列表响应 */
export interface LogListResponse {
  logs: AdminLog[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

/** API 成功响应 */
export interface ApiSuccessResponse<T> {
  success: true;
  data: T;
}

/** API 失败响应 */
export interface ApiErrorResponse {
  success: false;
  errorCode: string;
}

/** API 响应联合类型 */
export type ApiResponse<T> = ApiSuccessResponse<T> | ApiErrorResponse;

// ==================== 常量 ====================

export const ROLE_NAMES: Record<number, string> = {
  0: '普通用户',
  1: '管理员',
  2: '超级管理员'
};

export const ROLE_CLASSES: Record<number, string> = {
  0: 'user',
  1: 'admin',
  2: 'super-admin'
};

export const ACTION_NAMES: Record<string, string> = {
  'set_role': '修改角色',
  'delete_user': '删除用户',
  'ban_user': '封禁用户',
  'unban_user': '解封用户'
};

// ==================== DOM 元素 ====================

export const toastContainer = document.getElementById('toast-container') as HTMLElement | null;
export const userModal = document.getElementById('user-modal') as HTMLElement | null;
export const userModalBody = document.getElementById('user-modal-body') as HTMLElement | null;
export const userModalFooter = document.getElementById('user-modal-footer') as HTMLElement | null;
export const whitelistModal = document.getElementById('whitelist-modal') as HTMLElement | null;
export const whitelistModalBody = document.getElementById('whitelist-modal-body') as HTMLElement | null;
export const whitelistModalFooter = document.getElementById('whitelist-modal-footer') as HTMLElement | null;
export const whitelistModalClose = document.getElementById('whitelist-modal-close') as HTMLButtonElement | null;
export const confirmModal = document.getElementById('confirm-modal') as HTMLElement | null;
export const confirmTitle = document.getElementById('confirm-title') as HTMLElement | null;
export const confirmMessage = document.getElementById('confirm-message') as HTMLElement | null;
export const confirmCancel = document.getElementById('confirm-cancel') as HTMLButtonElement | null;
export const confirmOk = document.getElementById('confirm-ok') as HTMLButtonElement | null;

let currentConfirmHandler: (() => void) | null = null;
export const banModal = document.getElementById('ban-modal') as HTMLElement | null;
export const banReason = document.getElementById('ban-reason') as HTMLSelectElement | null;
export const banCustomReasonGroup = document.getElementById('ban-custom-reason-group') as HTMLElement | null;
export const banCustomReason = document.getElementById('ban-custom-reason') as HTMLInputElement | null;
export const banDuration = document.getElementById('ban-duration') as HTMLSelectElement | null;
export const banCancel = document.getElementById('ban-cancel') as HTMLButtonElement | null;
export const banConfirm = document.getElementById('ban-confirm') as HTMLButtonElement | null;
export const banModalClose = document.getElementById('ban-modal-close') as HTMLButtonElement | null;

// ==================== API 函数 ====================

export async function fetchApi<T>(url: string, options?: RequestInit): Promise<ApiResponse<T>> {
  try {
    const response = await fetch(url, {
      ...options,
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers
      }
    });

    if (response.status === 401) {
      window.location.href = '/account/login';
      return { success: false, errorCode: 'UNAUTHORIZED' };
    }

    if (response.status === 403) {
      return { success: false, errorCode: 'FORBIDDEN' };
    }

    const contentType = response.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
      console.error('[ADMIN] Server returned non-JSON response:', response.status, contentType);
      return { success: false, errorCode: 'SERVER_ERROR' };
    }

    const data = await response.json();
    return data;
  } catch (error) {
    if (error instanceof TypeError && error.message.includes('fetch')) {
      return { success: false, errorCode: 'NETWORK_ERROR' };
    }
    console.error('[ADMIN] API Error:', error);
    return { success: false, errorCode: 'SERVER_ERROR' };
  }
}

export async function getCurrentUser(): Promise<UserPublic | null> {
  const result = await fetchApi<UserPublic>('/api/auth/me');
  return result.success ? result.data! : null;
}

export async function logout(): Promise<void> {
  await fetchApi('/api/auth/logout', { method: 'POST' });
  window.location.href = '/account/login';
}

// ==================== UI 函数 ====================

export function showToast(message: string, type: 'success' | 'error' | 'warning' = 'success'): void {
  console.log('[ADMIN][COMMON] showToast called:', { message, type });
  
  if (!toastContainer) {
    console.error('[ADMIN][COMMON] toastContainer not found');
    return;
  }
  
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  toastContainer.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

export function showModal(modal: HTMLElement | null): void {
  console.log('[ADMIN][COMMON] showModal called');
  
  if (!modal) {
    console.error('[ADMIN][COMMON] showModal: modal is null');
    return;
  }
  modal.classList.remove('is-hidden');
}

export function hideModal(modal: HTMLElement | null): void {
  console.log('[ADMIN][COMMON] hideModal called');
  
  if (!modal) {
    console.error('[ADMIN][COMMON] hideModal: modal is null');
    return;
  }
  modal.classList.add('is-hidden');
}

export function showConfirm(title: string, message: string, onConfirm: () => void): void {
  console.log('[ADMIN][COMMON] showConfirm called');
  
  const localConfirmTitle = confirmTitle;
  const localConfirmMessage = confirmMessage;
  const localConfirmModal = confirmModal;
  const localConfirmOk = confirmOk;
  
  if (!localConfirmTitle || !localConfirmMessage || !localConfirmModal || !localConfirmOk) {
    console.error('[ADMIN][COMMON] showConfirm: required elements not found');
    return;
  }
  
  localConfirmTitle.textContent = title;
  localConfirmMessage.textContent = message;
  showModal(localConfirmModal);

  if (currentConfirmHandler) {
    localConfirmOk.removeEventListener('click', currentConfirmHandler);
  }

  const handleConfirm = (): void => {
    hideModal(localConfirmModal);
    currentConfirmHandler = null;
    onConfirm();
  };

  currentConfirmHandler = handleConfirm;
  localConfirmOk.addEventListener('click', handleConfirm);
}

// ==================== 格式化函数 ====================

export function formatDate(dateStr?: string): string {
  if (!dateStr) return '-';
  const date = new Date(dateStr);
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  });
}

export function formatRelativeTime(timestamp: number): string {
  const seconds = Math.floor((Date.now() - timestamp) / 1000);
  if (seconds < 5) return '刚刚';
  if (seconds < 60) return `${seconds}秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}分钟前`;
  return '超过1小时前';
}

export function escapeHtml(str: string): string {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

// ==================== 分页控件 ====================

export interface PaginationConfig {
  container: HTMLElement;
  current: number;
  total: number;
  onPageChange: (page: number) => void;
}

export function renderPagination(config: PaginationConfig): void {
  const { container, current, total, onPageChange } = config;

  if (total <= 1) {
    container.innerHTML = '';
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

  container.innerHTML = html;

  container.querySelectorAll('button[data-page]').forEach(btn => {
    btn.addEventListener('click', () => {
      const page = Number((btn as HTMLElement).dataset.page);
      if (page && page !== current) {
        onPageChange(page);
      }
    });
  });
}

// ==================== 通用缓存管理器 ====================

export interface CachedItem<T> {
  data: T;
  cachedAt: number;
}

export class DataCache<T> {
  private cache: Map<string | number, CachedItem<T>>;
  private maxSize: number;

  constructor(maxSize: number = 100) {
    this.cache = new Map();
    this.maxSize = maxSize;
  }

  get(key: string | number): CachedItem<T> | undefined {
    return this.cache.get(key);
  }

  set(key: string | number, data: T): void {
    if (this.cache.size >= this.maxSize && !this.cache.has(key)) {
      const oldestKey = this.cache.keys().next().value;
      if (oldestKey !== undefined) {
        this.cache.delete(oldestKey);
      }
    }
    this.cache.set(key, { data, cachedAt: Date.now() });
  }

  delete(key: string | number): boolean {
    return this.cache.delete(key);
  }

  has(key: string | number): boolean {
    return this.cache.has(key);
  }

  clear(): void {
    this.cache.clear();
  }

  get size(): number {
    return this.cache.size;
  }
}

// ==================== 表格行操作 ====================

export interface TableRowUpdateConfig<T> {
  tableBody: HTMLTableSectionElement;
  rowId: string | number;
  rowIdAttr: string;
  fetchData: () => Promise<T | null>;
  renderRow: (data: T) => string;
  bindEvents: (row: HTMLTableRowElement) => void;
  cache?: DataCache<T>;
  cacheKey?: string | number;
}

export async function updateTableRow<T>(config: TableRowUpdateConfig<T>): Promise<void> {
  const { tableBody, rowId, rowIdAttr, fetchData, renderRow, bindEvents, cache, cacheKey } = config;

  const oldRow = tableBody.querySelector(`tr[${rowIdAttr}="${rowId}"]`) as HTMLTableRowElement;
  if (!oldRow) return;

  oldRow.classList.add('is-updating');

  const data = await fetchData();
  if (!data) {
    oldRow.classList.remove('is-updating');
    return;
  }

  if (cache && cacheKey !== undefined) {
    cache.set(cacheKey, data);
  }

  const temp = document.createElement('tbody');
  temp.innerHTML = renderRow(data);
  const newRow = temp.firstElementChild as HTMLTableRowElement;

  oldRow.replaceWith(newRow);
  bindEvents(newRow);
}

export interface TableRowRemoveConfig {
  tableBody: HTMLTableSectionElement;
  rowId: string | number;
  rowIdAttr: string;
  cache?: DataCache<unknown>;
  cacheKey?: string | number;
  colspan: number;
  emptyMessage?: string;
}

export function removeTableRow(config: TableRowRemoveConfig): void {
  const { tableBody, rowId, rowIdAttr, cache, cacheKey, colspan, emptyMessage = '暂无数据' } = config;

  const row = tableBody.querySelector(`tr[${rowIdAttr}="${rowId}"]`) as HTMLTableRowElement;
  if (!row) return;

  if (cache && cacheKey !== undefined) {
    cache.delete(cacheKey);
  }

  row.classList.add('is-deleting');

  setTimeout(() => {
    row.style.transition = 'opacity 0.2s, transform 0.2s';
    row.style.opacity = '0';
    row.style.transform = 'translateX(-20px)';

    setTimeout(() => {
      row.remove();
      if (tableBody.children.length === 0) {
        tableBody.innerHTML = `<tr><td colspan="${colspan}" class="loading-cell">${emptyMessage}</td></tr>`;
      }
    }, 200);
  }, 600);
}

// ==================== 搜索初始化 ====================

export function initSearch(
  searchInput: HTMLInputElement,
  searchBtn: HTMLButtonElement,
  onSearch: (query: string) => void
): void {
  searchBtn.addEventListener('click', () => {
    onSearch(searchInput.value.trim());
  });

  searchInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
      onSearch(searchInput.value.trim());
    }
  });
}

// ==================== 剪贴板操作 ====================

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    const success = document.execCommand('copy');
    document.body.removeChild(textarea);
    return success;
  }
}

// ==================== 状态徽章 ====================

export function renderStatusBadge(
  enabled: boolean,
  enabledText: string = '已启用',
  disabledText: string = '已禁用'
): string {
  const statusClass = enabled ? 'enabled' : 'disabled';
  const statusText = enabled ? enabledText : disabledText;
  return `<span class="status-badge ${statusClass}">${statusText}</span>`;
}

export function renderRoleBadge(role: number): string {
  return `<span class="role-badge ${ROLE_CLASSES[role]}">${ROLE_NAMES[role]}</span>`;
}

// ==================== 详情弹窗缓存渲染 ====================

export interface DetailModalConfig<T> {
  modal: HTMLElement | null;
  modalBody: HTMLElement | null;
  modalFooter: HTMLElement | null;
  cache: DataCache<T>;
  cacheKey: string | number;
  fetchData: () => Promise<T | null>;
  skeletonHtml: string;
  renderContent: (data: T, cachedAt?: number, isRefreshing?: boolean) => string;
  renderFooter: (data: T) => string;
  bindFooterEvents: (data: T, modal: HTMLElement) => void;
}

export function showDetailWithCache<T>(config: DetailModalConfig<T>): void {
  const {
    modal,
    modalBody,
    modalFooter,
    cache,
    cacheKey,
    fetchData,
    skeletonHtml,
    renderContent,
    renderFooter,
    bindFooterEvents
  } = config;

  if (!modal || !modalBody || !modalFooter) {
    console.error('[ADMIN][COMMON] showDetailWithCache: modal elements not found');
    return;
  }

  const cached = cache.get(cacheKey);

  if (cached) {
    modalBody.innerHTML = renderContent(cached.data, cached.cachedAt, true);
    modalFooter.innerHTML = renderFooter(cached.data);
    bindFooterEvents(cached.data, modal);
    showModal(modal);

    fetchData().then(freshData => {
      if (!freshData) {
        const metaEl = modalBody.querySelector('.detail-meta') as HTMLElement;
        if (metaEl) {
          metaEl.textContent = `数据更新于 ${formatRelativeTime(cached.cachedAt)}`;
        }
        return;
      }

      cache.set(cacheKey, freshData);
      const newCachedAt = Date.now();

      if (modal && !modal.classList.contains('is-hidden')) {
        modalBody.innerHTML = renderContent(freshData, newCachedAt, false);
        modalFooter.innerHTML = renderFooter(freshData);
        bindFooterEvents(freshData, modal);
      }
    });

    return;
  }

  modalBody.innerHTML = skeletonHtml;
  modalFooter.innerHTML = '<button class="btn btn-secondary" data-close-modal>关闭</button>';
  modalFooter.querySelector('[data-close-modal]')?.addEventListener('click', () => hideModal(modal));
  showModal(modal);

  fetchData().then(data => {
    if (!data) {
      hideModal(modal);
      showToast('获取详情失败', 'error');
      return;
    }

    cache.set(cacheKey, data);
    modalBody.innerHTML = renderContent(data, Date.now(), false);
    modalFooter.innerHTML = renderFooter(data);
    bindFooterEvents(data, modal);
  });
}
