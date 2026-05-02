/**
 * Account 模块 API 请求工具
 *
 * 功能：
 * - 统一 fetch 请求封装
 * - 安全的 JSON 解析（处理非 JSON 响应）
 * - 区分网络错误与服务端错误
 * - 自动携带凭证
 */

import type { ApiErrorResponse } from '../../../../../../shared/js/types/auth.ts';

export type FetchResult<T = Record<string, unknown>> =
  | (T & { success: true; message?: string; errorCode?: undefined })
  | ApiErrorResponse;

export async function fetchApi<T = Record<string, unknown>>(url: string, options?: RequestInit): Promise<FetchResult<T>> {
  try {
    const response = await fetch(url, {
      credentials: 'include',
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...options?.headers,
      },
    });

    if (response.status === 401) {
      window.location.href = '/account/login';
      return { success: false, errorCode: 'SESSION_EXPIRED' } as FetchResult<T>;
    }

    const contentType = response.headers.get('content-type') || '';
    if (!contentType.includes('application/json')) {
      console.error('[ACCOUNT] Server returned non-JSON response:', response.status, contentType);
      return { success: false, errorCode: 'SERVER_ERROR' } as FetchResult<T>;
    }

    const data = await response.json();
    return data as FetchResult<T>;
  } catch (error) {
    if (error instanceof TypeError) {
      return { success: false, errorCode: 'NETWORK_ERROR' } as FetchResult<T>;
    }
    console.error('[ACCOUNT] API Error:', error);
    return { success: false, errorCode: 'SERVER_ERROR' } as FetchResult<T>;
  }
}
