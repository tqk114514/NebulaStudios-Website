/**
 * shared/js/utils/api.ts
 * 统一 fetch 请求封装（account / admin 模块共用）
 *
 * 功能：
 * - 统一 fetch 请求
 * - 安全的 JSON 解析（处理非 JSON 响应）
 * - 区分网络错误与服务端错误
 * - 自动携带凭证
 * - 401 未认证跳转登录页（account 版可跳过）
 * - 403 返回 FORBIDDEN
 *
 * 说明：account 后端接口返回展开式 { success, ...fields }，admin 后端接口返回
 * { success, data } 包裹式，因此提供两个封装共享同一核心逻辑。
 */

import type { ApiErrorResponse } from '../types/auth.ts';
import { isRefreshEndpoint, refreshSession } from './session-refresh.ts';

/** 展开式响应（account API：{ success, ...fields }） */
export type FetchResult<T = Record<string, unknown>> =
  | (T & { success: true; message?: string; errorCode?: undefined })
  | ApiErrorResponse;

/** data 包裹式响应（admin API：{ success, data }） */
export type FetchDataResult<T = Record<string, unknown>> =
  | { success: true; data: T }
  | ApiErrorResponse;

export interface FetchOptions extends RequestInit {
  /** 401 时不跳转登录页，仅返回错误 */
  skipAuthRedirect?: boolean;
}

interface JsonResponse {
  status: number;
  data: unknown;
}

/** 执行请求并解析 JSON；非 JSON 响应返回 data=null */
async function requestJson(url: string, options?: RequestInit): Promise<JsonResponse> {
  const response = await fetch(url, {
    credentials: 'include',
    ...options,
    headers: {
      // FormData 上传时由浏览器自动设置 multipart boundary，不能覆盖 Content-Type
      ...(options?.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
      ...options?.headers,
    },
  });

  const contentType = response.headers.get('content-type') || '';
  if (!contentType.includes('application/json')) {
    return { status: response.status, data: null };
  }

  return { status: response.status, data: await response.json() };
}

/** 请求 + 401 自动续期重放（刷新端点自身不重试，避免死循环） */
async function requestWithAuthRetry(url: string, options?: RequestInit): Promise<JsonResponse> {
  const result = await requestJson(url, options);
  if (result.status !== 401 || isRefreshEndpoint(url)) {
    return result;
  }
  const outcome = await refreshSession();
  if (outcome === 'fail') {
    return result;
  }
  // 'ok'（本次刷新成功）或 'retry'（其他 tab 已刷新）：新 token 已由 Set-Cookie 写入，重放原请求
  return requestJson(url, options);
}

/**
 * 供 blob 下载等非 JSON 请求使用：401 时静默续期并重试一次。
 * （fetchApi/fetchApiData 已内置此逻辑，但下载需要原始 Response 才能取 blob）
 */
export async function fetchWithAuthRetry(url: string, options?: RequestInit): Promise<Response> {
  let resp = await fetch(url, { credentials: 'include', ...options });
  if (resp.status === 401 && !isRefreshEndpoint(url)) {
    const outcome = await refreshSession();
    if (outcome !== 'fail') {
      resp = await fetch(url, { credentials: 'include', ...options });
    }
  }
  return resp;
}

/**
 * 展开式响应 fetch 封装（account 模块 API：{ success, ...fields }）
 */
export async function fetchApi<T = Record<string, unknown>>(url: string, options?: FetchOptions): Promise<FetchResult<T>> {
  try {
    const { skipAuthRedirect, ...fetchOptions } = options || {};

    const result = await requestWithAuthRetry(url, fetchOptions);

    // 走到这里仍是 401 = 静默续期失败（refresh_token 已失效），才跳登录
    if (result.status === 401) {
      if (!skipAuthRedirect) {
        window.location.href = '/account/login';
      }
      return { success: false, errorCode: 'SESSION_EXPIRED' } as FetchResult<T>;
    }

    // 注意：403 等其它状态不在此特判，直接透传后端 JSON，保留 USER_BANNED 等业务错误码；
    // admin 模块需要的 403 → FORBIDDEN 特判见 fetchApiData
    if (result.data === null) {
      return { success: false, errorCode: 'SERVER_ERROR' } as FetchResult<T>;
    }

    return result.data as FetchResult<T>;
  } catch (error) {
    if (error instanceof TypeError) {
      return { success: false, errorCode: 'NETWORK_ERROR' } as FetchResult<T>;
    }
    console.error('[API] Request failed:', error);
    return { success: false, errorCode: 'SERVER_ERROR' } as FetchResult<T>;
  }
}

/**
 * data 包裹式响应 fetch 封装（admin API：{ success, data }）
 */
export async function fetchApiData<T = Record<string, unknown>>(url: string, options?: RequestInit): Promise<FetchDataResult<T>> {
  try {
    const result = await requestWithAuthRetry(url, options);

    // 走到这里仍是 401 = 静默续期失败，才跳登录
    if (result.status === 401) {
      window.location.href = '/account/login';
      return { success: false, errorCode: 'SESSION_EXPIRED' } as FetchDataResult<T>;
    }

    if (result.status === 403) {
      return { success: false, errorCode: 'FORBIDDEN' } as FetchDataResult<T>;
    }

    if (result.data === null) {
      return { success: false, errorCode: 'SERVER_ERROR' } as FetchDataResult<T>;
    }

    return result.data as FetchDataResult<T>;
  } catch (error) {
    if (error instanceof TypeError) {
      return { success: false, errorCode: 'NETWORK_ERROR' } as FetchDataResult<T>;
    }
    console.error('[API] Request failed:', error);
    return { success: false, errorCode: 'SERVER_ERROR' } as FetchDataResult<T>;
  }
}
