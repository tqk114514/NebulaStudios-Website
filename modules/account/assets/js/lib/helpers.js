/**
 * 通用辅助函数模块
 * 
 * 功能：
 * - 倒计时管理（发送验证码按钮）
 * - UI 辅助函数（卡片高度调整、错误提示）
 * - URL 参数处理
 */

import { setCookie, getCookie, deleteCookie } from '../../../../../shared/js/utils/cookie.js';

// ==================== 倒计时管理 ====================

/** 倒计时定时器 */
let countdownTimer = null;

/**
 * 开始倒计时
 * @param {number} seconds - 倒计时秒数
 * @param {HTMLElement} button - 按钮元素
 * @param {HTMLElement} emailInput - 邮箱输入框
 * @param {Function} t - 翻译函数
 * @param {Function} onComplete - 完成回调
 */
export function startCountdown(seconds, button, emailInput, t, onComplete) {
  if (!button) {
    console.warn('[HELPERS] WARN: Button element not found for countdown');
    return;
  }
  
  const endTime = Date.now() + ((seconds || 60) * 1000);
  setCookie('countdown_end', endTime, seconds || 60);
  button.disabled = true;
  
  const updateCountdown = () => {
    const now = Date.now();
    const remaining = Math.ceil((endTime - now) / 1000);
    
    if (remaining <= 0) {
      clearInterval(countdownTimer);
      countdownTimer = null;
      deleteCookie('countdown_end');
      if (emailInput) emailInput.disabled = false;
      button.textContent = t ? t('register.sendCodeButton') : '发送验证码';
      if (onComplete) onComplete();
    } else {
      button.textContent = `${remaining}s`;
    }
  };
  
  updateCountdown();
  countdownTimer = setInterval(updateCountdown, 1000);
}

/**
 * 恢复倒计时状态（页面刷新后）
 */
export function resumeCountdown(button, emailInput, t, onComplete) {
  if (!button) {
    console.warn('[HELPERS] WARN: Button element not found for resume countdown');
    return null;
  }
  
  const endTime = getCookie('countdown_end');
  
  if (endTime) {
    const parsedEndTime = parseInt(endTime, 10);
    // 检查 parseInt 是否返回有效数字
    if (isNaN(parsedEndTime)) {
      deleteCookie('countdown_end');
      return null;
    }
    
    const now = Date.now();
    const remaining = Math.ceil((parsedEndTime - now) / 1000);
    
    if (remaining > 0) {
      button.disabled = true;

      const updateCountdown = () => {
        const now = Date.now();
        const remaining = Math.ceil((parsedEndTime - now) / 1000);
        
        if (remaining <= 0) {
          clearInterval(countdownTimer);
          countdownTimer = null;
          deleteCookie('countdown_end');
          if (emailInput) emailInput.disabled = false;
          button.textContent = t ? t('register.sendCodeButton') : '发送验证码';
          if (onComplete) onComplete();
        } else {
          button.textContent = `${remaining}s`;
        }
      };
      
      updateCountdown();
      countdownTimer = setInterval(updateCountdown, 1000);
      return { remaining };
    } else {
      deleteCookie('countdown_end');
    }
  }
  return null;
}

/**
 * 清除倒计时器
 */
export function clearCountdownTimer() {
  if (countdownTimer) {
    clearInterval(countdownTimer);
    countdownTimer = null;
  }
  deleteCookie('countdown_end');
}

/**
 * 检查是否正在倒计时
 * @returns {boolean}
 */
export function isCountingDown() {
  if (countdownTimer !== null) return true;
  const endTime = getCookie('countdown_end');
  return !!(endTime && parseInt(endTime) > Date.now());
}

// ==================== UI 辅助函数 ====================

/**
 * 动态调整卡片高度（带动画）
 * @param {HTMLElement} card - 卡片元素
 * @param {HTMLElement} loginView - 登录视图（可选）
 * @param {HTMLElement} registerView - 注册视图（可选）
 */
export function adjustCardHeight(card, loginView, registerView) {
  if (!card) return;
  
  if (!loginView && !registerView) {
    // 单视图模式：自动计算高度
    const currentHeight = card.offsetHeight;
    card.style.transition = 'none';
    card.style.height = 'auto';
    const targetHeight = card.scrollHeight;
    card.style.height = `${currentHeight}px`;
    card.offsetHeight; // 强制重绘
    card.style.transition = 'height 0.3s ease';
    card.style.height = `${targetHeight}px`;
  } else {
    // 双视图模式：根据可见视图计算
    if (!loginView || !registerView) return;
    const visibleView = registerView.classList.contains('is-hidden') ? loginView : registerView;
    card.style.transition = 'height 0.3s ease';
    card.style.height = `${visibleView.scrollHeight}px`;
  }
}


/** ResizeObserver 实例存储 */
const cardObservers = new WeakMap();

/**
 * 启用卡片内容自动高度调整
 * 当卡片内部内容变化时自动调整高度
 * @param {HTMLElement} card - 卡片元素
 * @returns {Function} 清理函数
 */
export function enableCardAutoResize(card) {
  if (!card || cardObservers.has(card)) return () => {};
  
  // 检查浏览器是否支持 ResizeObserver 和 MutationObserver
  if (typeof ResizeObserver === 'undefined' || typeof MutationObserver === 'undefined') {
    console.warn('[HELPERS] WARN: ResizeObserver or MutationObserver not supported');
    return () => {};
  }
  
  // 防抖函数
  let resizeTimeout = null;
  const debouncedAdjust = () => {
    if (resizeTimeout) clearTimeout(resizeTimeout);
    resizeTimeout = setTimeout(() => {
      adjustCardHeight(card);
    }, 50);
  };
  
  // 使用 ResizeObserver 监听内容变化
  const observer = new ResizeObserver((entries) => {
    for (const entry of entries) {
      if (entry.target !== card) {
        debouncedAdjust();
      }
    }
  });
  
  // 监听卡片内所有子元素
  const observeChildren = () => {
    card.querySelectorAll('*').forEach(child => {
      observer.observe(child);
    });
  };
  
  observeChildren();
  
  // 使用 MutationObserver 监听 DOM 变化
  const mutationObserver = new MutationObserver((mutations) => {
    let shouldReobserve = false;
    for (const mutation of mutations) {
      if (mutation.type === 'childList' && mutation.addedNodes.length > 0) {
        shouldReobserve = true;
        break;
      }
    }
    if (shouldReobserve) {
      observeChildren();
      debouncedAdjust();
    }
  });
  
  mutationObserver.observe(card, { childList: true, subtree: true });
  
  // 存储 observer 以便后续清理
  cardObservers.set(card, { resizeObserver: observer, mutationObserver });
  
  return () => disableCardAutoResize(card);
}

/**
 * 禁用卡片内容自动高度调整
 * @param {HTMLElement} card - 卡片元素
 */
export function disableCardAutoResize(card) {
  if (!card) return;
  
  const observers = cardObservers.get(card);
  if (observers) {
    observers.resizeObserver?.disconnect();
    observers.mutationObserver?.disconnect();
    cardObservers.delete(card);
  }
}

/**
 * 显示/隐藏错误提示并调整卡片高度
 */
export function toggleErrorMessage(show, baseHeight, card) {
  const ERROR_HEIGHT = 38;
  if (card) {
    card.style.transition = 'height 0.3s ease';
    card.style.height = show ? `${baseHeight + ERROR_HEIGHT}px` : `${baseHeight}px`;
  }
}

/**
 * 平滑调整元素高度
 */
export function smoothAdjustHeight(element, targetHeight) {
  element.style.transition = 'height 0.3s ease';
  element.style.height = `${targetHeight}px`;
}

/**
 * 延迟执行函数（确保 DOM 渲染完成）
 * @param {Function} callback - 回调函数
 */
export function delayedExecution(callback) {
  requestAnimationFrame(() => requestAnimationFrame(callback));
}

/**
 * 带条件的延迟执行
 */
export function conditionalDelayedExecution(callback, condition) {
  requestAnimationFrame(() => {
    if (!condition || condition()) callback();
  });
}

// ==================== URL 辅助函数 ====================

/**
 * 获取 URL 查询参数
 * @param {string} name - 参数名
 * @returns {string|null} 参数值
 */
export function getUrlParameter(name) {
  return new URLSearchParams(window.location.search).get(name);
}

/**
 * 获取所有 URL 查询参数
 * @returns {Object} 参数对象
 */
export function getAllUrlParameters() {
  const params = {};
  for (const [key, value] of new URLSearchParams(window.location.search)) {
    params[key] = value;
  }
  return params;
}

/**
 * 更新 URL 参数（不刷新页面）
 */
export function updateUrlParameter(key, value) {
  const url = new URL(window.location);
  value ? url.searchParams.set(key, value) : url.searchParams.delete(key);
  window.history.replaceState({}, '', url);
}

/**
 * 构建带参数的 URL
 */
export function buildUrl(baseUrl, params) {
  if (!baseUrl || typeof baseUrl !== 'string') {
    console.warn('[HELPERS] WARN: Invalid base URL');
    return '';
  }
  
  try {
    const url = new URL(baseUrl, window.location.origin);
    if (params && typeof params === 'object') {
      Object.keys(params).forEach(key => {
        if (params[key] != null) url.searchParams.set(key, params[key]);
      });
    }
    return url.toString();
  } catch (error) {
    console.error('[HELPERS] ERROR: Failed to build URL:', error.message);
    return baseUrl;
  }
}
