/**
 * 扫码登录模块
 * 
 * 功能：
 * - 从后端获取安全 Token（AES-256-GCM 加密）
 * - 二维码生成
 * - 扫码登录弹窗管理
 * - 移动端检测
 * - 错误处理
 */

// ==================== 回调函数 ====================

/** 错误提示回调 */
let showAlertCallback = null;

/** 翻译函数 */
let translateFn = (key) => key;

// ==================== 移动端检测 ====================

/**
 * 检测是否为移动端设备
 * @returns {boolean}
 */
export function isMobileDevice() {
  const mobileUA = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini|Mobile|mobile/i.test(navigator.userAgent);
  const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
  const smallScreen = window.innerWidth <= 768;
  return mobileUA || (hasTouch && smallScreen);
}

// ==================== Token 获取 ====================

/**
 * 从后端获取安全的扫码登录 Token
 * @returns {Promise<{success: boolean, token?: string, expireTime?: number, error?: string}>}
 */
export async function fetchLoginToken() {
  try {
    const response = await fetch('/api/qr-login/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' }
    });
    
    const result = await response.json();
    
    if (result.success) {
      console.log('[QR-LOGIN] Token fetched from server');
      return { success: true, token: result.token, expireTime: result.expireTime };
    } else {
      console.error('[QR-LOGIN] ERROR: Server returned error:', result.errorCode);
      return { success: false, error: result.errorCode };
    }
  } catch (error) {
    console.error('[QR-LOGIN] ERROR: Failed to fetch token:', error.message);
    return { success: false, error: 'NETWORK_ERROR' };
  }
}

// ==================== 二维码生成 ====================

/**
 * 生成二维码到容器
 * @param {string} data - 二维码内容
 * @param {HTMLElement} container - 容器元素
 * @param {number} size - 二维码尺寸（像素）
 * @returns {boolean} 是否成功
 */
export function generateQRCode(data, container, size = 200) {
  if (!window.QRCode) {
    console.error('[QR-LOGIN] ERROR: QRCode library not loaded');
    return false;
  }
  
  try {
    new window.QRCode(container, {
      text: data,
      width: size,
      height: size,
      colorDark: '#000000',
      colorLight: '#ffffff',
      correctLevel: window.QRCode.CorrectLevel.M
    });
    return true;
  } catch (error) {
    console.error('[QR-LOGIN] ERROR: QR code generation failed:', error);
    return false;
  }
}

// ==================== 弹窗管理 ====================

/** 弹窗元素引用 */
let qrLoginModal = null;
let qrCodeContainer = null;
let qrScannedIcon = null;
let qrLoginHint = null;
let qrLoginConfirmBtn = null;
let qrLoginCloseBtn = null;

/** 当前 token（用于关闭时删除） */
let currentToken = null;

/** 当前状态 */
let currentStatus = 'pending'; // pending | scanned | confirmed

/**
 * 初始化扫码登录功能
 * @param {HTMLElement} button - 扫码登录按钮
 * @param {Object} options - 配置选项
 * @param {Function} options.showAlert - 显示弹窗的回调函数
 * @param {Function} options.t - 翻译函数
 */
export function initQrLogin(button, options = {}) {
  if (!button) return;
  
  // 保存回调函数
  if (options.showAlert) showAlertCallback = options.showAlert;
  if (options.t) translateFn = options.t;
  
  // 移动端不显示扫码登录按钮
  if (isMobileDevice()) return;
  
  // 显示按钮
  button.classList.remove('is-hidden');
  
  // 获取弹窗元素
  qrLoginModal = document.getElementById('qr-login-modal');
  qrCodeContainer = document.getElementById('qr-code-container');
  qrScannedIcon = document.getElementById('qr-scanned-icon');
  qrLoginHint = document.getElementById('qr-login-hint');
  qrLoginConfirmBtn = document.getElementById('qr-login-confirm-btn');
  qrLoginCloseBtn = document.getElementById('qr-login-close-btn');
  
  if (!qrLoginModal || !qrCodeContainer) {
    console.error('[QR-LOGIN] ERROR: Modal elements not found');
    return;
  }
  
  // 绑定按钮点击事件
  button.addEventListener('click', showQrLoginModal);
  
  // 绑定关闭按钮
  if (qrLoginCloseBtn) {
    qrLoginCloseBtn.addEventListener('click', closeQrLoginModal);
  }
  
  // 点击遮罩关闭弹窗
  qrLoginModal.addEventListener('click', (e) => {
    if (e.target === qrLoginModal) {
      closeQrLoginModal();
    }
  });
}

/**
 * 显示错误提示
 * @param {string} errorKey - 翻译键
 */
function showError(errorKey) {
  if (showAlertCallback) {
    showAlertCallback(translateFn(errorKey));
  }
}

// ==================== WebSocket 管理 ====================

/** WebSocket 连接 */
let ws = null;

/**
 * 连接 WebSocket
 * @param {string} token - 加密后的 token
 */
function connectWebSocket(token) {
  // 构建 WebSocket URL（token 通过 URL 参数传递）
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${window.location.host}/ws/qr-login?token=${encodeURIComponent(token)}`;
  
  try {
    ws = new WebSocket(wsUrl);
    
    ws.onopen = () => {
      console.log('[QR-LOGIN] WebSocket connected');
    };
    
    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        console.log('[QR-LOGIN] WebSocket message:', message.type);
        
        if (message.type === 'status') {
          handleStatusChange(message.status, message);
        }
      } catch (e) {
        console.error('[QR-LOGIN] ERROR: Invalid WebSocket message');
      }
    };
    
    ws.onclose = () => {
      console.log('[QR-LOGIN] WebSocket disconnected');
      ws = null;
    };
    
    ws.onerror = (error) => {
      console.error('[QR-LOGIN] WebSocket error:', error);
    };
  } catch (error) {
    console.error('[QR-LOGIN] ERROR: WebSocket connection failed:', error);
  }
}

/**
 * 断开 WebSocket
 */
function disconnectWebSocket() {
  if (ws) {
    ws.close();
    ws = null;
  }
}

/**
 * 处理状态变化
 * @param {string} status - 新状态
 * @param {Object} data - 附加数据
 */
function handleStatusChange(status, data = {}) {
  if (status === 'scanned') {
    setScannedState();
  } else if (status === 'confirmed') {
    // 移动端已确认，PC 端自动登录
    currentStatus = 'confirmed';
    
    // 如果有 session token，设置 cookie
    if (data.sessionToken) {
      // 通过隐藏请求设置 cookie（WebSocket 无法设置 cookie）
      setSessionAndRedirect(data.sessionToken);
    } else {
      closeQrLoginModal();
      window.location.href = '/account/dashboard';
    }
  } else if (status === 'cancelled') {
    // 移动端取消登录
    setCancelledState();
  }
}

/**
 * 设置会话并跳转
 * @param {string} sessionToken - 会话 token
 */
async function setSessionAndRedirect(sessionToken) {
  try {
    // 调用后端接口设置 cookie
    await fetch('/api/qr-login/set-session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ sessionToken })
    });
  } catch (e) {
    console.error('[QR-LOGIN] ERROR: Set session failed:', e);
  }
  
  closeQrLoginModal();
  window.location.href = '/account/dashboard';
}

/**
 * 显示扫码登录弹窗
 */
export async function showQrLoginModal() {
  if (!qrLoginModal || !qrCodeContainer) return;
  
  // 清空之前的二维码，显示加载状态
  qrCodeContainer.innerHTML = '<div class="qr-loading"></div>';
  
  // 显示弹窗
  qrLoginModal.classList.remove('is-hidden');
  
  // 从后端获取安全 Token
  const result = await fetchLoginToken();
  
  if (result.success && result.token) {
    // 保存当前 token
    currentToken = result.token;
    
    // 连接 WebSocket 监听状态变化
    connectWebSocket(result.token);
    
    // 清空加载状态
    qrCodeContainer.innerHTML = '';
    
    // 生成二维码（直接渲染到容器）
    const success = generateQRCode(result.token, qrCodeContainer);
    if (!success) {
      // 二维码生成失败
      currentToken = null;
      disconnectWebSocket();
      closeQrLoginModal();
      showError('login.qrCodeGenerateFailed');
    }
  } else {
    // Token 获取失败
    closeQrLoginModal();
    
    // 根据错误类型显示不同提示
    if (result.error === 'NETWORK_ERROR') {
      showError('error.networkError');
    } else {
      showError('login.qrTokenGenerateFailed');
    }
  }
}

/**
 * 取消/删除当前 token
 */
async function cancelCurrentToken() {
  if (!currentToken) return;
  
  try {
    await fetch('/api/qr-login/cancel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: currentToken })
    });
    console.log('[QR-LOGIN] Token cancelled');
  } catch (error) {
    console.error('[QR-LOGIN] ERROR: Failed to cancel token:', error.message);
  }
  
  currentToken = null;
}

/**
 * 关闭扫码登录弹窗
 */
export function closeQrLoginModal() {
  if (qrLoginModal) {
    qrLoginModal.classList.add('is-hidden');
  }
  
  // 断开 WebSocket
  disconnectWebSocket();
  
  // 关闭时删除 token（仅在未确认状态下）
  if (currentStatus !== 'confirmed') {
    cancelCurrentToken();
  }
  
  // 延迟重置状态，等弹窗动画结束后再重置，避免二维码闪现
  setTimeout(() => {
    resetModalState();
  }, 300);
}

/**
 * 重置弹窗状态
 */
function resetModalState() {
  currentStatus = 'pending';
  
  // 显示二维码容器，隐藏状态图标并重置样式
  if (qrCodeContainer) qrCodeContainer.classList.remove('is-hidden');
  if (qrScannedIcon) {
    qrScannedIcon.classList.add('is-hidden');
    qrScannedIcon.classList.remove('is-cancelled');
  }
  
  // 重置提示文本
  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrLoginHint');
  }
}

/**
 * 更新为已扫描状态
 * 当移动端扫描二维码后，PC 端显示等待确认状态
 */
export function setScannedState() {
  if (currentStatus !== 'pending') return;
  
  currentStatus = 'scanned';
  console.log('[QR-LOGIN] Status changed to scanned');
  
  // 隐藏二维码，显示成功图标
  if (qrCodeContainer) qrCodeContainer.classList.add('is-hidden');
  if (qrScannedIcon) {
    qrScannedIcon.classList.remove('is-hidden', 'is-cancelled');
  }
  
  // 更新提示文本（等待移动端确认）
  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrWaitingConfirm');
  }
}

/**
 * 更新为已取消状态
 * 当移动端取消登录后，PC 端显示取消状态
 */
export function setCancelledState() {
  if (currentStatus !== 'scanned') return;
  
  currentStatus = 'cancelled';
  console.log('[QR-LOGIN] Status changed to cancelled');
  
  // 显示取消图标（勾变叉）
  if (qrScannedIcon) {
    qrScannedIcon.classList.add('is-cancelled');
  }
  
  // 更新提示文本
  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrLoginCancelled');
  }
}
