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
// 优先使用 Web Locks API（浏览器级互斥，无竞态）；不支持时回退 localStorage
// 时间戳锁（>3s 视为陈旧可抢占，兜底 tab 崩溃场景）。
const REFRESH_LOCK_KEY = 'nebula-refresh-lock';
const REFRESH_LOCK_STALE_MS = 3000;
const REFRESH_LOCK_WAIT_MS = 1500;
const REFRESH_FETCH_TIMEOUT_MS = 10000;

export function isRefreshEndpoint(url: string): boolean {
  try {
    return new URL(url, window.location.origin).pathname === '/api/auth/refresh';
  } catch {
    return false;
  }
}

/** localStorage 回退路径：获取刷新锁，acquired=可刷新；released=其他 tab 刚完成；timeout=等待超时 */
async function acquireRefreshLockViaStorage(): Promise<'acquired' | 'released' | 'timeout'> {
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

/** 执行刷新请求（带超时，避免挂起的 fetch 长期占用跨标签页锁） */
function performRefreshFetch(): Promise<RefreshOutcome> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REFRESH_FETCH_TIMEOUT_MS);
  return fetch('/api/auth/refresh', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    signal: controller.signal,
  })
    .then((response): RefreshOutcome => (response.status === 200 ? 'ok' : 'fail'))
    .catch((): RefreshOutcome => 'fail')
    .finally(() => clearTimeout(timer));
}

/** Web Locks 路径：拿不到锁说明其他 tab 正在刷新，轮询等锁释放（=它已完成）后返回 'retry' */
async function refreshWithWebLocks(): Promise<RefreshOutcome> {
  if (!navigator.locks) {
    return refreshWithStorageLock();
  }
  try {
    return await navigator.locks.request(REFRESH_LOCK_KEY, { ifAvailable: true }, async (lock) => {
      if (lock) {
        // 整个 fetch 期间持有锁，回调返回即释放
        return performRefreshFetch();
      }
      const deadline = Date.now() + REFRESH_LOCK_WAIT_MS;
      while (Date.now() < deadline) {
        // 短暂探测：拿到即放（不持有），仅用于判断其他 tab 是否已释放
        const freed = await navigator.locks
          .request(REFRESH_LOCK_KEY, { ifAvailable: true }, async (probe) => Boolean(probe))
          .catch(() => false);
        if (freed) {
          return 'retry';
        }
        await new Promise((r) => setTimeout(r, 50));
      }
      return 'fail';
    });
  } catch {
    // Web Locks 异常（极少见）：退化为不加锁刷新，好于直接失败
    return performRefreshFetch();
  }
}

/** localStorage 回退路径 */
async function refreshWithStorageLock(): Promise<RefreshOutcome> {
  let acquired = false;
  try {
    // localStorage 在隐私模式/禁用存储时可能抛异常：退化为不加锁刷新
    const lockOutcome = await acquireRefreshLockViaStorage().catch(() => 'acquired' as const);
    if (lockOutcome === 'released') {
      // 其他 tab 刚完成刷新：用新 cookie 直接重试
      return 'retry';
    }
    if (lockOutcome === 'timeout') {
      return 'fail';
    }
    acquired = true;
    return performRefreshFetch();
  } finally {
    if (acquired) {
      try {
        localStorage.removeItem(REFRESH_LOCK_KEY);
      } catch {
        // 存储不可用时无法释放，锁会因时间戳陈旧被其他 tab 抢占
      }
    }
  }
}

/**
 * 尝试用 refresh_token 静默续期。
 * 返回 'ok' 本次刷新成功 / 'retry' 其他 tab 已刷新（直接用新 cookie 重试）/ 'fail' 刷新失败。
 */
export async function refreshSession(): Promise<RefreshOutcome> {
  if (refreshPromise) {
    return refreshPromise;
  }
  const task = (async () => {
    let outcome: RefreshOutcome;
    try {
      if (navigator.locks) {
        outcome = await refreshWithWebLocks();
      } else {
        outcome = await refreshWithStorageLock();
      }
    } catch {
      outcome = 'fail';
    } finally {
      // 所有出口（含 'retry'/'fail' 早退与异常）都必须复位共享 promise，
      // 否则一次结果会被永久缓存，后续刷新永远拿到过期结论
      refreshPromise = null;
    }
    return outcome;
  })();
  refreshPromise = task;
  return task;
}
