/**
 * 人机验证模块
 *
 * 功能：
 * - 统一的验证组件接口
 * - 自动加载配置和初始化
 * - 支持多实例（基于容器 ID 隔离状态）
 */

// ==================== 类型定义 ====================

import { fetchApi } from './api/fetch.ts';

/** 回调函数类型 */
type CaptchaCallback = (token?: string) => void;

/** 单个验证码实例的状态 */
interface CaptchaInstance {
  widgetId: string | null;
  token: string | null;
}

/** Turnstile API */
interface TurnstileAPI {
  render: (selector: string, options: TurnstileOptions) => string;
  remove: (widgetId: string) => void;
}

interface TurnstileOptions {
  sitekey: string;
  theme?: string;
  size?: string;
  callback?: (token: string) => void;
  'error-callback'?: () => void;
  'expired-callback'?: () => void;
}

// 扩展 Window 接口
declare global {
  interface Window {
    turnstile?: TurnstileAPI;
  }
}

// ==================== 全局共享状态 ====================

const SDK_URL = '{{TURNSTILE_SDK_URL}}';

/** 站点密钥（全局共享） */
let siteKey: string = '';

// ==================== 实例状态（按容器 ID 隔离） ====================

/** 各容器的验证码实例状态 */
const instances = new Map<string, CaptchaInstance>();

/**
 * 获取指定容器的实例状态
 */
function getInstance(containerId: string): CaptchaInstance {
  let instance = instances.get(containerId);
  if (!instance) {
    instance = { widgetId: null, token: null };
    instances.set(containerId, instance);
  }
  return instance;
}

// ==================== 配置加载 ====================

/**
 * 加载验证码配置
 */
export async function loadCaptchaConfig(): Promise<boolean> {
  try {
    const result = await fetchApi<{ data: { siteKey: string } }>('/api/config/captcha');
    if (!result.success || !result.data) {
      throw new Error('Invalid captcha config response');
    }

    siteKey = result.data.siteKey || '';

    if (siteKey) {
      loadSDK().catch((err) => {
        console.warn('[CAPTCHA] SDK background load failed:', (err as Error).message);
      });
    }

    return true;
  } catch (error) {
    console.error('[CAPTCHA] Failed to load config:', (error as Error).message);
    return false;
  }
}

/**
 * 动态加载 Turnstile SDK
 */
function loadSDK(): Promise<void> {
  return new Promise((resolve, reject) => {
    const existingScript = document.querySelector<HTMLScriptElement>(`script[src^="${SDK_URL.split('?')[0]}"]`);
    if (existingScript) {
      if (existingScript.dataset.loaded === 'true') {
        resolve();
        return;
      }
      existingScript.addEventListener('load', () => resolve(), { once: true });
      existingScript.addEventListener('error', () => reject(new Error('Failed to load captcha SDK')), { once: true });
      return;
    }

    const script = document.createElement('script');
    script.src = SDK_URL;
    script.async = true;
    script.defer = true;
    script.onload = (): void => {
      script.dataset.loaded = 'true';
      resolve();
    };
    script.onerror = (): void => {
      reject(new Error('Failed to load captcha SDK'));
    };
    document.head.appendChild(script);
  });
}

/**
 * 获取站点密钥
 */
export function getCaptchaSiteKey(): string {
  return siteKey;
}

// ==================== API 就绪等待 ====================

/**
 * 等待 Turnstile API 就绪
 */
function waitForAPI(timeout: number = 5000): Promise<boolean> {
  return new Promise((resolve) => {
    if (window.turnstile) {
      resolve(true);
      return;
    }

    const startTime = Date.now();
    const checkInterval = setInterval(() => {
      if (window.turnstile) {
        clearInterval(checkInterval);
        resolve(true);
      } else if (Date.now() - startTime > timeout) {
        clearInterval(checkInterval);
        console.error('[CAPTCHA] API not ready after timeout');
        resolve(false);
      }
    }, 100);
  });
}

// ==================== 辅助函数 ====================

/**
 * 构建验证器回调函数（绑定到指定容器实例）
 */
function buildCallbacks(
  containerId: string,
  onSuccess?: CaptchaCallback,
  onError?: CaptchaCallback,
  onExpired?: CaptchaCallback
) {
  const instance = getInstance(containerId);
  return {
    callback: (token: string) => {
      instance.token = token;
      if (onSuccess) { onSuccess(token); }
    },
    'error-callback': () => {
      console.error('[CAPTCHA] Verification failed');
      if (onError) { onError(); }
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] Token expired');
      instance.token = null;
      if (onExpired) { onExpired(); }
    }
  };
}

// ==================== 组件管理 ====================

/**
 * 初始化验证组件
 */
export async function initCaptcha(
  containerId: string,
  onSuccess?: CaptchaCallback,
  onError?: CaptchaCallback,
  onExpired?: CaptchaCallback
): Promise<string | null> {
  if (!siteKey) {
    console.warn('[CAPTCHA] Site key not loaded');
    return null;
  }

  const container = document.getElementById(containerId);
  if (!container) {
    console.error('[CAPTCHA] Container element not found:', containerId);
    return null;
  }

  container.classList.remove('is-hidden');

  let ready = await waitForAPI();
  if (!ready) {
    console.warn('[CAPTCHA] API not ready, retrying SDK load...');
    try {
      await loadSDK();
      ready = await waitForAPI(8000);
    } catch (err) {
      console.error('[CAPTCHA] SDK reload failed:', (err as Error).message);
    }
  }
  if (!ready) {
    console.error('[CAPTCHA] API not ready after retry');
    if (onError) { onError(); }
    return null;
  }

  removeWidget(containerId);
  container.innerHTML = '';

  const instance = getInstance(containerId);

  try {
    const options = buildCallbacks(containerId, onSuccess, onError, onExpired);

    instance.widgetId = window.turnstile!.render(`#${containerId}`, {
      sitekey: siteKey,
      theme: 'dark',
      size: 'normal',
      ...options
    });
  } catch (error) {
    console.error('[CAPTCHA] Failed to render widget:', (error as Error).message);
    if (onError) { onError(); }
    return null;
  }

  return instance.widgetId;
}

/**
 * 移除指定容器的 widget
 */
function removeWidget(containerId: string): void {
  const instance = getInstance(containerId);
  if (instance.widgetId === null) { return; }

  try {
    if (window.turnstile) { window.turnstile.remove(instance.widgetId); }
  } catch (error) {
    console.warn('[CAPTCHA] Failed to remove widget:', (error as Error).message);
  }
  instance.widgetId = null;
}

/**
 * 隐藏验证容器
 */
export function hideCaptcha(containerId: string): void {
  const container = document.getElementById(containerId);
  if (container) {
    container.classList.add('is-hidden');
    container.innerHTML = '';
  }
}

/**
 * 清除验证组件
 */
export function clearCaptcha(containerId: string): void {
  removeWidget(containerId);
  const instance = getInstance(containerId);
  instance.token = null;
  hideCaptcha(containerId);
}

// ==================== Token 管理 ====================

/**
 * 获取指定容器的验证 Token
 */
export function getCaptchaToken(containerId: string): string | null {
  return getInstance(containerId).token;
}

/**
 * 重置指定容器的验证 Token
 */
export function resetCaptchaToken(containerId: string): void {
  getInstance(containerId).token = null;
}