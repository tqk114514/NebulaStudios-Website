/**
 * 验证码过期管理模块
 * 
 * 功能：
 * - 验证码过期倒计时显示
 * - 过期状态检查
 * - Cookie 持久化
 */

import { setCookie, getCookie, deleteCookie } from '../../../../../shared/js/utils/cookie.js';

// ==================== 状态变量 ====================

/** 过期倒计时定时器 */
let codeExpiryTimer = null;

/** 过期时间戳 */
let codeExpiryTime = null;

// ==================== 倒计时管理 ====================

/**
 * 启动验证码过期倒计时
 * @param {number} expireTime - 过期时间戳
 * @param {string} email - 邮箱地址
 * @param {HTMLElement} timerElement - 倒计时显示元素
 * @param {Function} onExpired - 过期回调函数
 */
export function startCodeExpiryTimer(expireTime, email, timerElement, onExpired) {
  // 保存过期时间和邮箱
  codeExpiryTime = expireTime;
  
  // 保存到 Cookie（1天有效）
  setCookie('codeExpiryTime', expireTime, 86400);
  setCookie('codeEmail', email, 86400);
  
  // 清除之前的定时器
  if (codeExpiryTimer) {
    clearInterval(codeExpiryTimer);
  }
  
  // 更新倒计时显示
  updateExpiryDisplay(timerElement, onExpired);
  
  // 每秒更新一次
  codeExpiryTimer = setInterval(() => {
    updateExpiryDisplay(timerElement, onExpired);
  }, 1000);
}

/**
 * 更新倒计时显示
 * @param {HTMLElement} timerElement - 倒计时显示元素
 * @param {Function} onExpired - 过期回调函数
 */
function updateExpiryDisplay(timerElement, onExpired) {
  if (!codeExpiryTime || !timerElement) return;

  const now = Date.now();
  const remaining = codeExpiryTime - now;
  
  if (remaining <= 0) {
    // 倒计时结束，检查服务器端是否真的过期
    checkCodeExpiry(onExpired);
    return;
  }
  
  // 计算剩余时间
  const minutes = Math.floor(remaining / 60000);
  const seconds = Math.floor((remaining % 60000) / 1000);
  
  // 格式化显示
  const timeText = `${minutes}:${seconds.toString().padStart(2, '0')}`;
  timerElement.textContent = timeText;
  
  // 根据剩余时间设置样式
  timerElement.classList.remove('warning', 'expired');
  if (remaining < 60000) {
    timerElement.classList.add('warning');
  }
}

/**
 * 检查验证码是否过期（服务器端验证）
 * @param {Function} onExpired - 过期回调函数
 */
async function checkCodeExpiry(onExpired) {
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
    
    const result = await response.json();
    
    if (result.success) {
      if (result.expired) {
        // 验证码已过期
        if (onExpired) onExpired();
      } else {
        // 服务器端验证码未过期，更新过期时间
        if (result.expireTime) {
          const timerElement = document.getElementById('code-expiry-timer');
          startCodeExpiryTimer(result.expireTime, email, timerElement, onExpired);
        }
      }
    }
  } catch (error) {
    console.error('[CODE] ERROR: Check code expiry failed:', error.message);
    // 网络错误，保守处理
    if (onExpired) onExpired();
  }
}

/**
 * 清除验证码过期倒计时
 * @param {HTMLElement} timerElement - 倒计时显示元素（可选）
 */
export function clearCodeExpiryTimer(timerElement) {
  if (codeExpiryTimer) {
    clearInterval(codeExpiryTimer);
    codeExpiryTimer = null;
  }
  
  codeExpiryTime = null;
  
  if (timerElement) {
    timerElement.textContent = '';
    timerElement.classList.remove('warning', 'expired', 'verified');
  }
  
  // 清除 Cookie
  deleteCookie('codeExpiryTime');
  deleteCookie('codeEmail');
}

/**
 * 获取当前过期时间
 * @returns {number|null} 过期时间戳
 */
export function getCodeExpiryTime() {
  return codeExpiryTime;
}

/**
 * 设置过期时间（用于恢复状态）
 * @param {number} expireTime - 过期时间戳
 */
export function setCodeExpiryTime(expireTime) {
  codeExpiryTime = expireTime;
}
