/**
 * URL 工具模块
 *
 * 功能：
 * - URL 参数获取
 * - URL 参数更新
 * - URL 构建
 */

// ==================== URL 参数处理 ====================

/**
 * 获取 URL 查询参数
 */
export function getUrlParameter(name: string): string | null {
  return new URLSearchParams(window.location.search).get(name);
}

/**
 * 获取所有 URL 查询参数
 */
export function getAllUrlParameters(): Record<string, string> {
  const params: Record<string, string> = {};
  for (const [key, value] of new URLSearchParams(window.location.search)) {
    params[key] = value;
  }
  return params;
}

/**
 * 更新 URL 参数（不刷新页面）
 */
export function updateUrlParameter(key: string, value: string | null): void {
  const url = new URL(window.location.href);
  value ? url.searchParams.set(key, value) : url.searchParams.delete(key);
  window.history.replaceState({}, '', url);
}

/**
 * 构建带参数的 URL
 */
export function buildUrl(baseUrl: string, params?: Record<string, string | null>): string {
  if (!baseUrl || typeof baseUrl !== 'string') {
    console.warn('[URL] Invalid base URL');
    return '';
  }

  try {
    const url = new URL(baseUrl, window.location.origin);
    if (params && typeof params === 'object') {
      Object.keys(params).forEach(key => {
        if (params[key] != null) url.searchParams.set(key, params[key]!);
      });
    }
    return url.toString();
  } catch (error) {
    console.error('[URL] Failed to build URL:', (error as Error).message);
    return baseUrl;
  }
}
