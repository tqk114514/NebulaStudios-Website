/**
 * shared/js/utils/session-refresh.ts
 * 会话静默续期（token 轮转的前端半截）
 *
 * 后端 POST /api/auth/refresh 会轮转 access/refresh token：
 * 旧 refresh token 标记已用并签发新 token 对（Set-Cookie），
 * 重放检测（旧 token 二次使用）会撤销整个 token 家族。
 * 因此本模块保证：只在 401 时刷新一次、并发去重、多标签页互斥。
 */

// 共享刷新 promise：并发请求同时 401 时只触发一次 refresh（去重）
let refreshPromise: Promise<RefreshOutcome> | null = null;

// 多标签页互斥：两个 tab 同时 401 会双发 refresh，第二个请求带的是已被轮转的旧
// refresh_token → 触发后端重放检测 → 撤销整个 token 家族 → 双双登出。
// localStorage 锁保证同一时刻只有一个 tab 在刷新；锁带时间戳，>3s 视为陈旧可抢占（tab 崩溃场景）。
const REFRESH_LOCK_KEY = 'nebula-refresh-lock';
const REFRESH_LOCK_STALE_MS = 3000;
const REFRESH_LOCK_WAIT_MS = 1500;

export function isRefreshEndpoint(url: string): boolean {
  try {
    return new URL(url, window.location.origin).pathname === '/api/auth/refresh';
  } catch {
    return false;
  }
}

/** 获取刷新锁：acquired=拿到锁可刷新；released=其他 tab 刚完成刷新（直接重试）；timeout=等待超时 */
async function acquireRefreshLock(): Promise<'acquired' | 'released' | 'timeout'> {
  const lock = localStorage.getItem(REFRESH_LOCK_KEY);
  if (!lock || Date.now() - Number(lock) > REFRESH_LOCK_STALE_MS) {
    localStorage.setItem(REFRESH_LOCK_KEY, String(Date.now()));
    return 'acquired';
  }
  const deadline = Date.now() + REFRESH_LOCK_WAIT_MS;
  while (Date.now() < deadline) {
    if (!localStorage.getItem(REFRESH_LOCK_KEY)) {
      return 'released';
    }
    await new Promise((r) => setTimeout(r, 50));
  }
  return 'timeout';
}

export type RefreshOutcome = 'ok' | 'retry' | 'fail';

/**
 * 尝试用 refresh_token 静默续期。
 * 返回 'ok' 本次刷新成功 / 'retry' 其他 tab 已刷新（直接用新 cookie 重试）/ 'fail' 刷新失败。
 */
export async function refreshSession(): Promise<RefreshOutcome> {
  if (refreshPromise) {
    return refreshPromise;
  }
  refreshPromise = (async () => {
    switch (await acquireRefreshLock()) {
      case 'released':
        return 'retry'; // 其他 tab 刚完成刷新：用新 cookie 直接重试，不再重复刷
      case 'timeout':
        return 'fail';
    }
    try {
      const response = await fetch('/api/auth/refresh', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
      });
      // 刷新端点自身 401 = refresh_token 也已失效，不再重试
      return response.status === 200 ? 'ok' : 'fail';
    } catch {
      return 'fail';
    } finally {
      localStorage.removeItem(REFRESH_LOCK_KEY);
      refreshPromise = null;
    }
  })();
  return refreshPromise;
}
