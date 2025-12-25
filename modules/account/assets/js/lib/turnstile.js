/**
 * Cloudflare Turnstile 人机验证配置模块
 * 
 * 功能：
 * - 加载 Turnstile 配置
 * - 初始化验证组件
 * - 管理验证 Token
 */

// ==================== 状态变量 ====================

/** Turnstile 站点密钥 */
let TURNSTILE_SITE_KEY = '';

/** 当前 Widget ID */
let turnstileWidgetId = null;

/** 当前验证 Token */
let turnstileToken = null;

// ==================== 配置加载 ====================

/**
 * 加载 Turnstile 配置
 * @returns {Promise<boolean>} 是否加载成功
 */
export async function loadTurnstileConfig() {
  try {
    const response = await fetch('/api/config/turnstile');
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    const config = await response.json();
    if (!config || !config.siteKey) {
      throw new Error('Invalid config: missing siteKey');
    }
    TURNSTILE_SITE_KEY = config.siteKey;
    return true;
  } catch (error) {
    console.error('[TURNSTILE] ERROR: Failed to load config:', error.message);
    return false;
  }
}

/**
 * 获取 Turnstile 站点密钥
 * @returns {string}
 */
export function getTurnstileSiteKey() {
  return TURNSTILE_SITE_KEY;
}

// ==================== 组件管理 ====================

/**
 * 等待 Turnstile 脚本加载完成
 * @param {number} timeout - 超时时间（毫秒）
 * @returns {Promise<boolean>} 是否加载成功
 */
function waitForTurnstile(timeout = 5000) {
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
        console.error('[TURNSTILE] ERROR: Script load timeout');
        resolve(false);
      }
    }, 100);
  });
}

/**
 * 初始化 Turnstile 验证组件
 * @param {Function} onSuccess - 验证成功回调
 * @param {Function} onError - 验证失败回调
 * @param {Function} onExpired - 验证过期回调
 * @param {string} containerId - 容器元素 ID
 * @returns {Promise<string|null>} Widget ID
 */
export async function initTurnstile(onSuccess, onError, onExpired, containerId = 'turnstile-container') {
  if (!TURNSTILE_SITE_KEY) {
    console.warn('[TURNSTILE] WARN: Site key not loaded');
    return null;
  }

  const container = document.getElementById(containerId);
  if (!container) {
    console.error('[TURNSTILE] ERROR: Container element not found:', containerId);
    return null;
  }
  
  // 显示容器
  container.classList.remove('is-hidden');
  
  // 等待 Turnstile 脚本加载
  const loaded = await waitForTurnstile();
  if (!loaded) {
    console.error('[TURNSTILE] ERROR: Script not loaded');
    if (onError) onError();
    return null;
  }
  
  // 如果已有 widget，先清除
  if (turnstileWidgetId !== null && window.turnstile) {
    window.turnstile.remove(turnstileWidgetId);
    turnstileWidgetId = null;
  }
  
  // 清空容器内容（防止重复渲染）
  container.innerHTML = '';
  
  // 渲染新的 widget
  try {
    turnstileWidgetId = window.turnstile.render(`#${containerId}`, {
      sitekey: TURNSTILE_SITE_KEY,
      theme: 'dark',
      size: 'normal',
      callback: function(token) {
        turnstileToken = token;
        if (onSuccess) onSuccess(token);
      },
      'error-callback': function() {
        console.error('[TURNSTILE] ERROR: Verification failed');
        if (onError) onError();
      },
      'expired-callback': function() {
        console.warn('[TURNSTILE] WARN: Token expired');
        turnstileToken = null;
        if (onExpired) onExpired();
      }
    });
  } catch (error) {
    console.error('[TURNSTILE] ERROR: Failed to render widget:', error.message);
    if (onError) onError();
    return null;
  }
  
  return turnstileWidgetId;
}

/**
 * 隐藏 Turnstile 容器
 * @param {string} containerId - 容器元素 ID
 */
export function hideTurnstile(containerId = 'turnstile-container') {
  const container = document.getElementById(containerId);
  if (container) {
    container.classList.add('is-hidden');
    container.innerHTML = '';
  }
}

/**
 * 清除 Turnstile Widget
 * @param {string} containerId - 容器元素 ID
 */
export function clearTurnstile(containerId = 'turnstile-container') {
  if (turnstileWidgetId !== null && window.turnstile) {
    try {
      window.turnstile.remove(turnstileWidgetId);
    } catch (error) {
      console.warn('[TURNSTILE] WARN: Failed to remove widget:', error.message);
    }
    turnstileWidgetId = null;
  }
  turnstileToken = null;
  hideTurnstile(containerId);
}

// ==================== Token 管理 ====================

/**
 * 获取当前的 Turnstile Token
 * @returns {string|null}
 */
export function getTurnstileToken() {
  return turnstileToken;
}

/**
 * 重置 Turnstile Token
 */
export function resetTurnstileToken() {
  turnstileToken = null;
}
