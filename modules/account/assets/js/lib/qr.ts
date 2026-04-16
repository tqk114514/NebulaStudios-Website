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
import { fetchApi } from './api/fetch.ts';

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

// ==================== 状态管理 ====================

/** 模块状态（封装所有模块级变量） */
const state = {
  /** 弹窗元素引用 */
  qrCodeContainer: null as HTMLElement | null,
  qrScannedIcon: null as HTMLElement | null,
  qrLoginHint: null as HTMLElement | null,

  /** 弹窗控制器 */
  modalController: null as ModalController | null,

  /** 当前 token（用于关闭时删除） */
  currentToken: null as string | null,

  /** 当前状态 */
  currentStatus: 'pending' as LoginStatus,

  /** WebSocket 连接 */
  ws: null as WebSocket | null,

  /** 错误提示回调 */
  showAlertCallback: null as ShowAlertCallback | null,

  /** 翻译函数 */
  translateFn: ((key: string) => key) as TranslateFunction
};

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
  const result = await fetchApi<{ token: string; expireTime: number }>('/api/qr-login/generate', {
    method: 'POST'
  });

  if (result.success) {
    console.log('[QR-LOGIN] Token fetched from server');
    return { success: true, token: result.token, expireTime: result.expireTime };
  } else {
    console.error('[QR-LOGIN] ERROR: Server returned error:', result.errorCode);
    return { success: false, error: result.errorCode };
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

/**
 * 初始化扫码登录功能
 */
export function initQrLogin(button: HTMLElement | null, options: QrLoginOptions = {}): void {
  if (!button) {return;}

  if (options.showAlert) {state.showAlertCallback = options.showAlert;}
  if (options.t) {state.translateFn = options.t;}

  if (isMobileDevice()) {return;}

  button.classList.remove('is-hidden');

  state.qrCodeContainer = document.getElementById('qr-code-container');
  state.qrScannedIcon = document.getElementById('qr-scanned-icon');
  state.qrLoginHint = document.getElementById('qr-login-hint');

  if (!state.qrCodeContainer) {
    console.error('[QR-LOGIN] ERROR: Modal elements not found');
    return;
  }

  // 创建弹窗控制器
  state.modalController = createModalController({
    modalId: 'qr-login-modal',
    cancelBtnId: 'qr-login-close-btn',
    closeOnOverlay: true,
    onCleanup: () => {
      disconnectWebSocket();
      if (state.currentStatus !== 'confirmed') {
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
  if (state.showAlertCallback) {
    state.showAlertCallback(state.translateFn(errorKey));
  }
}

// ==================== WebSocket 管理 ====================

/**
 * 连接 WebSocket
 */
function connectWebSocket(token: string): void {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${protocol}//${window.location.host}/ws/qr-login?token=${encodeURIComponent(token)}`;

  try {
    state.ws = new WebSocket(wsUrl);

    state.ws.onopen = (): void => {
      console.log('[QR-LOGIN] WebSocket connected');
    };

    state.ws.onmessage = (event): void => {
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

    state.ws.onclose = (): void => {
      console.log('[QR-LOGIN] WebSocket disconnected');
      state.ws = null;
    };

    state.ws.onerror = (error): void => {
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
  if (state.ws) {
    state.ws.close();
    state.ws = null;
  }
}

/**
 * 处理状态变化
 */
function handleStatusChange(status: string, data: WSMessage = { type: 'status' }): void {
  if (status === 'scanned') {
    setScannedState();
  } else if (status === 'confirmed') {
    state.currentStatus = 'confirmed';

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
  const result = await fetchApi('/api/qr-login/set-session', {
    method: 'POST',
    body: JSON.stringify({ sessionToken, token: state.currentToken })
  });

  if (!result.success) {
    console.error('[QR-LOGIN] ERROR: Set session failed:', result.errorCode);
    closeQrLoginModal();
    if (state.showAlertCallback) {
      state.showAlertCallback(state.translateFn('login.qrSessionFailed'));
    }
    return;
  }

  closeQrLoginModal();
  window.location.href = '/account/dashboard';
}

/**
 * 显示扫码登录弹窗
 */
export async function showQrLoginModal(): Promise<void> {
  if (!state.modalController || !state.qrCodeContainer) {return;}

  state.qrCodeContainer.innerHTML = '<div class="qr-loading"></div>';
  state.modalController.open();

  const result = await fetchLoginToken();

  if (result.success && result.token) {
    state.currentToken = result.token;
    connectWebSocket(result.token);
    state.qrCodeContainer.innerHTML = '';

    const success = generateQRCode(result.token, state.qrCodeContainer);
    if (!success) {
      state.currentToken = null;
      disconnectWebSocket();
      state.modalController.close();
      showError('login.qrCodeGenerateFailed');
    }
  } else {
    state.modalController.close();

    if (result.error === 'NETWORK_ERROR') {
      showError('error.networkError');
    } else if (result.error === 'SERVER_ERROR') {
      showError('error.serverError');
    } else {
      showError('login.qrTokenGenerateFailed');
    }
  }
}

/**
 * 取消/删除当前 token
 */
async function cancelCurrentToken(): Promise<void> {
  if (!state.currentToken) {return;}

  await fetchApi('/api/qr-login/cancel', {
    method: 'POST',
    body: JSON.stringify({ token: state.currentToken })
  });
  console.log('[QR-LOGIN] Token cancelled');

  state.currentToken = null;
}

/**
 * 关闭扫码登录弹窗
 */
export function closeQrLoginModal(): void {
  state.modalController?.close();
}

/**
 * 重置弹窗状态
 */
function resetModalState(): void {
  state.currentStatus = 'pending';

  if (state.qrCodeContainer) {state.qrCodeContainer.classList.remove('is-hidden');}
  if (state.qrScannedIcon) {
    state.qrScannedIcon.classList.add('is-hidden');
    state.qrScannedIcon.classList.remove('is-cancelled');
  }

  if (state.qrLoginHint) {
    state.qrLoginHint.textContent = state.translateFn('login.qrLoginHint');
  }
}

/**
 * 更新为已扫描状态
 */
export function setScannedState(): void {
  if (state.currentStatus !== 'pending') {return;}

  state.currentStatus = 'scanned';
  console.log('[QR-LOGIN] Status changed to scanned');

  if (state.qrCodeContainer) {state.qrCodeContainer.classList.add('is-hidden');}
  if (state.qrScannedIcon) {
    state.qrScannedIcon.classList.remove('is-hidden', 'is-cancelled');
  }

  if (state.qrLoginHint) {
    state.qrLoginHint.textContent = state.translateFn('login.qrWaitingConfirm');
  }
}

/**
 * 更新为已取消状态
 */
export function setCancelledState(): void {
  if (state.currentStatus !== 'scanned') {return;}

  state.currentStatus = 'cancelled';
  console.log('[QR-LOGIN] Status changed to cancelled');

  if (state.qrScannedIcon) {
    state.qrScannedIcon.classList.add('is-cancelled');
  }

  if (state.qrLoginHint) {
    state.qrLoginHint.textContent = state.translateFn('login.qrLoginCancelled');
  }
}
