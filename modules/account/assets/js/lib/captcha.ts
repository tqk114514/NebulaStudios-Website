/**
 * 通用人机验证模块
 *
 * 功能：
 * - 支持多种验证器（Turnstile、hCaptcha、reCAPTCHA 等）
 * - 统一接口，调用方无需关心具体验证器
 * - 自动加载配置和初始化
 * - 多验证器随机选择（50/50）
 *
 * 当前支持：
 * - Turnstile (Cloudflare)
 * - hCaptcha
 */

// ==================== 类型定义 ====================

/** 验证器类型 */
type CaptchaType = 'turnstile' | 'hcaptcha' | 'recaptcha' | '';

/** 验证器配置 */
interface CaptchaProvider {
  type: CaptchaType;
  siteKey: string;
}

/** 验证码配置响应 */
interface CaptchaConfig {
  providers: CaptchaProvider[];
}

/** 回调函数类型 */
type CaptchaCallback = (token?: string) => void;

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

// ==================== 状态变量 ====================

/** 所有可用的验证器配置 */
let providers: CaptchaProvider[] = [];

/** 当前选中的验证器类型 */
let captchaType: CaptchaType = '';

/** 当前选中的站点密钥 */
let siteKey: string = '';

/** 当前 Widget ID */
let widgetId: string | null = null;

/** 当前验证 Token */
let captchaToken: string | null = null;

/** SDK 脚本 URL 映射 */
const SDK_URLS: Record<string, string> = {
  turnstile: 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit',
  hcaptcha: 'https://js.hcaptcha.com/1/api.js?render=explicit'
};

// ==================== 配置加载 ====================

/**
 * 加载验证码配置
 */
export async function loadCaptchaConfig(): Promise<boolean> {
  try {
    const response = await fetch('/api/config/captcha');
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    const config: CaptchaConfig = await response.json();
    if (!config || !config.providers || config.providers.length === 0) {
      throw new Error('Invalid config: no providers available');
    }

    providers = config.providers;

    const selected = providers[Math.floor(Math.random() * providers.length)];
    captchaType = selected.type;
    siteKey = selected.siteKey;

    console.log(`[CAPTCHA] Selected provider: ${captchaType} (${providers.length} available)`);

    await loadSDK(captchaType);

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

    const existingScript = document.querySelector(`script[src^="${url.split('?')[0]}"]`);
    if (existingScript) {
      resolve();
      return;
    }

    const script = document.createElement('script');
    script.src = url;
    script.async = true;
    script.defer = true;
    script.onload = (): void => {
      console.log(`[CAPTCHA] SDK loaded: ${type}`);
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

// ==================== 组件管理 ====================

/**
 * 初始化验证组件
 */
export async function initCaptcha(
  onSuccess?: CaptchaCallback,
  onError?: CaptchaCallback,
  onExpired?: CaptchaCallback,
  containerId: string = 'captcha-container'
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

  const ready = await waitForAPI();
  if (!ready) {
    console.error('[CAPTCHA] ERROR: API not ready');
    if (onError) {onError();}
    return null;
  }

  removeWidget();
  container.innerHTML = '';

  try {
    switch (captchaType) {
      case 'turnstile':
        widgetId = renderTurnstile(containerId, onSuccess, onError, onExpired);
        break;
      case 'hcaptcha':
        widgetId = renderHCaptcha(containerId, onSuccess, onError, onExpired);
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

  return widgetId;
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
  return window.turnstile!.render(`#${containerId}`, {
    sitekey: siteKey,
    theme: 'dark',
    size: 'normal',
    callback: (token: string) => {
      captchaToken = token;
      if (onSuccess) {onSuccess(token);}
    },
    'error-callback': () => {
      console.error('[CAPTCHA] ERROR: Turnstile verification failed');
      if (onError) {onError();}
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] WARN: Token expired');
      captchaToken = null;
      if (onExpired) {onExpired();}
    }
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
  return window.hcaptcha!.render(containerId, {
    sitekey: siteKey,
    theme: 'dark',
    callback: (token: string) => {
      captchaToken = token;
      if (onSuccess) {onSuccess(token);}
    },
    'error-callback': () => {
      console.error('[CAPTCHA] ERROR: hCaptcha verification failed');
      if (onError) {onError();}
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] WARN: Token expired');
      captchaToken = null;
      if (onExpired) {onExpired();}
    }
  });
}

/**
 * 移除当前 widget
 */
function removeWidget(): void {
  if (widgetId === null) {return;}

  try {
    switch (captchaType) {
      case 'turnstile':
        if (window.turnstile) {window.turnstile.remove(widgetId);}
        break;
      case 'hcaptcha':
        if (window.hcaptcha) {window.hcaptcha.remove(widgetId);}
        break;
    }
  } catch (error) {
    console.warn('[CAPTCHA] WARN: Failed to remove widget:', (error as Error).message);
  }
  widgetId = null;
}

/**
 * 隐藏验证容器
 */
export function hideCaptcha(containerId: string = 'captcha-container'): void {
  const container = document.getElementById(containerId);
  if (container) {
    container.classList.add('is-hidden');
    container.innerHTML = '';
  }
}

/**
 * 清除验证组件
 */
export function clearCaptcha(containerId: string = 'captcha-container'): void {
  removeWidget();
  captchaToken = null;
  hideCaptcha(containerId);
}

// ==================== Token 管理 ====================

/**
 * 获取当前的验证 Token
 */
export function getCaptchaToken(): string | null {
  return captchaToken;
}

/**
 * 重置验证 Token
 */
export function resetCaptchaToken(): void {
  captchaToken = null;
}
