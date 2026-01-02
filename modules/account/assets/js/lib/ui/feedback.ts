/**
 * UI 反馈模块
 *
 * 功能：
 * - Loading 加载状态
 * - Toast 提示消息
 * - Modal 弹窗管理
 * - 通用弹窗控制器
 */

// ==================== 类型定义 ====================

/** Toast 类型 */
type ToastType = 'info' | 'success' | 'error' | 'warning';

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 邮箱服务商类型 */
type EmailProviders = Record<string, string> | string[];

/** 弹窗控制器配置 */
export interface ModalControllerConfig {
  /** 弹窗元素 ID */
  modalId: string;
  /** 确认按钮 ID（可选） */
  confirmBtnId?: string;
  /** 取消按钮 ID（可选） */
  cancelBtnId?: string;
  /** 点击遮罩层是否关闭，默认 true */
  closeOnOverlay?: boolean;
  /** 额外清理逻辑（如清理 captcha、定时器等） */
  onCleanup?: () => void;
}

/** 弹窗控制器返回值 */
export interface ModalController {
  /** 打开弹窗 */
  open: () => void;
  /** 关闭弹窗 */
  close: () => void;
  /** 是否已清理 */
  isCleanedUp: () => boolean;
  /** 设置确认回调 */
  onConfirm: (handler: () => void | Promise<void>) => void;
  /** 设置取消回调 */
  onCancel: (handler: () => void) => void;
  /** 弹窗元素 */
  modal: HTMLElement | null;
}

// ==================== Loading ====================

/**
 * 创建加载动画 HTML
 */
export function createLoadingSpinner(): string {
  return '<div class="loading-spinner"></div>';
}

/**
 * 显示加载状态
 */
export function showLoading(container: HTMLElement | null, message: string = ''): void {
  if (!container) {return;}

  container.innerHTML = '';

  const loadingState = document.createElement('div');
  loadingState.className = 'loading-state';

  if (message) {
    const messageEl = document.createElement('div');
    messageEl.className = 'loading-message';
    messageEl.textContent = message;
    loadingState.appendChild(messageEl);
  }

  const spinner = document.createElement('div');
  spinner.className = 'loading-spinner';
  loadingState.appendChild(spinner);

  container.appendChild(loadingState);
}

/**
 * 隐藏加载状态
 */
export function hideLoading(container: HTMLElement | null): void {
  if (!container) {return;}
  const loadingState = container.querySelector('.loading-state');
  if (loadingState) {loadingState.remove();}
}

// ==================== Toast ====================

/** Toast 容器 */
let toastContainer: HTMLElement | null = null;

/**
 * 初始化 Toast 容器
 */
function initToastContainer(): void {
  if (!toastContainer) {
    toastContainer = document.getElementById('toast-container');

    if (!toastContainer) {
      toastContainer = document.createElement('div');
      toastContainer.id = 'toast-container';
      toastContainer.className = 'toast-container';
      document.body.appendChild(toastContainer);
    }
  }
}

/**
 * 显示 Toast 提示
 */
export function showToast(message: string, type: ToastType = 'info', duration: number = 3000): void {
  initToastContainer();

  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;
  toastContainer!.appendChild(toast);

  requestAnimationFrame(() => toast.classList.add('is-visible'));

  setTimeout(() => {
    toast.classList.remove('is-visible');
    toast.classList.add('is-hidden');
    setTimeout(() => toast.parentNode?.removeChild(toast), 300);
  }, duration);
}

/** Toast 快捷方法 */
export const showSuccess = (msg: string, duration?: number): void => showToast(msg, 'success', duration);
export const showError = (msg: string, duration?: number): void => showToast(msg, 'error', duration);
export const showInfo = (msg: string, duration?: number): void => showToast(msg, 'info', duration);
export const showWarning = (msg: string, duration?: number): void => showToast(msg, 'warning', duration);

// ==================== Modal 弹窗 ====================

/**
 * 显示通用提示弹窗
 */
export function showAlert(message: string, title: string = '', t?: TranslateFunction): void {
  const alertModal = document.getElementById('alert-modal');
  const alertTitle = document.getElementById('alert-title');
  const alertMessage = document.getElementById('alert-message');
  const alertCloseBtn = document.getElementById('alert-close-btn');

  if (!alertModal || !alertMessage) {
    window.alert(message);
    return;
  }

  alertMessage.textContent = message;
  if (alertTitle) {alertTitle.textContent = title || (t ? t('modal.alert') : '') || '提示';}
  if (alertCloseBtn) {alertCloseBtn.textContent = (t ? t('modal.close') : '') || '关闭';}
  alertModal.classList.remove('is-hidden');

  const handleClose = (): void => {
    alertModal.classList.add('is-hidden');
    alertCloseBtn?.removeEventListener('click', handleClose);
    alertModal.removeEventListener('click', handleOverlayClick);
  };

  const handleOverlayClick = (e: Event): void => {
    if (e.target === alertModal) {handleClose();}
  };

  alertCloseBtn?.addEventListener('click', handleClose);
  alertModal.addEventListener('click', handleOverlayClick);
}

/**
 * 关闭提示弹窗
 */
export function closeAlert(): void {
  document.getElementById('alert-modal')?.classList.add('is-hidden');
}

/**
 * 显示确认弹窗
 */
export function showConfirm(message: string, title: string | null = null, t?: TranslateFunction): Promise<boolean> {
  return new Promise((resolve) => {
    const modal = document.getElementById('confirm-modal');
    const titleEl = document.getElementById('confirm-title');
    const messageEl = document.getElementById('confirm-message');
    const confirmBtn = document.getElementById('confirm-yes-btn');
    const cancelBtn = document.getElementById('confirm-no-btn');

    if (!modal || !messageEl) {
      resolve(window.confirm(message));
      return;
    }

    if (titleEl) {
      if (title) {
        titleEl.textContent = title;
        titleEl.removeAttribute('data-i18n');
      } else {
        titleEl.textContent = t ? t('modal.confirm') : '确认';
        titleEl.setAttribute('data-i18n', 'modal.confirm');
      }
    }

    messageEl.textContent = message;
    modal.classList.remove('is-hidden');

    const cleanup = (): void => {
      modal.classList.add('is-hidden');
      confirmBtn?.removeEventListener('click', handleConfirm);
      cancelBtn?.removeEventListener('click', handleCancel);
      modal.removeEventListener('click', handleOverlayClick);
    };

    const handleConfirm = (): void => { cleanup(); resolve(true); };
    const handleCancel = (): void => { cleanup(); resolve(false); };
    const handleOverlayClick = (e: Event): void => { if (e.target === modal) { cleanup(); resolve(false); } };

    confirmBtn?.addEventListener('click', handleConfirm);
    cancelBtn?.addEventListener('click', handleCancel);
    modal.addEventListener('click', handleOverlayClick);
  });
}

/**
 * 显示外部链接确认弹窗
 */
export function showExternalLinkConfirm(url: string, t?: TranslateFunction): void {
  const modal = document.getElementById('external-link-modal');

  if (!modal) {
    window.open(url, '_blank', 'noopener,noreferrer');
    return;
  }

  const urlDisplay = modal.querySelector('#external-link-url') as HTMLAnchorElement | null;
  const confirmBtn = modal.querySelector('#external-link-confirm');
  const cancelBtn = modal.querySelector('#external-link-cancel');

  if (urlDisplay) {
    urlDisplay.textContent = url;
    urlDisplay.href = url;
  }

  if (confirmBtn) {confirmBtn.textContent = (t ? t('modal.externalLink.continue') : '') || '继续访问';}
  if (cancelBtn) {cancelBtn.textContent = (t ? t('modal.cancel') : '') || '取消';}

  if (confirmBtn && confirmBtn.parentNode) {
    const newConfirmBtn = confirmBtn.cloneNode(true) as HTMLElement;
    confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn);

    newConfirmBtn.addEventListener('click', () => {
      window.open(url, '_blank', 'noopener,noreferrer');
      modal.classList.add('is-hidden');
    });
  }

  if (cancelBtn && cancelBtn.parentNode) {
    const newCancelBtn = cancelBtn.cloneNode(true) as HTMLElement;
    cancelBtn.parentNode.replaceChild(newCancelBtn, cancelBtn);

    newCancelBtn.addEventListener('click', () => {
      modal.classList.add('is-hidden');
    });
  }

  modal.classList.remove('is-hidden');
}

/**
 * 关闭外部链接确认弹窗
 */
export function closeExternalLinkModal(): void {
  document.getElementById('external-link-modal')?.classList.add('is-hidden');
}

/**
 * 显示支持的邮箱列表弹窗
 */
export function showSupportedEmailsModal(emailProviders: EmailProviders, t: TranslateFunction): void {
  const modalOverlay = document.getElementById('modal-overlay');
  const supportedEmailsList = document.getElementById('supported-emails-list');
  const modalCloseBtn = document.querySelector('#modal-overlay .modal-close');

  if (!modalOverlay || !supportedEmailsList) {
    console.warn('[UI] 支持邮箱弹窗元素不存在');
    return;
  }

  supportedEmailsList.innerHTML = '';

  if (Array.isArray(emailProviders)) {
    emailProviders.forEach(domain => {
      const item = document.createElement('div');
      item.className = 'email-provider-item';
      item.textContent = domain;
      supportedEmailsList.appendChild(item);
    });
  } else {
    Object.entries(emailProviders).forEach(([domain, url]) => {
      const item = document.createElement('div');
      item.className = 'email-provider-item';
      item.textContent = domain;
      item.style.cursor = 'pointer';
      item.addEventListener('click', () => {
        showExternalLinkConfirm(url, t);
      });
      supportedEmailsList.appendChild(item);
    });
  }

  if (modalCloseBtn) {modalCloseBtn.textContent = t('modal.close') || '关闭';}
  modalOverlay.classList.remove('is-hidden');
}

/**
 * 关闭支持邮箱弹窗
 */
export function closeModal(): void {
  document.getElementById('modal-overlay')?.classList.add('is-hidden');
}

/**
 * 初始化弹窗事件监听
 */
export function initializeModals(t: TranslateFunction): void {
  const alertModal = document.getElementById('alert-modal');
  const alertCloseBtn = document.getElementById('alert-close-btn');
  const modalOverlay = document.getElementById('modal-overlay');
  const modalCloseBtn = document.querySelector('#modal-overlay .modal-close');
  const externalLinkModal = document.getElementById('external-link-modal');

  alertCloseBtn?.addEventListener('click', closeAlert);
  modalCloseBtn?.addEventListener('click', closeModal);

  alertModal?.addEventListener('click', (e) => {
    if (e.target === alertModal) {closeAlert();}
  });

  modalOverlay?.addEventListener('click', (e) => {
    if (e.target === modalOverlay) {closeModal();}
  });

  externalLinkModal?.addEventListener('click', (e) => {
    if (e.target === externalLinkModal) {closeExternalLinkModal();}
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      if (externalLinkModal && !externalLinkModal.classList.contains('is-hidden')) {
        closeExternalLinkModal();
      } else if (alertModal && !alertModal.classList.contains('is-hidden')) {
        closeAlert();
      } else if (modalOverlay && !modalOverlay.classList.contains('is-hidden')) {
        closeModal();
      }
    }
  });

  initializeModalTranslations(t);
}

/**
 * 初始化弹窗翻译
 */
export function initializeModalTranslations(t: TranslateFunction): void {
  document.querySelectorAll('.modal-close').forEach(btn => {
    if (btn.hasAttribute('data-i18n')) {
      const key = btn.getAttribute('data-i18n');
      if (key) {btn.textContent = t(key) || btn.textContent;}
    }
  });

  document.querySelectorAll('[data-i18n]').forEach(element => {
    if (element.closest('.modal-overlay')) {
      const key = element.getAttribute('data-i18n');
      if (key) {element.textContent = t(key) || element.textContent;}
    }
  });
}

// ==================== 通用弹窗控制器 ====================

/**
 * 创建通用弹窗控制器
 *
 * 用于管理复杂弹窗的打开、关闭、事件绑定和清理逻辑
 *
 * @example
 * ```ts
 * const controller = createModalController({
 *   modalId: 'my-modal',
 *   confirmBtnId: 'my-confirm-btn',
 *   cancelBtnId: 'my-cancel-btn',
 *   onCleanup: () => clearCaptcha('my-captcha')
 * });
 *
 * controller.onConfirm(async () => {
 *   await doSomething();
 *   controller.close();
 * });
 *
 * controller.open();
 * ```
 */
export function createModalController(config: ModalControllerConfig): ModalController {
  const modal = document.getElementById(config.modalId);
  const confirmBtn = config.confirmBtnId ? document.getElementById(config.confirmBtnId) : null;
  const cancelBtn = config.cancelBtnId ? document.getElementById(config.cancelBtnId) : null;
  const closeOnOverlay = config.closeOnOverlay !== false;

  let isCleanedUp = false;
  let confirmHandler: (() => void | Promise<void>) | null = null;
  let cancelHandler: (() => void) | null = null;

  const close = (): void => {
    if (isCleanedUp) {return;}
    isCleanedUp = true;

    modal?.classList.add('is-hidden');

    confirmBtn?.removeEventListener('click', handleConfirm);
    cancelBtn?.removeEventListener('click', handleCancel);
    modal?.removeEventListener('click', handleOverlayClick);

    config.onCleanup?.();
  };

  const handleConfirm = (): void => {
    if (isCleanedUp) {return;}
    confirmHandler?.();
  };

  const handleCancel = (): void => {
    if (isCleanedUp) {return;}
    cancelHandler?.();
    close();
  };

  const handleOverlayClick = (e: MouseEvent): void => {
    if (e.target === modal && closeOnOverlay) {
      cancelHandler?.();
      close();
    }
  };

  const open = (): void => {
    if (!modal) {return;}

    isCleanedUp = false;
    modal.classList.remove('is-hidden');

    confirmBtn?.addEventListener('click', handleConfirm);
    cancelBtn?.addEventListener('click', handleCancel);
    modal.addEventListener('click', handleOverlayClick);
  };

  return {
    open,
    close,
    isCleanedUp: (): boolean => isCleanedUp,
    onConfirm: (handler): void => { confirmHandler = handler; },
    onCancel: (handler): void => { cancelHandler = handler; },
    modal
  };
}
