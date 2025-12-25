/**
 * UI 反馈模块
 * 
 * 功能：
 * - Loading 加载状态
 * - Toast 提示消息
 * - Modal 弹窗管理
 */

// ==================== Loading ====================

/**
 * 创建加载动画 HTML
 * @returns {string} HTML 字符串
 */
export function createLoadingSpinner() {
  return '<div class="loading-spinner"></div>';
}

/**
 * 显示加载状态
 * @param {HTMLElement} container - 容器元素
 * @param {string} message - 加载提示文本
 */
export function showLoading(container, message = '') {
  if (!container) return;
  
  // 清空容器
  container.innerHTML = '';
  
  // 创建加载状态容器
  const loadingState = document.createElement('div');
  loadingState.className = 'loading-state';
  
  // 如果有消息，创建消息元素（使用 textContent 防止 XSS）
  if (message) {
    const messageEl = document.createElement('div');
    messageEl.className = 'loading-message';
    messageEl.textContent = message;
    loadingState.appendChild(messageEl);
  }
  
  // 创建加载动画
  const spinner = document.createElement('div');
  spinner.className = 'loading-spinner';
  loadingState.appendChild(spinner);
  
  container.appendChild(loadingState);
}

/**
 * 隐藏加载状态
 * @param {HTMLElement} container - 容器元素
 */
export function hideLoading(container) {
  if (!container) return;
  const loadingState = container.querySelector('.loading-state');
  if (loadingState) loadingState.remove();
}

// ==================== Toast ====================

/** Toast 容器 */
let toastContainer = null;

/**
 * 初始化 Toast 容器
 */
function initToastContainer() {
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
 * @param {string} message - 提示消息
 * @param {string} type - 类型：info/success/error/warning
 * @param {number} duration - 显示时长（毫秒）
 */
export function showToast(message, type = 'info', duration = 3000) {
  initToastContainer();
  
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.textContent = message;
  toastContainer.appendChild(toast);
  
  // 触发显示动画
  requestAnimationFrame(() => toast.classList.add('is-visible'));
  
  // 自动隐藏
  setTimeout(() => {
    toast.classList.remove('is-visible');
    toast.classList.add('is-hidden');
    setTimeout(() => toast.parentNode?.removeChild(toast), 300);
  }, duration);
}

/** Toast 快捷方法 */
export const showSuccess = (msg, duration) => showToast(msg, 'success', duration);
export const showError = (msg, duration) => showToast(msg, 'error', duration);
export const showInfo = (msg, duration) => showToast(msg, 'info', duration);
export const showWarning = (msg, duration) => showToast(msg, 'warning', duration);

// ==================== Modal 弹窗 ====================

/**
 * 显示通用提示弹窗
 * @param {string} message - 提示消息
 * @param {string} title - 标题
 * @param {Function} t - 翻译函数
 */
export function showAlert(message, title = '', t) {
  const alertModal = document.getElementById('alert-modal');
  const alertTitle = document.getElementById('alert-title');
  const alertMessage = document.getElementById('alert-message');
  const alertCloseBtn = document.getElementById('alert-close-btn');
  
  // 降级处理：若弹窗元素不存在，使用原生 alert
  if (!alertModal || !alertMessage) {
    window.alert(message);
    return;
  }
  
  alertMessage.textContent = message;
  if (alertTitle) alertTitle.textContent = title || (t ? t('modal.alert') : '') || '提示';
  if (alertCloseBtn) alertCloseBtn.textContent = (t ? t('modal.close') : '') || '关闭';
  alertModal.classList.remove('is-hidden');
  
  // 绑定关闭事件（先移除旧的防止重复绑定）
  const handleClose = () => {
    alertModal.classList.add('is-hidden');
    alertCloseBtn.removeEventListener('click', handleClose);
    alertModal.removeEventListener('click', handleOverlayClick);
  };
  
  const handleOverlayClick = (e) => {
    if (e.target === alertModal) handleClose();
  };
  
  alertCloseBtn.addEventListener('click', handleClose);
  alertModal.addEventListener('click', handleOverlayClick);
}

/**
 * 关闭提示弹窗
 */
export function closeAlert() {
  document.getElementById('alert-modal')?.classList.add('is-hidden');
}

/**
 * 显示确认弹窗
 * @param {string} message - 确认消息
 * @param {string} title - 标题（可选）
 * @param {Function} t - 翻译函数
 * @returns {Promise<boolean>} 用户确认返回 true，取消返回 false
 */
export function showConfirm(message, title = null, t) {
  return new Promise((resolve) => {
    const modal = document.getElementById('confirm-modal');
    const titleEl = document.getElementById('confirm-title');
    const messageEl = document.getElementById('confirm-message');
    const confirmBtn = document.getElementById('confirm-yes-btn');
    const cancelBtn = document.getElementById('confirm-no-btn');
    
    // 降级处理：若弹窗元素不存在，使用原生 confirm
    if (!modal || !messageEl) {
      resolve(window.confirm(message));
      return;
    }
    
    // 设置标题
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
    
    // 清理事件监听
    const cleanup = () => {
      modal.classList.add('is-hidden');
      confirmBtn?.removeEventListener('click', handleConfirm);
      cancelBtn?.removeEventListener('click', handleCancel);
      modal.removeEventListener('click', handleOverlayClick);
    };
    
    const handleConfirm = () => { cleanup(); resolve(true); };
    const handleCancel = () => { cleanup(); resolve(false); };
    const handleOverlayClick = (e) => { if (e.target === modal) { cleanup(); resolve(false); } };
    
    confirmBtn?.addEventListener('click', handleConfirm);
    cancelBtn?.addEventListener('click', handleCancel);
    modal.addEventListener('click', handleOverlayClick);
  });
}

/**
 * 显示外部链接确认弹窗
 * @param {string} url - 目标链接
 * @param {Function} t - 翻译函数
 */
export function showExternalLinkConfirm(url, t) {
  const modal = document.getElementById('external-link-modal');
  
  // 降级处理：若弹窗元素不存在，直接打开链接
  if (!modal) {
    window.open(url, '_blank', 'noopener,noreferrer');
    return;
  }
  
  const urlDisplay = modal.querySelector('#external-link-url');
  const confirmBtn = modal.querySelector('#external-link-confirm');
  const cancelBtn = modal.querySelector('#external-link-cancel');
  
  // 显示目标 URL
  if (urlDisplay) {
    urlDisplay.textContent = url;
    urlDisplay.href = url;
  }
  
  // 更新按钮文本
  if (confirmBtn) confirmBtn.textContent = (t ? t('modal.externalLink.continue') : '') || '继续访问';
  if (cancelBtn) cancelBtn.textContent = (t ? t('modal.cancel') : '') || '取消';
  
  // 移除旧的事件监听器（通过克隆节点），需要检查元素存在性
  if (confirmBtn && confirmBtn.parentNode) {
    const newConfirmBtn = confirmBtn.cloneNode(true);
    confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn);
    
    // 添加新的事件监听器
    newConfirmBtn.addEventListener('click', () => {
      window.open(url, '_blank', 'noopener,noreferrer');
      modal.classList.add('is-hidden');
    });
  }
  
  if (cancelBtn && cancelBtn.parentNode) {
    const newCancelBtn = cancelBtn.cloneNode(true);
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
export function closeExternalLinkModal() {
  document.getElementById('external-link-modal')?.classList.add('is-hidden');
}

/**
 * 显示支持的邮箱列表弹窗
 * @param {Object} emailProviders - 邮箱服务商对象 { domain: url }
 * @param {Function} t - 翻译函数
 */
export function showSupportedEmailsModal(emailProviders, t) {
  const modalOverlay = document.getElementById('modal-overlay');
  const supportedEmailsList = document.getElementById('supported-emails-list');
  const modalCloseBtn = document.querySelector('#modal-overlay .modal-close');
  
  // 降级处理：若弹窗元素不存在，记录警告并返回
  if (!modalOverlay || !supportedEmailsList) {
    console.warn('[UI-FEEDBACK] 支持邮箱弹窗元素不存在');
    return;
  }
  
  supportedEmailsList.innerHTML = '';
  
  // 支持两种格式：数组（旧）或对象（新）
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
  
  if (modalCloseBtn) modalCloseBtn.textContent = t('modal.close') || '关闭';
  modalOverlay.classList.remove('is-hidden');
}

/**
 * 关闭支持邮箱弹窗
 */
export function closeModal() {
  document.getElementById('modal-overlay')?.classList.add('is-hidden');
}

/**
 * 初始化弹窗事件监听
 * @param {Function} t - 翻译函数
 */
export function initializeModals(t) {
  const alertModal = document.getElementById('alert-modal');
  const alertCloseBtn = document.getElementById('alert-close-btn');
  const modalOverlay = document.getElementById('modal-overlay');
  const modalCloseBtn = document.querySelector('#modal-overlay .modal-close');
  const externalLinkModal = document.getElementById('external-link-modal');
  
  // 绑定关闭按钮
  alertCloseBtn?.addEventListener('click', closeAlert);
  modalCloseBtn?.addEventListener('click', closeModal);
  
  // 点击遮罩关闭
  alertModal?.addEventListener('click', (e) => {
    if (e.target === alertModal) closeAlert();
  });
  
  modalOverlay?.addEventListener('click', (e) => {
    if (e.target === modalOverlay) closeModal();
  });
  
  externalLinkModal?.addEventListener('click', (e) => {
    if (e.target === externalLinkModal) closeExternalLinkModal();
  });
  
  // ESC 键关闭
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
 * @param {Function} t - 翻译函数
 */
export function initializeModalTranslations(t) {
  // 翻译关闭按钮
  document.querySelectorAll('.modal-close').forEach(btn => {
    if (btn.hasAttribute('data-i18n')) {
      const key = btn.getAttribute('data-i18n');
      btn.textContent = t(key) || btn.textContent;
    }
  });
  
  // 翻译弹窗内的元素
  document.querySelectorAll('[data-i18n]').forEach(element => {
    if (element.closest('.modal-overlay')) {
      const key = element.getAttribute('data-i18n');
      element.textContent = t(key) || element.textContent;
    }
  });
}
