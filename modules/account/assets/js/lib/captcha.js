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

// ==================== 状态变量 ====================

/** 所有可用的验证器配置 */
let providers = [];

/** 当前选中的验证器类型 */
let captchaType = '';

/** 当前选中的站点密钥 */
let siteKey = '';

/** 当前 Widget ID */
let widgetId = null;

/** 当前验证 Token */
let captchaToken = null;

/** SDK 脚本 URL 映射 */
const SDK_URLS = {
  turnstile: 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit',
  hcaptcha: 'https://js.hcaptcha.com/1/api.js?render=explicit'
};

// ==================== 配置加载 ====================

/**
 * 加载验证码配置
 * @returns {Promise<boolean>} 是否加载成功
 */
export async function loadCaptchaConfig() {
  try {
    const response = await fetch('/api/config/captcha');
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    const config = await response.json();
    if (!config || !config.providers || config.providers.length === 0) {
      throw new Error('Invalid config: no providers available');
    }
    
    providers = config.providers;
    
    // 随机选择一个验证器
    const selected = providers[Math.floor(Math.random() * providers.length)];
    captchaType = selected.type;
    siteKey = selected.siteKey;
    
    console.log(`[CAPTCHA] Selected provider: ${captchaType} (${providers.length} available)`);
    
    // 动态加载对应的 SDK
    await loadSDK(captchaType);
    
    return true;
  } catch (error) {
    console.error('[CAPTCHA] ERROR: Failed to load config:', error.message);
    return false;
  }
}

/**
 * 动态加载验证器 SDK
 * @param {string} type - 验证器类型
 * @returns {Promise<void>}
 */
function loadSDK(type) {
  return new Promise((resolve, reject) => {
    const url = SDK_URLS[type];
    if (!url) {
      reject(new Error(`Unknown captcha type: ${type}`));
      return;
    }
    
    // 检查是否已加载
    const existingScript = document.querySelector(`script[src^="${url.split('?')[0]}"]`);
    if (existingScript) {
      resolve();
      return;
    }
    
    const script = document.createElement('script');
    script.src = url;
    script.async = true;
    script.defer = true;
    script.onload = () => {
      console.log(`[CAPTCHA] SDK loaded: ${type}`);
      resolve();
    };
    script.onerror = () => {
      reject(new Error(`Failed to load SDK: ${type}`));
    };
    document.head.appendChild(script);
  });
}

/**
 * 获取当前验证器类型
 * @returns {string} 验证器类型
 */
export function getCaptchaType() {
  return captchaType;
}

/**
 * 获取站点密钥
 * @returns {string}
 */
export function getCaptchaSiteKey() {
  return siteKey;
}

/**
 * 获取所有可用的验证器
 * @returns {Array}
 */
export function getProviders() {
  return providers;
}

// ==================== 脚本加载 ====================

/**
 * 等待验证器 API 就绪
 * @param {number} timeout - 超时时间（毫秒）
 * @returns {Promise<boolean>} 是否就绪
 */
function waitForAPI(timeout = 5000) {
  return new Promise((resolve) => {
    const checkLoaded = () => {
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
 * @param {Function} onSuccess - 验证成功回调
 * @param {Function} onError - 验证失败回调
 * @param {Function} onExpired - 验证过期回调
 * @param {string} containerId - 容器元素 ID
 * @returns {Promise<string|null>} Widget ID
 */
export async function initCaptcha(onSuccess, onError, onExpired, containerId = 'captcha-container') {
  if (!siteKey) {
    console.warn('[CAPTCHA] WARN: Site key not loaded');
    return null;
  }

  const container = document.getElementById(containerId);
  if (!container) {
    console.error('[CAPTCHA] ERROR: Container element not found:', containerId);
    return null;
  }
  
  // 显示容器
  container.classList.remove('is-hidden');
  
  // 等待 API 就绪
  const ready = await waitForAPI();
  if (!ready) {
    console.error('[CAPTCHA] ERROR: API not ready');
    if (onError) onError();
    return null;
  }
  
  // 清除旧 widget
  removeWidget();
  
  // 清空容器内容
  container.innerHTML = '';
  
  // 根据类型渲染
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
        if (onError) onError();
        return null;
    }
  } catch (error) {
    console.error('[CAPTCHA] ERROR: Failed to render widget:', error.message);
    if (onError) onError();
    return null;
  }
  
  return widgetId;
}

/**
 * 渲染 Turnstile widget
 */
function renderTurnstile(containerId, onSuccess, onError, onExpired) {
  return window.turnstile.render(`#${containerId}`, {
    sitekey: siteKey,
    theme: 'dark',
    size: 'normal',
    callback: (token) => {
      captchaToken = token;
      if (onSuccess) onSuccess(token);
    },
    'error-callback': () => {
      console.error('[CAPTCHA] ERROR: Turnstile verification failed');
      if (onError) onError();
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] WARN: Token expired');
      captchaToken = null;
      if (onExpired) onExpired();
    }
  });
}

/**
 * 渲染 hCaptcha widget（预留）
 */
function renderHCaptcha(containerId, onSuccess, onError, onExpired) {
  return window.hcaptcha.render(containerId, {
    sitekey: siteKey,
    theme: 'dark',
    callback: (token) => {
      captchaToken = token;
      if (onSuccess) onSuccess(token);
    },
    'error-callback': () => {
      console.error('[CAPTCHA] ERROR: hCaptcha verification failed');
      if (onError) onError();
    },
    'expired-callback': () => {
      console.warn('[CAPTCHA] WARN: Token expired');
      captchaToken = null;
      if (onExpired) onExpired();
    }
  });
}

/**
 * 移除当前 widget
 */
function removeWidget() {
  if (widgetId === null) return;
  
  try {
    switch (captchaType) {
      case 'turnstile':
        if (window.turnstile) window.turnstile.remove(widgetId);
        break;
      case 'hcaptcha':
        if (window.hcaptcha) window.hcaptcha.remove(widgetId);
        break;
    }
  } catch (error) {
    console.warn('[CAPTCHA] WARN: Failed to remove widget:', error.message);
  }
  widgetId = null;
}

/**
 * 隐藏验证容器
 * @param {string} containerId - 容器元素 ID
 */
export function hideCaptcha(containerId = 'captcha-container') {
  const container = document.getElementById(containerId);
  if (container) {
    container.classList.add('is-hidden');
    container.innerHTML = '';
  }
}

/**
 * 清除验证组件
 * @param {string} containerId - 容器元素 ID
 */
export function clearCaptcha(containerId = 'captcha-container') {
  removeWidget();
  captchaToken = null;
  hideCaptcha(containerId);
}

// ==================== Token 管理 ====================

/**
 * 获取当前的验证 Token
 * @returns {string|null}
 */
export function getCaptchaToken() {
  return captchaToken;
}

/**
 * 重置验证 Token
 */
export function resetCaptchaToken() {
  captchaToken = null;
}
