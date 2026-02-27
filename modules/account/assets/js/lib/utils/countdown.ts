/**
 * 倒计时工具模块
 *
 * 功能：
 * - 发送验证码按钮倒计时
 * - 验证码过期倒计时
 * - Cookie 持久化（页面刷新后恢复）
 * - 支持多实例（不同 cookie key）
 */

import { setCookie, getCookie, deleteCookie } from '../../../../../../shared/js/utils/cookie.ts';

// ==================== 类型定义 ====================

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 过期回调函数类型 */
type ExpiredCallback = () => void;

/** 检查过期响应 */
interface CheckExpiryResponse {
  success: boolean;
  expired?: boolean;
  expireTime?: number;
}

/** 倒计时配置 */
export interface CountdownConfig {
  /** 倒计时秒数，默认 60 */
  seconds?: number;
  /** Cookie 存储的 key，默认 'countdown_end' */
  cookieKey?: string;
  /** 完成后按钮显示的文本，默认使用 t('register.sendCodeButton') */
  completeText?: string;
  /** 关联的输入框（倒计时结束后启用） */
  input?: HTMLInputElement | null;
  /** 翻译函数 */
  t?: TranslateFunction;
  /** 完成回调 */
  onComplete?: () => void;
}

/** 倒计时实例 */
interface CountdownInstance {
  timer: ReturnType<typeof setInterval>;
  cookieKey: string;
}

// ==================== 发送按钮倒计时 ====================

/** 倒计时实例存储（按 cookieKey 索引） */
const countdownInstances = new Map<string, CountdownInstance>();

/**
 * 开始发送按钮倒计时
 *
 * @returns 清理函数，调用后停止倒计时
 */
export function startCountdown(
  button: HTMLButtonElement | null,
  config: CountdownConfig = {}
): () => void {
  if (!button) {
    console.warn('[COUNTDOWN] Button element not found');
    return () => {};
  }

  const {
    seconds = 60,
    cookieKey = 'countdown_end',
    completeText,
    input,
    t = window.t,
    onComplete
  } = config;

  // 清理同 key 的旧实例
  clearCountdown(cookieKey);

  const endTime = Date.now() + (seconds * 1000);
  setCookie(cookieKey, String(endTime), seconds, true);
  button.disabled = true;

  const getCompleteText = (): string => {
    if (completeText) {return completeText;}
    return t('register.sendCodeButton');
  };

  const updateCountdown = (): void => {
    const now = Date.now();
    const remaining = Math.ceil((endTime - now) / 1000);

    if (remaining <= 0) {
      clearCountdown(cookieKey);
      if (input) {input.disabled = false;}
      button.textContent = getCompleteText();
      if (onComplete) {onComplete();}
    } else {
      button.textContent = `${remaining}s`;
    }
  };

  updateCountdown();
  const timer = setInterval(updateCountdown, 1000);
  countdownInstances.set(cookieKey, { timer, cookieKey });

  return () => clearCountdown(cookieKey);
}

/**
 * 恢复发送按钮倒计时状态（页面刷新后）
 *
 * @returns 剩余秒数，如果没有倒计时则返回 null
 */
export function resumeCountdown(
  button: HTMLButtonElement | null,
  config: CountdownConfig = {}
): { remaining: number; cleanup: () => void } | null {
  if (!button) {
    console.warn('[COUNTDOWN] Button element not found');
    return null;
  }

  const {
    cookieKey = 'countdown_end',
    completeText,
    input,
    t = window.t,
    onComplete
  } = config;

  const endTimeStr = getCookie(cookieKey);

  if (endTimeStr) {
    const endTime = parseInt(endTimeStr, 10);
    if (isNaN(endTime)) {
      deleteCookie(cookieKey);
      return null;
    }

    const now = Date.now();
    const remaining = Math.ceil((endTime - now) / 1000);

    if (remaining > 0) {
      // 清理同 key 的旧实例
      clearCountdown(cookieKey);

      button.disabled = true;

      const getCompleteText = (): string => {
        if (completeText) {return completeText;}
        return t('register.sendCodeButton');
      };

      const updateCountdown = (): void => {
        const now = Date.now();
        const remaining = Math.ceil((endTime - now) / 1000);

        if (remaining <= 0) {
          clearCountdown(cookieKey);
          if (input) {input.disabled = false;}
          button.textContent = getCompleteText();
          if (onComplete) {onComplete();}
        } else {
          button.textContent = `${remaining}s`;
        }
      };

      updateCountdown();
      const timer = setInterval(updateCountdown, 1000);
      countdownInstances.set(cookieKey, { timer, cookieKey });

      return { remaining, cleanup: () => clearCountdown(cookieKey) };
    } else {
      deleteCookie(cookieKey);
    }
  }
  return null;
}

/**
 * 清除指定 key 的倒计时
 */
export function clearCountdown(cookieKey: string = 'countdown_end'): void {
  const instance = countdownInstances.get(cookieKey);
  if (instance) {
    clearInterval(instance.timer);
    countdownInstances.delete(cookieKey);
  }
  deleteCookie(cookieKey);
}

/**
 * 检查指定 key 是否正在倒计时
 */
export function isCountingDown(cookieKey: string = 'countdown_end'): boolean {
  if (countdownInstances.has(cookieKey)) {return true;}
  const endTime = getCookie(cookieKey);
  return !!(endTime && parseInt(endTime) > Date.now());
}

// ==================== 兼容旧 API ====================

/**
 * 清除默认倒计时（兼容旧代码）
 * @deprecated 使用 clearCountdown() 代替
 */
export function clearCountdownTimer(): void {
  clearCountdown('countdown_end');
}

// ==================== 验证码过期倒计时 ====================

/** 验证码过期倒计时定时器 */
let codeExpiryTimer: ReturnType<typeof setInterval> | null = null;

/** 验证码过期时间戳 */
let codeExpiryTime: number | null = null;

/**
 * 启动验证码过期倒计时
 */
export function startCodeExpiryTimer(
  expireTime: number,
  email: string,
  timerElement: HTMLElement | null,
  onExpired?: ExpiredCallback
): void {
  codeExpiryTime = expireTime;

  setCookie('codeExpiryTime', String(expireTime), 86400, true);
  setCookie('codeEmail', email, 86400, true);

  if (codeExpiryTimer) {
    clearInterval(codeExpiryTimer);
  }

  updateExpiryDisplay(timerElement, onExpired);

  codeExpiryTimer = setInterval(() => {
    updateExpiryDisplay(timerElement, onExpired);
  }, 1000);
}

/**
 * 更新验证码过期倒计时显示
 */
function updateExpiryDisplay(timerElement: HTMLElement | null, onExpired?: ExpiredCallback): void {
  if (!codeExpiryTime || !timerElement) {return;}

  const now = Date.now();
  const remaining = codeExpiryTime - now;

  if (remaining <= 0) {
    checkCodeExpiry(onExpired);
    return;
  }

  const minutes = Math.floor(remaining / 60000);
  const seconds = Math.floor((remaining % 60000) / 1000);

  const timeText = `${minutes}:${seconds.toString().padStart(2, '0')}`;
  timerElement.textContent = timeText;

  timerElement.classList.remove('warning', 'expired');
  if (remaining < 60000) {
    timerElement.classList.add('warning');
  }
}

/**
 * 检查验证码是否过期（服务器端验证）
 */
async function checkCodeExpiry(onExpired?: ExpiredCallback): Promise<void> {
  const email = getCookie('codeEmail');

  if (!email) {
    clearCodeExpiryTimer();
    return;
  }

  try {
    const response = await fetch('/api/auth/check-code-expiry', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email })
    });

    const result: CheckExpiryResponse = await response.json();

    if (result.success) {
      if (result.expired) {
        if (onExpired) {onExpired();}
      } else {
        if (result.expireTime) {
          const timerElement = document.getElementById('code-expiry-timer');
          startCodeExpiryTimer(result.expireTime, email, timerElement, onExpired);
        }
      }
    }
  } catch (error) {
    console.error('[COUNTDOWN] Check code expiry failed:', (error as Error).message);
    if (onExpired) {onExpired();}
  }
}

/**
 * 清除验证码过期倒计时
 */
export function clearCodeExpiryTimer(timerElement?: HTMLElement | null): void {
  if (codeExpiryTimer) {
    clearInterval(codeExpiryTimer);
    codeExpiryTimer = null;
  }

  codeExpiryTime = null;

  if (timerElement) {
    timerElement.textContent = '';
    timerElement.classList.remove('warning', 'expired', 'verified');
  }

  deleteCookie('codeExpiryTime');
  deleteCookie('codeEmail');
}

/**
 * 获取当前验证码过期时间
 */
export function getCodeExpiryTime(): number | null {
  return codeExpiryTime;
}

/**
 * 设置验证码过期时间（用于恢复状态）
 */
export function setCodeExpiryTime(expireTime: number): void {
  codeExpiryTime = expireTime;
}
