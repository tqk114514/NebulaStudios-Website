/**
 * 通用人机验证模块
 *
 * 功能：
 * - 支持多种验证器（Turnstile、hCaptcha、reCAPTCHA 等）
 * - 统一接口，调用方无需关心具体验证器
 * - 自动加载配置和初始化
 * - 多验证器随机选择（50/50）
 * - 支持多实例（基于容器 ID 隔离状态）
 *
 * 当前支持：
 * - Turnstile (Cloudflare)
 * - hCaptcha
 */

// ==================== 类型定义 ====================

import { fetchApi } from './api/fetch.ts';

/** 验证器类型 */
type CaptchaType = 'turnstile' | 'hcaptcha' | 'recaptcha' | '';

/** 验证器配置 */
interface CaptchaProvider {
  type: CaptchaType;
  siteKey: string;
}

/** 验证码配置 */
interface CaptchaConfig {
  providers: CaptchaProvider[];
}

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

/** hCaptcha API */
interface HCaptchaAPI {
  render: (containerId: string, options: HCaptchaOptions) => string;
  remove: (widgetId: string) => void;
}

interface HCaptchaOptions {
  sitekey: string;
  theme?: string;
  callback?: (token: string) => void;
  'error-callback'?: () => void;
  'expired-callback'?: () => void;
}

// 扩展 Window 接口
declare global {
  interface Window {
    turnstile?: TurnstileAPI;
    hcaptcha?: HCaptchaAPI;
    grecaptcha?: unknown;
  }
}

// ==================== 全局共享状态 ====================

/** 所有可用的验证器配置（全局共享，只加载一次） */
let providers: CaptchaProvider[] = [];

/** 当前选中的验证器类型（全局共享） */
let captchaType: CaptchaType = '';

/** 当前选中的站点密钥（全局共享） */
let siteKey: string = '';

/** SDK 脚本 URL 映射 */
const SDK_URLS: Record<string, string> = {
  turnstile: 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit',
  hcaptcha: 'https://js.hcaptcha.com/1/api.js?render=explicit'
};

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
    const result = await fetchApi<{ data: CaptchaConfig }>('/api/config/captcha');
    if (!result.success || !result.data || !result.data.providers || result.data.providers.length === 0) {
      throw new Error('Invalid config: no providers available');
    }

    providers = result.data.providers;

    const selected = providers[Math.floor(Math.random() * providers.length)];
    captchaType = selected.type;
    siteKey = selected.siteKey;

    loadSDK(captchaType).catch((err) => {
      console.warn('[CAPTCHA] WARN: SDK background load failed:', (err as Error).message);
    });

    return true;
  } catch (error) {
    console.error('[CAPTCHA] ERROR: Failed to load config:', (error as Error).message);
    return false;
  }
}

/**
 * 动态加载验证器 SDK
 */
function loadSDK(type: CaptchaType): Promise<void> {
  return new Promise((resolve, reject) => {
    if (!type) {
      reject(new Error('Captcha type not specified'));
      return;
    }

    const url = SDK_URLS[type];
    if (!url) {
      reject(new Error(`Unknown captcha type: ${type}`));
      return;
    }

    const existingScript = document.querySelector<HTMLScriptElement>(`script[src^="${url.split('?')[0]}"]`);
    if (existingScript) {
      if (existingScript.dataset.loaded === 'true') {
        resolve();
        return;
      }
      existingScript.addEventListener('load', () => resolve(), { once: true });
      existingScript.addEventListener('error', () => reject(new Error(`Failed to load SDK: ${type}`)), { once: true });
      return;
    }

    const script = document.createElement('script');
    script.src = url;
    script.async = true;
    script.defer = true;
    script.onload = (): void => {
      script.dataset.loaded = 'true';
      resolve();
    };
    script.onerror = (): void => {
      reject(new Error(`Failed to load SDK: ${type}`));
    };
    document.head.appendChild(script);
  });
}

/**
 * 获取当前验证器类型
 */
export function getCaptchaType(): CaptchaType {
  return captchaType;
}

/**
 * 获取站点密钥
 */
export function getCaptchaSiteKey(): string {
  return siteKey;
}

/**
 * 获取所有可用的验证器
 */
export function getProviders(): CaptchaProvider[] {
  return providers;
}

// ==================== 脚本加载 ====================

/**
 * 等待验证器 API 就绪
 */
function waitForAPI(timeout: number = 5000): Promise<boolean> {
  return new Promise((resolve) => {
    const checkLoaded = (): boolean => {
      switch (captchaType) {
        case 'turnstile':
          return !!window.turnstile;
        case 'hcaptcha':
          return !!window.hcaptcha;
        case 'recaptcha':
          return !!window.grecaptcha;
        default:
          return false;
      }
    };

    if (checkLoaded()) {
      resolve(true);
      return;
    }

    const startTime = Date.now();
    const checkInterval = setInterval(() => {
      if (checkLoaded()) {
        clearInterval(checkInterval);
        resolve(true);
      } else if (Date.now() - startTime > timeout) {
        clearInterval(checkInterval);
        console.error('[CAPTCHA] ERROR: API not ready for', captchaType);
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
      if (onSuccess) {onSuccess(token);}
    },
    'error-callback': () => {
      console.error('[CAPTCHA] ERROR: Verification failed');
      if (onError) {onError();}
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] WARN: Token expired');
      instance.token = null;
      if (onExpired) {onExpired();}
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
    console.warn('[CAPTCHA] WARN: Site key not loaded');
    return null;
  }

  const container = document.getElementById(containerId);
  if (!container) {
    console.error('[CAPTCHA] ERROR: Container element not found:', containerId);
    return null;
  }

  container.classList.remove('is-hidden');

  let ready = await waitForAPI();
  if (!ready) {
    console.warn('[CAPTCHA] WARN: API not ready, retrying SDK load...');
    try {
      await loadSDK(captchaType);
      ready = await waitForAPI(8000);
    } catch (err) {
      console.error('[CAPTCHA] ERROR: SDK reload failed:', (err as Error).message);
    }
  }
  if (!ready) {
    console.error('[CAPTCHA] ERROR: API not ready after retry');
    if (onError) {onError();}
    return null;
  }

  removeWidget(containerId);
  container.innerHTML = '';

  const instance = getInstance(containerId);

  try {
    switch (captchaType) {
      case 'turnstile':
        instance.widgetId = renderTurnstile(containerId, onSuccess, onError, onExpired);
        break;
      case 'hcaptcha':
        instance.widgetId = renderHCaptcha(containerId, onSuccess, onError, onExpired);
        break;
      default:
        console.error('[CAPTCHA] ERROR: Unknown captcha type:', captchaType);
        if (onError) {onError();}
        return null;
    }
  } catch (error) {
    console.error('[CAPTCHA] ERROR: Failed to render widget:', (error as Error).message);
    if (onError) {onError();}
    return null;
  }

  return instance.widgetId;
}

/**
 * 渲染 Turnstile widget
 */
function renderTurnstile(
  containerId: string,
  onSuccess?: CaptchaCallback,
  onError?: CaptchaCallback,
  onExpired?: CaptchaCallback
): string {
  if (!window.turnstile) {
    throw new Error('Turnstile API not loaded');
  }

  const options = buildCallbacks(containerId, onSuccess, onError, onExpired);

  return window.turnstile.render(`#${containerId}`, {
    sitekey: siteKey,
    theme: 'dark',
    size: 'normal',
    ...options
  });
}

/**
 * 渲染 hCaptcha widget
 */
function renderHCaptcha(
  containerId: string,
  onSuccess?: CaptchaCallback,
  onError?: CaptchaCallback,
  onExpired?: CaptchaCallback
): string {
  if (!window.hcaptcha) {
    throw new Error('hCaptcha API not loaded');
  }

  const options = buildCallbacks(containerId, onSuccess, onError, onExpired);

  return window.hcaptcha.render(containerId, {
    sitekey: siteKey,
    theme: 'dark',
    ...options
  });
}

/**
 * 移除指定容器的 widget
 */
function removeWidget(containerId: string): void {
  const instance = getInstance(containerId);
  if (instance.widgetId === null) {return;}

  try {
    switch (captchaType) {
      case 'turnstile':
        if (window.turnstile) {window.turnstile.remove(instance.widgetId);}
        break;
      case 'hcaptcha':
        if (window.hcaptcha) {window.hcaptcha.remove(instance.widgetId);}
        break;
    }
  } catch (error) {
    console.warn('[CAPTCHA] WARN: Failed to remove widget:', (error as Error).message);
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
