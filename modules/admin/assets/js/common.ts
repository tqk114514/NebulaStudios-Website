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

// 用户数据缓存（带时间戳）
export interface CachedUser {
  user: UserPublic;
  cachedAt: number;
}

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

    const data = await response.json();
    return data;
  } catch (error) {
    console.error('[ADMIN] API Error:', error);
    return { success: false, errorCode: 'NETWORK_ERROR' };
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

  const handleConfirm = (): void => {
    hideModal(localConfirmModal);
    localConfirmOk.removeEventListener('click', handleConfirm);
    onConfirm();
  };

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
