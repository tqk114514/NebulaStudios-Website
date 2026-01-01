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

import { createModalController, type ModalController } from './ui/feedback.ts';

// ==================== 类型定义 ====================

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 显示弹窗回调类型 */
type ShowAlertCallback = (message: string) => void;

/** Token 获取结果 */
interface TokenResult {
  success: boolean;
  token?: string;
  expireTime?: number;
  error?: string;
}

/** WebSocket 消息 */
interface WSMessage {
  type: string;
  status?: string;
  sessionToken?: string;
}

/** 初始化选项 */
interface QrLoginOptions {
  showAlert?: ShowAlertCallback;
  t?: TranslateFunction;
}

/** 登录状态 */
type LoginStatus = 'pending' | 'scanned' | 'confirmed' | 'cancelled';

// ==================== 回调函数 ====================

/** 错误提示回调 */
let showAlertCallback: ShowAlertCallback | null = null;

/** 翻译函数 */
let translateFn: TranslateFunction = (key) => key;

// ==================== 移动端检测 ====================

/**
 * 检测是否为移动端设备
 */
export function isMobileDevice(): boolean {
  const mobileUA = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini|Mobile|mobile/i.test(navigator.userAgent);
  const hasTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
  const smallScreen = window.innerWidth <= 768;
  return mobileUA || (hasTouch && smallScreen);
}

// ==================== Token 获取 ====================

/**
 * 从后端获取安全的扫码登录 Token
 */
export async function fetchLoginToken(): Promise<TokenResult> {
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
    console.error('[QR-LOGIN] ERROR: Failed to fetch token:', (error as Error).message);
    return { success: false, error: 'NETWORK_ERROR' };
  }
}

// ==================== 二维码生成 ====================

/**
 * 生成二维码到容器
 */
export function generateQRCode(data: string, container: HTMLElement, size: number = 200): boolean {
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
let qrCodeContainer: HTMLElement | null = null;
let qrScannedIcon: HTMLElement | null = null;
let qrLoginHint: HTMLElement | null = null;

/** 弹窗控制器 */
let modalController: ModalController | null = null;

/** 当前 token（用于关闭时删除） */
let currentToken: string | null = null;

/** 当前状态 */
let currentStatus: LoginStatus = 'pending';

/**
 * 初始化扫码登录功能
 */
export function initQrLogin(button: HTMLElement | null, options: QrLoginOptions = {}): void {
  if (!button) return;

  if (options.showAlert) showAlertCallback = options.showAlert;
  if (options.t) translateFn = options.t;

  if (isMobileDevice()) return;

  button.classList.remove('is-hidden');

  qrCodeContainer = document.getElementById('qr-code-container');
  qrScannedIcon = document.getElementById('qr-scanned-icon');
  qrLoginHint = document.getElementById('qr-login-hint');

  if (!qrCodeContainer) {
    console.error('[QR-LOGIN] ERROR: Modal elements not found');
    return;
  }

  // 创建弹窗控制器
  modalController = createModalController({
    modalId: 'qr-login-modal',
    cancelBtnId: 'qr-login-close-btn',
    closeOnOverlay: true,
    onCleanup: () => {
      disconnectWebSocket();
      if (currentStatus !== 'confirmed') {
        cancelCurrentToken();
      }
      setTimeout(() => resetModalState(), 300);
    }
  });

  button.addEventListener('click', showQrLoginModal);
}

/**
 * 显示错误提示
 */
function showError(errorKey: string): void {
  if (showAlertCallback) {
    showAlertCallback(translateFn(errorKey));
  }
}

// ==================== WebSocket 管理 ====================

/** WebSocket 连接 */
let ws: WebSocket | null = null;

/**
 * 连接 WebSocket
 */
function connectWebSocket(token: string): void {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${window.location.host}/ws/qr-login?token=${encodeURIComponent(token)}`;

  try {
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      console.log('[QR-LOGIN] WebSocket connected');
    };

    ws.onmessage = (event) => {
      try {
        const message: WSMessage = JSON.parse(event.data);
        console.log('[QR-LOGIN] WebSocket message:', message.type);

        if (message.type === 'status' && message.status) {
          handleStatusChange(message.status, message);
        }
      } catch {
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
function disconnectWebSocket(): void {
  if (ws) {
    ws.close();
    ws = null;
  }
}

/**
 * 处理状态变化
 */
function handleStatusChange(status: string, data: WSMessage = { type: 'status' }): void {
  if (status === 'scanned') {
    setScannedState();
  } else if (status === 'confirmed') {
    currentStatus = 'confirmed';

    if (data.sessionToken) {
      setSessionAndRedirect(data.sessionToken);
    } else {
      closeQrLoginModal();
      window.location.href = '/account/dashboard';
    }
  } else if (status === 'cancelled') {
    setCancelledState();
  }
}

/**
 * 设置会话并跳转
 */
async function setSessionAndRedirect(sessionToken: string): Promise<void> {
  try {
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
export async function showQrLoginModal(): Promise<void> {
  if (!modalController || !qrCodeContainer) return;

  qrCodeContainer.innerHTML = '<div class="qr-loading"></div>';
  modalController.open();

  const result = await fetchLoginToken();

  if (result.success && result.token) {
    currentToken = result.token;
    connectWebSocket(result.token);
    qrCodeContainer.innerHTML = '';

    const success = generateQRCode(result.token, qrCodeContainer);
    if (!success) {
      currentToken = null;
      disconnectWebSocket();
      modalController.close();
      showError('login.qrCodeGenerateFailed');
    }
  } else {
    modalController.close();

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
async function cancelCurrentToken(): Promise<void> {
  if (!currentToken) return;

  try {
    await fetch('/api/qr-login/cancel', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: currentToken })
    });
    console.log('[QR-LOGIN] Token cancelled');
  } catch (error) {
    console.error('[QR-LOGIN] ERROR: Failed to cancel token:', (error as Error).message);
  }

  currentToken = null;
}

/**
 * 关闭扫码登录弹窗
 */
export function closeQrLoginModal(): void {
  modalController?.close();
}

/**
 * 重置弹窗状态
 */
function resetModalState(): void {
  currentStatus = 'pending';

  if (qrCodeContainer) qrCodeContainer.classList.remove('is-hidden');
  if (qrScannedIcon) {
    qrScannedIcon.classList.add('is-hidden');
    qrScannedIcon.classList.remove('is-cancelled');
  }

  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrLoginHint');
  }
}

/**
 * 更新为已扫描状态
 */
export function setScannedState(): void {
  if (currentStatus !== 'pending') return;

  currentStatus = 'scanned';
  console.log('[QR-LOGIN] Status changed to scanned');

  if (qrCodeContainer) qrCodeContainer.classList.add('is-hidden');
  if (qrScannedIcon) {
    qrScannedIcon.classList.remove('is-hidden', 'is-cancelled');
  }

  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrWaitingConfirm');
  }
}

/**
 * 更新为已取消状态
 */
export function setCancelledState(): void {
  if (currentStatus !== 'scanned') return;

  currentStatus = 'cancelled';
  console.log('[QR-LOGIN] Status changed to cancelled');

  if (qrScannedIcon) {
    qrScannedIcon.classList.add('is-cancelled');
  }

  if (qrLoginHint) {
    qrLoginHint.textContent = translateFn('login.qrLoginCancelled');
  }
}
