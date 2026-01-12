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
  created_at?: string;
}

export interface StatsResponse {
  totalUsers: number;
  todayNewUsers: number;
  adminCount: number;
  microsoftLinked: number;
}

export interface UserListResponse {
  users: UserPublic[];
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

// ==================== DOM 元素 ====================

export const toastContainer = document.getElementById('toast-container') as HTMLElement;
export const userModal = document.getElementById('user-modal') as HTMLElement;
export const userModalBody = document.getElementById('user-modal-body') as HTMLElement;
export const userModalFooter = document.getElementById('user-modal-footer') as HTMLElement;
export const confirmModal = document.getElementById('confirm-modal') as HTMLElement;
export const confirmTitle = document.getElementById('confirm-title') as HTMLElement;
export const confirmMessage = document.getElementById('confirm-message') as HTMLElement;
export const confirmCancel = document.getElementById('confirm-cancel') as HTMLButtonElement;
export const confirmOk = document.getElementById('confirm-ok') as HTMLButtonElement;

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

    if (response.status === 401 || response.status === 403) {
      window.location.href = '/account/login';
      return { success: false, errorCode: 'UNAUTHORIZED' };
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
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  toastContainer.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

export function showModal(modal: HTMLElement): void {
  modal.classList.remove('is-hidden');
}

export function hideModal(modal: HTMLElement): void {
  modal.classList.add('is-hidden');
}

export function showConfirm(title: string, message: string, onConfirm: () => void): void {
  confirmTitle.textContent = title;
  confirmMessage.textContent = message;
  showModal(confirmModal);

  const handleConfirm = (): void => {
    hideModal(confirmModal);
    confirmOk.removeEventListener('click', handleConfirm);
    onConfirm();
  };

  confirmOk.addEventListener('click', handleConfirm);
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
