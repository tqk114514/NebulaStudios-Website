/**
 * assets/js/dashboard.js
 * Dashboard 页面逻辑
 * 
 * 功能�?
 * - 用户信息展示
 * - 头像管理（更换头像、使用微软头像）
 * - 微软账户绑定/解绑
 * - 修改密码（待实现�?
 * - 删除账户（需验证码和密码确认�?
 * - 登出
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, loadLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.js';
import { verifySession, logout } from './lib/auth-service.js';
import { loadTurnstileConfig, getTurnstileSiteKey, initTurnstile, clearTurnstile, getTurnstileToken } from './lib/turnstile.js';
import { showAlert as showAlertBase, showConfirm as showConfirmBase } from './lib/ui-feedback.js';
import { validateAvatarUrl, validatePassword } from './lib/validators.js';

// 翻译函数（从全局获取，若不存在则返回原始 key�?
const t = window.t || ((key) => key);

// ==================== 设备检�?====================

/**
 * 检测是否为移动设备
 * @returns {boolean} 是否为移动设�?
 */
function isMobileDevice() {
  const userAgent = navigator.userAgent.toLowerCase();
  const mobileKeywords = [
    'android', 'webos', 'iphone', 'ipad', 'ipod', 'blackberry',
    'windows phone', 'opera mini', 'iemobile', 'mobile'
  ];
  return mobileKeywords.some(keyword => userAgent.includes(keyword));
}

// ==================== 弹窗封装 ====================

/**
 * 显示提示弹窗（封装，自动传入翻译函数�?
 * @param {string} message - 提示消息内容
 */
function showAlert(message) {
  showAlertBase(message, '', t);
}

/**
 * 显示确认弹窗（封装，自动传入翻译函数�?
 * @param {string} message - 确认消息内容
 * @param {string|null} title - 弹窗标题（可选）
 * @returns {Promise<boolean>} 用户确认返回 true，取消返�?false
 */
function showConfirm(message, title = null) {
  return showConfirmBase(message, title, t);
}

// ==================== 头像相关 ====================

/**
 * 更新头像显示
 * @param {HTMLElement} avatarEl - 头像容器元素
 * @param {string|null} avatarUrl - 头像 URL
 * @param {string} username - 用户名（用于显示首字母）
 */
function updateAvatarDisplay(avatarEl, avatarUrl, username) {
  if (!avatarEl) return;
  avatarEl.innerHTML = '';
  
  if (avatarUrl) {
    // 有头�?URL，显示图�?
    const img = document.createElement('img');
    img.src = avatarUrl;
    img.alt = username;
    img.className = 'avatar-img';
    avatarEl.appendChild(img);
  } else if (username) {
    // 无头像，显示用户名首字母
    avatarEl.textContent = username.charAt(0).toUpperCase();
  }
}

/**
 * 验证 URL 格式
 * 已移�?utils/validators.js �?validateAvatarUrl 函数
 */

/**
 * 显示更改头像弹窗
 * @param {Object} user - 用户信息对象
 * @param {Function} onSuccess - 头像更新成功回调
 */
function showAvatarModal(user, onSuccess) {
  // 获取弹窗相关元素
  const modal = document.getElementById('avatar-modal');
  const currentPreview = document.getElementById('current-avatar-preview');
  const newPreview = document.getElementById('new-avatar-preview');
  const urlInput = document.getElementById('avatar-url-input');
  const errorEl = document.getElementById('avatar-error');
  const microsoftBtn = document.getElementById('use-microsoft-avatar-btn');
  const confirmBtn = document.getElementById('avatar-confirm-btn');
  const cancelBtn = document.getElementById('avatar-cancel-btn');
  
  if (!modal) return;
  
  // 重置弹窗状�?
  urlInput.value = '';
  urlInput.readOnly = false;
  urlInput.classList.remove('is-error', 'readonly-placeholder');
  errorEl.classList.add('is-hidden');
  errorEl.textContent = '';
  confirmBtn.disabled = true;
  newPreview.innerHTML = '';
  newPreview.classList.remove('is-loaded');
  
  let validatedUrl = null; // 已验证的头像 URL
  
  // 显示当前头像预览
  currentPreview.innerHTML = '';
  if (user.avatar_url) {
    const img = document.createElement('img');
    img.src = user.avatar_url;
    img.alt = user.username;
    currentPreview.appendChild(img);
  } else if (user.username) {
    currentPreview.textContent = user.username.charAt(0).toUpperCase();
  }
  
  // 微软头像按钮（只有绑定了微软账户且有头像时才显示�?
  const hasMicrosoftAvatar = user.microsoft_avatar_url && user.microsoft_avatar_url.trim();
  if (hasMicrosoftAvatar) {
    microsoftBtn.classList.remove('is-hidden');
  } else {
    microsoftBtn.classList.add('is-hidden');
  }
  
  /**
   * 加载新头像预�?
   * @param {string} url - 头像 URL
   */
  function loadNewAvatar(url) {
    newPreview.innerHTML = '';
    newPreview.classList.remove('is-loaded');
    errorEl.classList.add('is-hidden');
    urlInput.classList.remove('is-error');
    confirmBtn.disabled = true;
    validatedUrl = null;
    
    if (!url || !url.trim()) {
      return;
    }
    
    // 验证 URL 格式（前端验证）
    const urlValidation = validateAvatarUrl(url);
    if (!urlValidation.valid) {
      errorEl.textContent = t(urlValidation.errorKey);
      errorEl.classList.remove('is-hidden');
      urlInput.classList.add('is-error');
      return;
    }
    
    // 尝试加载图片验证可用�?
    const img = document.createElement('img');
    img.onload = () => {
      newPreview.innerHTML = '';
      newPreview.appendChild(img);
      newPreview.classList.add('is-loaded');
      confirmBtn.disabled = false;
      validatedUrl = url;
    };
    img.onerror = () => {
      errorEl.textContent = t('dashboard.avatarLoadFailed');
      errorEl.classList.remove('is-hidden');
      urlInput.classList.add('is-error');
    };
    img.src = url;
  }
  
  // 输入框失焦时加载预览
  const handleBlur = () => {
    if (urlInput.readOnly) return; // 使用微软头像时不处理
    loadNewAvatar(urlInput.value.trim());
  };
  
  // 输入框获得焦点时，如果是微软头像占位符则清除
  const handleFocus = () => {
    if (urlInput.readOnly && urlInput.value === '[Microsoft Avatar]') {
      urlInput.value = '';
      urlInput.readOnly = false;
      urlInput.classList.remove('readonly-placeholder');
      // 重置预览
      newPreview.innerHTML = '';
      newPreview.classList.remove('is-loaded');
      confirmBtn.disabled = true;
      validatedUrl = null;
    }
  };
  
  // 使用微软头像按钮点击
  const handleMicrosoftClick = () => {
    const msAvatarUrl = user.microsoft_avatar_url;
    // data URL 太长，输入框显示占位文本
    if (msAvatarUrl.startsWith('data:')) {
      urlInput.value = '[Microsoft Avatar]';
      urlInput.readOnly = true;
      urlInput.classList.add('readonly-placeholder');
    } else {
      urlInput.value = msAvatarUrl;
      urlInput.readOnly = false;
      urlInput.classList.remove('readonly-placeholder');
    }
    loadNewAvatar(msAvatarUrl);
  };

  // 确认更换头像
  const handleConfirm = async () => {
    if (!validatedUrl) return;
    
    confirmBtn.disabled = true;
    try {
      const response = await fetch('/api/user/avatar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ avatar_url: validatedUrl })
      });
      const result = await response.json();
      
      if (result.success) {
        cleanup();
        onSuccess(result.avatar_url);
        showAlert(t('dashboard.avatarUpdateSuccess'));
      } else {
        // 根据错误码显示对应提�?
        const errorMessages = {
          'INVALID_IMAGE_URL': 'dashboard.invalidImageUrl',
          'INVALID_URL': 'dashboard.invalidUrl',
          'URL_TOO_LONG': 'dashboard.invalidUrl'
        };
        const errorKey = errorMessages[result.errorCode] || 'dashboard.avatarUpdateFailed';
        errorEl.textContent = t(errorKey);
        errorEl.classList.remove('is-hidden');
        confirmBtn.disabled = false;
      }
    } catch (e) {
      errorEl.textContent = t('dashboard.avatarUpdateFailed');
      errorEl.classList.remove('is-hidden');
      confirmBtn.disabled = false;
    }
  };
  
  // 清理弹窗状态和事件监听
  const cleanup = () => {
    modal.classList.add('is-hidden');
    urlInput.removeEventListener('blur', handleBlur);
    urlInput.removeEventListener('focus', handleFocus);
    microsoftBtn.removeEventListener('click', handleMicrosoftClick);
    confirmBtn.removeEventListener('click', handleConfirm);
    cancelBtn.removeEventListener('click', cleanup);
    modal.removeEventListener('click', handleOverlayClick);
  };
  
  // 点击遮罩层关�?
  const handleOverlayClick = (e) => { if (e.target === modal) cleanup(); };
  
  // 绑定事件
  urlInput.addEventListener('blur', handleBlur);
  urlInput.addEventListener('focus', handleFocus);
  microsoftBtn.addEventListener('click', handleMicrosoftClick);
  confirmBtn.addEventListener('click', handleConfirm);
  cancelBtn.addEventListener('click', cleanup);
  modal.addEventListener('click', handleOverlayClick);
  
  // 显示弹窗并聚焦输入框
  modal.classList.remove('is-hidden');
  urlInput.focus();
}

// ==================== 页面初始�?====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();
    
    // 并行加载语言切换器和 Turnstile 配置
    await Promise.all([
      loadLanguageSwitcher(),
      loadTurnstileConfig()
    ]);
    
    // 验证用户会话
    const sessionResult = await verifySession();
    
    if (!sessionResult.success) {
      console.warn('[DASHBOARD] WARN: Session invalid:', sessionResult.errorCode);
      window.location.href = '/account/login';
      return;
    }
    
    // 隐藏页面加载遮罩
    hidePageLoader();
  
  const user = sessionResult.data;

  // 检�?URL 参数（处理绑定结果提示）
  const urlParams = new URLSearchParams(window.location.search);
  const success = urlParams.get('success');
  const error = urlParams.get('error');
  
  // 显示绑定结果提示
  if (success === 'microsoft_linked') {
    setTimeout(() => showAlert(t('dashboard.linkSuccess')), 100);
    window.history.replaceState({}, document.title, window.location.pathname);
  } else if (error === 'microsoft_already_linked') {
    setTimeout(() => showAlert(t('dashboard.microsoftAlreadyLinked')), 100);
    window.history.replaceState({}, document.title, window.location.pathname);
  } else if (error === 'session_expired') {
    setTimeout(() => showAlert(t('error.sessionExpired')), 100);
    window.history.replaceState({}, document.title, window.location.pathname);
  }

  // ==================== 用户信息展示 ====================
  
  // 头部欢迎区元�?
  const usernameEl = document.getElementById('display-username');
  const emailEl = document.getElementById('display-email');
  const avatarEl = document.getElementById('user-avatar');

  // 显示用户�?
  if (usernameEl) {
    usernameEl.textContent = user.username;
    usernameEl.removeAttribute('data-i18n');
  }
  
  // 显示邮箱
  if (emailEl) {
    emailEl.textContent = user.email;
    emailEl.removeAttribute('data-i18n');
  }
  
  // 显示头像
  if (avatarEl) {
    if (user.avatar_url) {
      const img = document.createElement('img');
      img.src = user.avatar_url;
      img.alt = user.username;
      img.className = 'avatar-img';
      avatarEl.textContent = '';
      avatarEl.appendChild(img);
    } else if (user.username) {
      avatarEl.textContent = user.username.charAt(0).toUpperCase();
    }
  }

  // ==================== 账户信息列表 ====================
  
  const infoUsername = document.getElementById('info-username');
  const infoEmail = document.getElementById('info-email');
  const infoMicrosoft = document.getElementById('info-microsoft');

  if (infoUsername) infoUsername.textContent = user.username;
  if (infoEmail) infoEmail.textContent = user.email;
  
  const microsoftLinkItem = document.getElementById('microsoft-link-item');
  
  /**
   * 更新微软账户绑定状态显�?
   * @param {boolean} isLinked - 是否已绑�?
   * @param {string|null} microsoftName - 微软账户名称
   */
  function updateMicrosoftStatus(isLinked, microsoftName) {
    if (infoMicrosoft) {
      if (isLinked && microsoftName) {
        // 已绑定且有名�?
        infoMicrosoft.textContent = microsoftName;
        infoMicrosoft.classList.add('is-linked');
        infoMicrosoft.classList.remove('is-not-linked');
        infoMicrosoft.removeAttribute('data-i18n');
      } else if (isLinked) {
        // 已绑定但无名�?
        infoMicrosoft.textContent = t('dashboard.linked');
        infoMicrosoft.classList.add('is-linked');
        infoMicrosoft.classList.remove('is-not-linked');
        infoMicrosoft.removeAttribute('data-i18n');
      } else {
        // 未绑�?
        infoMicrosoft.textContent = t('dashboard.notLinked');
        infoMicrosoft.classList.remove('is-linked');
        infoMicrosoft.classList.add('is-not-linked');
      }
    }
  }
  
  // 初始化微软账户状�?
  updateMicrosoftStatus(!!user.microsoft_id, user.microsoft_name);
  
  // ==================== 微软账户绑定/解绑 ====================
  
  if (microsoftLinkItem) {
    microsoftLinkItem.addEventListener('click', async () => {
      if (user.microsoft_id) {
        // 解绑流程
        const confirmed = await showConfirm(t('dashboard.confirmUnlink'), t('dashboard.unlinkThirdParty'));
        if (!confirmed) return;
        
        try {
          const response = await fetch('/api/auth/microsoft/unlink', {
            method: 'POST',
            credentials: 'include'
          });
          const result = await response.json();
          
          if (result.success) {
            user.microsoft_id = null;
            user.microsoft_name = null;
            updateMicrosoftStatus(false, null);
            showAlert(t('dashboard.unlinkSuccess'));
          } else {
            showAlert(t('dashboard.unlinkFailed'));
          }
        } catch (e) {
          showAlert(t('dashboard.unlinkFailed'));
        }
      } else {
        // 绑定流程
        const confirmed = await showConfirm(t('dashboard.confirmLink'), t('dashboard.linkThirdParty'));
        if (!confirmed) return;
        window.location.href = '/api/auth/microsoft?action=link';
      }
    });
  }

  // 更新页面标题
  updatePageTitle();
  
  const dashboardMain = document.querySelector('.dashboard-main');
  
  /**
   * 调整信息列表高度（带动画效果�?
   */
  function adjustInfoListHeight() {
    document.querySelectorAll('.info-list').forEach(list => {
      const currentHeight = list.offsetHeight;
      list.style.height = 'auto';
      const targetHeight = list.scrollHeight;
      list.style.height = `${currentHeight}px`;
      list.offsetHeight; // 强制重绘
      list.style.height = `${targetHeight}px`;
    });
  }
  
  // 初始化语言切换�?
  initLanguageSwitcher(() => {
    updatePageTitle();
    // 语言切换后重新应用微软账户状态和按钮文本
    updateMicrosoftStatus(!!user.microsoft_id, user.microsoft_name);
    // 触发高度过渡动画
    requestAnimationFrame(() => requestAnimationFrame(adjustInfoListHeight));
  });

  // ==================== 功能按钮事件 ====================

  // 登出按钮
  const logoutBtn = document.getElementById('logout-btn');
  if (logoutBtn) {
    logoutBtn.addEventListener('click', async () => {
      const confirmed = await showConfirm(t('dashboard.confirmLogout'), t('dashboard.logout'));
      if (confirmed) logout();
    });
  }

  // 修改密码
  const changePasswordItem = document.getElementById('change-password-item');
  if (changePasswordItem) {
    changePasswordItem.addEventListener('click', showChangePasswordModal);
  }

  // 更改头像
  const changeAvatarItem = document.getElementById('change-avatar-item');
  if (changeAvatarItem) {
    changeAvatarItem.addEventListener('click', () => {
      showAvatarModal(user, (newAvatarUrl) => {
        // 更新用户数据和页面显�?
        user.avatar_url = newAvatarUrl;
        updateAvatarDisplay(avatarEl, newAvatarUrl, user.username);
      });
    });
  }

  // 修改用户�?
  const changeUsernameItem = document.getElementById('change-username-item');
  if (changeUsernameItem) {
    changeUsernameItem.addEventListener('click', () => {
      showChangeUsernameModal(user, (newUsername) => {
        // 更新用户数据和页面显�?
        user.username = newUsername;
        if (usernameEl) usernameEl.textContent = newUsername;
        if (infoUsername) infoUsername.textContent = newUsername;
        // 更新头像显示（如果是首字母头像）
        if (!user.avatar_url && avatarEl) {
          avatarEl.textContent = newUsername.charAt(0).toUpperCase();
        }
      });
    });
  }

  // ==================== 快捷登录区域（仅移动端显示） ====================
  
  const quickLoginSection = document.getElementById('quick-login-section');
  if (quickLoginSection && isMobileDevice()) {
    quickLoginSection.classList.remove('is-hidden');
    
    // 扫码登录点击事件
    const qrScanLoginItem = document.getElementById('qr-scan-login-item');
    if (qrScanLoginItem) {
      let isScanning = false;
      qrScanLoginItem.addEventListener('click', () => {
        // 防止重复点击
        if (isScanning) {
          console.log('[QR-SCAN] Already scanning, ignoring click');
          return;
        }
        isScanning = true;
        
        // 打开扫码弹窗
        showQrScanModal(() => {
          // 弹窗关闭回调，重置状�?
          isScanning = false;
        });
      });
    }
  }

  /**
   * 检测内容是否溢出，决定是否允许滚动
   */
  function checkOverflow() {
    if (dashboardMain) {
      const isOverflow = dashboardMain.scrollHeight > window.innerHeight;
      dashboardMain.classList.toggle('is-overflow', isOverflow);
    }
  }
  checkOverflow();
  window.addEventListener('resize', checkOverflow);

  // 删除账户
  const deleteAccountItem = document.getElementById('delete-account-item');
  if (deleteAccountItem) {
    deleteAccountItem.addEventListener('click', showDeleteAccountModal);
  }

  } catch (error) {
    console.error('[DASHBOARD] ERROR: Page initialization failed:', error.message);
    hidePageLoader();
  }
});

// ==================== 删除账户弹窗 ====================

/**
 * 显示删除账户弹窗
 * 需要验证码和密码双重确�?
 */
function showDeleteAccountModal() {
  // 获取弹窗相关元素
  const modal = document.getElementById('delete-account-modal');
  const codeInput = document.getElementById('delete-code-input');
  const passwordInput = document.getElementById('delete-password-input');
  const sendCodeBtn = document.getElementById('delete-send-code-btn');
  const confirmBtn = document.getElementById('delete-confirm-btn');
  const cancelBtn = document.getElementById('delete-cancel-btn');
  const codeError = document.getElementById('delete-code-error');
  const passwordError = document.getElementById('delete-password-error');
  const turnstileContainer = document.getElementById('delete-turnstile-container');
  
  if (!modal) return;
  
  // 先清理可能残留的 Turnstile
  clearTurnstile('delete-turnstile-container');
  turnstileContainer.classList.add('is-hidden');
  
  // 重置弹窗状�?
  codeInput.value = '';
  passwordInput.value = '';
  codeInput.classList.remove('is-error');
  passwordInput.classList.remove('is-error');
  codeError.classList.add('is-hidden');
  passwordError.classList.add('is-hidden');
  confirmBtn.disabled = true;
  sendCodeBtn.disabled = false;
  sendCodeBtn.textContent = t('dashboard.sendCode');
  
  let countdownTimer = null; // 倒计时定时器
  let codeSent = false; // 验证码是否已发�?
  let isCleanedUp = false; // 防止重复清理
  
  /**
   * 更新确认按钮状�?
   * 只有验证码已发送且输入框都有值时才启�?
   */
  function updateConfirmState() {
    const hasCode = codeInput.value.trim().length > 0;
    const hasPassword = passwordInput.value.length > 0;
    confirmBtn.disabled = !(codeSent && hasCode && hasPassword);
  }
  
  codeInput.addEventListener('input', updateConfirmState);
  passwordInput.addEventListener('input', updateConfirmState);

  /**
   * 发送删除账户验证码
   */
  async function handleSendCode() {
    // 如果弹窗已关闭，不执�?
    if (isCleanedUp) return;
    
    try {
      const response = await fetch('/api/auth/send-delete-code', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ turnstileToken: getTurnstileToken(), language: document.documentElement.lang || 'zh-CN' })
      });
      const result = await response.json();
      
      // 再次检查弹窗是否已关闭
      if (isCleanedUp) return;
      
      if (result.success) {
        codeSent = true;
        showAlert(t('dashboard.codeSent'));
        startCountdown();
      } else {
        // 根据错误码显示对应错误信�?
        sendCodeBtn.disabled = false;
        if (result.errorCode === 'TURNSTILE_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else if (result.errorCode === 'RATE_LIMIT') {
          showAlert(t('error.rateLimitExceeded') || '请求过于频繁，请稍后再试');
        } else {
          showAlert(t('dashboard.sendCodeFailed') || '发送验证码失败，请稍后再试');
        }
      }
    } catch (e) {
      if (isCleanedUp) return;
      sendCodeBtn.disabled = false;
      showAlert(t('error.networkError'));
    }
    
    clearTurnstile('delete-turnstile-container');
  }
  
  /**
   * 开始发送按钮倒计�?
   */
  function startCountdown() {
    let seconds = 60;
    sendCodeBtn.disabled = true;
    sendCodeBtn.textContent = `${seconds}s`;
    
    countdownTimer = setInterval(() => {
      seconds--;
      if (seconds <= 0) {
        clearInterval(countdownTimer);
        sendCodeBtn.disabled = false;
        sendCodeBtn.textContent = t('dashboard.sendCode');
      } else {
        sendCodeBtn.textContent = `${seconds}s`;
      }
    }, 1000);
  }
  
  /**
   * 发送验证码按钮点击处理
   */
  const handleSendCodeClick = async () => {
    sendCodeBtn.disabled = true;
    codeError.classList.add('is-hidden');
    
    // 无需人机验证时直接发�?
    if (!getTurnstileSiteKey()) {
      await handleSendCode();
    } else {
      // 显示人机验证
      turnstileContainer.classList.remove('is-hidden');
      await initTurnstile(
        async () => {
          // 如果弹窗已关闭，不执�?
          if (isCleanedUp) return;
          // 验证成功，隐藏验证容器并发送验证码
          turnstileContainer.classList.add('is-hidden');
          await handleSendCode();
        },
        () => {
          if (isCleanedUp) return;
          showAlert(t('register.humanVerifyFailed'));
          sendCodeBtn.disabled = false;
          clearTurnstile('delete-turnstile-container');
        },
        () => {
          if (isCleanedUp) return;
          sendCodeBtn.disabled = false;
          clearTurnstile('delete-turnstile-container');
        },
        'delete-turnstile-container'
      );
    }
  };
  
  /**
   * 确认删除账户
   */
  const handleConfirm = async () => {
    const code = codeInput.value.trim();
    const password = passwordInput.value;
    
    // 验证输入
    if (!code) {
      codeError.textContent = t('dashboard.codeRequired');
      codeError.classList.remove('is-hidden');
      codeInput.classList.add('is-error');
      return;
    }
    
    if (!password) {
      passwordError.textContent = t('dashboard.passwordRequired');
      passwordError.classList.remove('is-hidden');
      passwordInput.classList.add('is-error');
      return;
    }
    
    confirmBtn.disabled = true;
    
    try {
      const response = await fetch('/api/auth/delete-account', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ code, password })
      });
      const result = await response.json();
      
      if (result.success) {
        showAlert(t('dashboard.deleteSuccess'));
        // 删除成功后跳转到登录�?
        setTimeout(() => {
          window.location.href = '/account/login';
        }, 1500);
      } else {
        // 根据错误码显示对应错误信�?
        if (result.errorCode === 'INVALID_CODE' || result.errorCode === 'CODE_EXPIRED') {
          codeError.textContent = t(result.errorCode === 'CODE_EXPIRED' ? 'dashboard.codeExpired' : 'dashboard.invalidCode');
          codeError.classList.remove('is-hidden');
          codeInput.classList.add('is-error');
        } else if (result.errorCode === 'WRONG_PASSWORD') {
          passwordError.textContent = t('dashboard.wrongPassword');
          passwordError.classList.remove('is-hidden');
          passwordInput.classList.add('is-error');
        } else {
          showAlert(t('dashboard.deleteFailed'));
        }
        confirmBtn.disabled = false;
      }
    } catch (e) {
      showAlert(t('error.networkError'));
      confirmBtn.disabled = false;
    }
  };

  /**
   * 清理弹窗状态和事件监听
   */
  const cleanup = () => {
    if (isCleanedUp) return; // 防止重复清理
    isCleanedUp = true;
    
    modal.classList.add('is-hidden');
    if (countdownTimer) {
      clearInterval(countdownTimer);
      countdownTimer = null;
    }
    clearTurnstile('delete-turnstile-container');
    turnstileContainer.classList.add('is-hidden');
    sendCodeBtn.removeEventListener('click', handleSendCodeClick);
    confirmBtn.removeEventListener('click', handleConfirm);
    cancelBtn.removeEventListener('click', cleanup);
    modal.removeEventListener('click', handleOverlayClick);
    codeInput.removeEventListener('input', updateConfirmState);
    passwordInput.removeEventListener('input', updateConfirmState);
  };
  
  // 点击遮罩层关�?
  const handleOverlayClick = (e) => { if (e.target === modal) cleanup(); };
  
  // 绑定事件
  sendCodeBtn.addEventListener('click', handleSendCodeClick);
  confirmBtn.addEventListener('click', handleConfirm);
  cancelBtn.addEventListener('click', cleanup);
  modal.addEventListener('click', handleOverlayClick);
  
  // 显示弹窗
  modal.classList.remove('is-hidden');
}


// ==================== 修改密码弹窗 ====================

/**
 * 显示修改密码弹窗
 * 需要验证当前密码，新密码需满足强度要求，点击确认后触发人机验证
 */
function showChangePasswordModal() {
  // 获取弹窗相关元素
  const modal = document.getElementById('change-password-modal');
  const currentPasswordInput = document.getElementById('current-password-input');
  const newPasswordInput = document.getElementById('new-password-input');
  const confirmPasswordInput = document.getElementById('confirm-new-password-input');
  const confirmBtn = document.getElementById('change-password-confirm-btn');
  const cancelBtn = document.getElementById('change-password-cancel-btn');
  const currentPasswordError = document.getElementById('current-password-error');
  const confirmPasswordError = document.getElementById('confirm-password-error');
  const turnstileContainer = document.getElementById('change-password-turnstile-container');
  
  // 密码强度指示�?
  const reqLength = document.getElementById('pwd-req-length');
  const reqNumber = document.getElementById('pwd-req-number');
  const reqSpecial = document.getElementById('pwd-req-special');
  const reqCase = document.getElementById('pwd-req-case');
  
  if (!modal) return;
  
  // 先清理可能残留的 Turnstile
  clearTurnstile('change-password-turnstile-container');
  turnstileContainer.classList.add('is-hidden');
  
  // 重置弹窗状�?
  currentPasswordInput.value = '';
  newPasswordInput.value = '';
  confirmPasswordInput.value = '';
  currentPasswordInput.classList.remove('is-error');
  confirmPasswordInput.classList.remove('is-error');
  currentPasswordError.classList.add('is-hidden');
  confirmPasswordError.classList.add('is-hidden');
  confirmBtn.disabled = true;
  
  // 重置密码强度指示�?
  reqLength?.classList.remove('is-valid');
  reqNumber?.classList.remove('is-valid');
  reqSpecial?.classList.remove('is-valid');
  reqCase?.classList.remove('is-valid');
  
  let isCleanedUp = false; // 防止重复清理
  
  /**
   * 更新密码强度指示�?
   * @param {string} password - 密码
   */
  function updatePasswordRequirements(password) {
    const hasLength = password.length >= 16 && password.length <= 64;
    const hasNumber = /\d/.test(password);
    const hasSpecial = /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/.test(password);
    const hasCase = /[a-z]/.test(password) && /[A-Z]/.test(password);
    
    reqLength?.classList.toggle('is-valid', hasLength);
    reqNumber?.classList.toggle('is-valid', hasNumber);
    reqSpecial?.classList.toggle('is-valid', hasSpecial);
    reqCase?.classList.toggle('is-valid', hasCase);
    
    return hasLength && hasNumber && hasSpecial && hasCase;
  }
  
  /**
   * 更新确认按钮状�?
   */
  function updateConfirmState() {
    const hasCurrent = currentPasswordInput.value.length > 0;
    const hasNew = newPasswordInput.value.length > 0;
    const hasConfirm = confirmPasswordInput.value.length > 0;
    const isPasswordValid = updatePasswordRequirements(newPasswordInput.value);
    
    confirmBtn.disabled = !(hasCurrent && hasNew && hasConfirm && isPasswordValid);
  }
  
  // 监听输入
  const handleCurrentInput = () => {
    currentPasswordError.classList.add('is-hidden');
    currentPasswordInput.classList.remove('is-error');
    updateConfirmState();
  };
  
  const handleNewInput = () => {
    updateConfirmState();
  };
  
  const handleConfirmInput = () => {
    confirmPasswordError.classList.add('is-hidden');
    confirmPasswordInput.classList.remove('is-error');
    updateConfirmState();
  };
  
  currentPasswordInput.addEventListener('input', handleCurrentInput);
  newPasswordInput.addEventListener('input', handleNewInput);
  confirmPasswordInput.addEventListener('input', handleConfirmInput);
  
  /**
   * 执行修改密码请求
   */
  async function doChangePassword() {
    if (isCleanedUp) return;
    
    const currentPassword = currentPasswordInput.value;
    const newPassword = newPasswordInput.value;
    
    try {
      const response = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ currentPassword, newPassword, turnstileToken: getTurnstileToken() })
      });
      const result = await response.json();
      
      if (isCleanedUp) return;
      
      if (result.success) {
        cleanup();
        showAlert(t('dashboard.changePasswordSuccess'));
      } else {
        // 根据错误码显示对应错误信�?
        if (result.errorCode === 'WRONG_PASSWORD') {
          currentPasswordError.textContent = t('dashboard.wrongCurrentPassword');
          currentPasswordError.classList.remove('is-hidden');
          currentPasswordInput.classList.add('is-error');
        } else if (result.errorCode === 'SAME_PASSWORD') {
          confirmPasswordError.textContent = t('dashboard.samePassword');
          confirmPasswordError.classList.remove('is-hidden');
        } else if (result.errorCode === 'TURNSTILE_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else {
          showAlert(t('dashboard.changePasswordFailed'));
        }
        confirmBtn.disabled = false;
      }
    } catch (e) {
      if (isCleanedUp) return;
      showAlert(t('error.networkError'));
      confirmBtn.disabled = false;
    }
    
    clearTurnstile('change-password-turnstile-container');
    turnstileContainer.classList.add('is-hidden');
  }
  
  /**
   * 确认修改密码（点击确认按钮）
   */
  const handleConfirm = async () => {
    const newPassword = newPasswordInput.value;
    const confirmPassword = confirmPasswordInput.value;
    
    // 验证新密码强�?
    const passwordValidation = validatePassword(newPassword);
    if (!passwordValidation.valid) {
      showAlert(t(passwordValidation.errorKey));
      return;
    }
    
    // 验证确认密码
    if (newPassword !== confirmPassword) {
      confirmPasswordError.textContent = t('register.passwordMismatch');
      confirmPasswordError.classList.remove('is-hidden');
      confirmPasswordInput.classList.add('is-error');
      return;
    }
    
    confirmBtn.disabled = true;
    
    // 无需人机验证时直接提�?
    if (!getTurnstileSiteKey()) {
      await doChangePassword();
    } else {
      // 显示人机验证
      turnstileContainer.classList.remove('is-hidden');
      await initTurnstile(
        async () => {
          if (isCleanedUp) return;
          await doChangePassword();
        },
        () => {
          if (isCleanedUp) return;
          showAlert(t('register.humanVerifyFailed'));
          confirmBtn.disabled = false;
          clearTurnstile('change-password-turnstile-container');
          turnstileContainer.classList.add('is-hidden');
        },
        () => {
          if (isCleanedUp) return;
          confirmBtn.disabled = false;
          clearTurnstile('change-password-turnstile-container');
          turnstileContainer.classList.add('is-hidden');
        },
        'change-password-turnstile-container'
      );
    }
  };
  
  /**
   * 清理弹窗状态和事件监听
   */
  const cleanup = () => {
    if (isCleanedUp) return;
    isCleanedUp = true;
    
    modal.classList.add('is-hidden');
    clearTurnstile('change-password-turnstile-container');
    turnstileContainer.classList.add('is-hidden');
    currentPasswordInput.removeEventListener('input', handleCurrentInput);
    newPasswordInput.removeEventListener('input', handleNewInput);
    confirmPasswordInput.removeEventListener('input', handleConfirmInput);
    confirmBtn.removeEventListener('click', handleConfirm);
    cancelBtn.removeEventListener('click', cleanup);
    modal.removeEventListener('click', handleOverlayClick);
  };
  
  // 点击遮罩层关�?
  const handleOverlayClick = (e) => { if (e.target === modal) cleanup(); };
  
  // 绑定事件
  confirmBtn.addEventListener('click', handleConfirm);
  cancelBtn.addEventListener('click', cleanup);
  modal.addEventListener('click', handleOverlayClick);
  
  // 显示弹窗并聚焦输入框
  modal.classList.remove('is-hidden');
  currentPasswordInput.focus();
}

// ==================== 修改用户名弹�?====================

/**
 * 显示修改用户名弹�?
 * @param {Object} user - 用户信息对象
 * @param {Function} onSuccess - 用户名更新成功回�?
 */
function showChangeUsernameModal(user, onSuccess) {
  // 获取弹窗相关元素
  const modal = document.getElementById('change-username-modal');
  const usernameInput = document.getElementById('new-username-input');
  const usernameError = document.getElementById('username-error');
  const confirmBtn = document.getElementById('change-username-confirm-btn');
  const cancelBtn = document.getElementById('change-username-cancel-btn');
  const turnstileContainer = document.getElementById('change-username-turnstile-container');
  
  if (!modal) return;
  
  // 先清理可能残留的 Turnstile
  clearTurnstile('change-username-turnstile-container');
  turnstileContainer.classList.add('is-hidden');
  
  // 重置弹窗状�?
  usernameInput.value = user.username;
  usernameInput.classList.remove('is-error');
  usernameError.classList.add('is-hidden');
  confirmBtn.disabled = true;
  
  let isCleanedUp = false; // 防止重复清理
  
  /**
   * 更新确认按钮状�?
   */
  function updateConfirmState() {
    const newUsername = usernameInput.value.trim();
    const hasValue = newUsername.length > 0;
    const isChanged = newUsername !== user.username;
    const isValidLength = newUsername.length >= 1 && newUsername.length <= 15;
    
    confirmBtn.disabled = !(hasValue && isChanged && isValidLength);
  }
  
  // 监听输入
  const handleInput = () => {
    usernameError.classList.add('is-hidden');
    usernameInput.classList.remove('is-error');
    
    const username = usernameInput.value.trim();
    if (username.length > 15) {
      usernameError.textContent = t('register.usernameTooLong');
      usernameError.classList.remove('is-hidden');
      usernameInput.classList.add('is-error');
    }
    
    updateConfirmState();
  };
  
  usernameInput.addEventListener('input', handleInput);
  
  /**
   * 执行修改用户名请�?
   */
  async function doChangeUsername() {
    if (isCleanedUp) return;
    
    const newUsername = usernameInput.value.trim();
    
    try {
      const response = await fetch('/api/user/username', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ username: newUsername, turnstileToken: getTurnstileToken() })
      });
      const result = await response.json();
      
      if (isCleanedUp) return;
      
      if (result.success) {
        cleanup();
        onSuccess(result.username);
        showAlert(t('dashboard.usernameUpdateSuccess'));
      } else {
        // 根据错误码显示对应错误信�?
        if (result.errorCode === 'USERNAME_ALREADY_EXISTS') {
          usernameError.textContent = t('register.usernameExists');
          usernameError.classList.remove('is-hidden');
          usernameInput.classList.add('is-error');
        } else if (result.errorCode === 'USERNAME_TOO_LONG') {
          usernameError.textContent = t('register.usernameTooLong');
          usernameError.classList.remove('is-hidden');
          usernameInput.classList.add('is-error');
        } else if (result.errorCode === 'TURNSTILE_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else {
          showAlert(t('dashboard.usernameUpdateFailed'));
        }
        confirmBtn.disabled = false;
      }
    } catch (e) {
      if (isCleanedUp) return;
      showAlert(t('error.networkError'));
      confirmBtn.disabled = false;
    }
    
    clearTurnstile('change-username-turnstile-container');
    turnstileContainer.classList.add('is-hidden');
  }
  
  /**
   * 确认修改用户名（点击确认按钮�?
   */
  const handleConfirm = async () => {
    const newUsername = usernameInput.value.trim();
    
    // 验证用户名长�?
    if (newUsername.length < 1 || newUsername.length > 15) {
      usernameError.textContent = t('register.usernameLength');
      usernameError.classList.remove('is-hidden');
      usernameInput.classList.add('is-error');
      return;
    }
    
    confirmBtn.disabled = true;
    
    // 无需人机验证时直接提�?
    if (!getTurnstileSiteKey()) {
      await doChangeUsername();
    } else {
      // 显示人机验证
      turnstileContainer.classList.remove('is-hidden');
      await initTurnstile(
        async () => {
          if (isCleanedUp) return;
          await doChangeUsername();
        },
        () => {
          if (isCleanedUp) return;
          showAlert(t('register.humanVerifyFailed'));
          confirmBtn.disabled = false;
          clearTurnstile('change-username-turnstile-container');
          turnstileContainer.classList.add('is-hidden');
        },
        () => {
          if (isCleanedUp) return;
          confirmBtn.disabled = false;
          clearTurnstile('change-username-turnstile-container');
          turnstileContainer.classList.add('is-hidden');
        },
        'change-username-turnstile-container'
      );
    }
  };
  
  /**
   * 清理弹窗状态和事件监听
   */
  const cleanup = () => {
    if (isCleanedUp) return;
    isCleanedUp = true;
    
    modal.classList.add('is-hidden');
    clearTurnstile('change-username-turnstile-container');
    turnstileContainer.classList.add('is-hidden');
    usernameInput.removeEventListener('input', handleInput);
    confirmBtn.removeEventListener('click', handleConfirm);
    cancelBtn.removeEventListener('click', cleanup);
    modal.removeEventListener('click', handleOverlayClick);
  };
  
  // 点击遮罩层关�?
  const handleOverlayClick = (e) => { if (e.target === modal) cleanup(); };
  
  // 绑定事件
  confirmBtn.addEventListener('click', handleConfirm);
  cancelBtn.addEventListener('click', cleanup);
  modal.addEventListener('click', handleOverlayClick);
  
  // 显示弹窗并聚焦输入框
  modal.classList.remove('is-hidden');
  usernameInput.focus();
  usernameInput.select();
}


// ==================== 扫码登录弹窗 ====================

/**
 * 显示扫码登录弹窗
 * 优先使用 BarcodeDetector API，不支持时使�?jsQR �?
 * @param {Function} onClose - 弹窗关闭回调
 */
function showQrScanModal(onClose) {
  const modal = document.getElementById('qr-scan-modal');
  const video = document.getElementById('qr-scanner-video');
  const statusEl = document.getElementById('qr-scan-status');
  const closeBtn = document.getElementById('qr-scan-close-btn');
  
  if (!modal || !video) {
    if (onClose) onClose();
    return;
  }
  
  let stream = null;
  let animationId = null;
  let isCleanedUp = false;
  let detectionStarted = false;
  let canvas = null;
  let canvasCtx = null;
  
  // 检查浏览器是否支持 BarcodeDetector
  const hasBarcodeDetector = 'BarcodeDetector' in window;
  // 检查是否有 jsQR �?
  const hasJsQR = typeof jsQR === 'function';
  
  console.log('[QR-SCAN] BarcodeDetector:', hasBarcodeDetector, 'jsQR:', hasJsQR);
  
  /**
   * 更新状态文�?
   * @param {string} key - 翻译�?
   * @param {string} type - 状态类�?(normal/error/success)
   */
  function updateStatus(key, type = 'normal') {
    if (statusEl) {
      statusEl.textContent = t(key);
      statusEl.className = 'qr-scan-status';
      if (type === 'error') statusEl.classList.add('error');
      if (type === 'success') statusEl.classList.add('success');
    }
  }
  
  /**
   * 获取后置摄像头设�?ID
   * @returns {Promise<string|null>} 后置摄像�?deviceId
   */
  async function getBackCameraId() {
    try {
      const devices = await navigator.mediaDevices.enumerateDevices();
      const cameras = devices.filter(d => d.kind === 'videoinput');
      console.log('[QR-SCAN] Available cameras:', cameras.map(c => ({ id: c.deviceId.substring(0, 8), label: c.label })));
      
      // 查找后置摄像头（通常 label 包含 back、rear、environment 等关键词）
      const backCamera = cameras.find(c => {
        const label = c.label.toLowerCase();
        return label.includes('back') || label.includes('rear') || label.includes('environment');
      });
      
      if (backCamera) {
        console.log('[QR-SCAN] Found back camera:', backCamera.label);
        return backCamera.deviceId;
      }
      
      // 如果有多个摄像头，通常最后一个是后置
      if (cameras.length > 1) {
        console.log('[QR-SCAN] Using last camera as back camera');
        return cameras[cameras.length - 1].deviceId;
      }
      
      return null;
    } catch (e) {
      console.warn('[QR-SCAN] Cannot enumerate devices:', e);
      return null;
    }
  }
  
  /**
   * 启动摄像�?
   */
  async function startCamera() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      updateStatus('dashboard.scanQrNotSupported', 'error');
      return;
    }
    
    try {
      updateStatus('dashboard.scanQrRequesting');
      
      // 尝试获取后置摄像�?ID
      const backCameraId = await getBackCameraId();
      
      // 构建约束条件
      let constraints;
      if (backCameraId) {
        // 如果找到后置摄像头，直接使用 deviceId
        constraints = {
          video: {
            deviceId: { exact: backCameraId },
            width: { ideal: 1280 },
            height: { ideal: 720 }
          },
          audio: false
        };
      } else {
        // 否则使用 facingMode
        constraints = {
          video: {
            facingMode: { exact: 'environment' },
            width: { ideal: 1280 },
            height: { ideal: 720 }
          },
          audio: false
        };
      }
      
      try {
        stream = await navigator.mediaDevices.getUserMedia(constraints);
        console.log('[QR-SCAN] Got stream with preferred constraints');
      } catch (e) {
        console.warn('[QR-SCAN] Preferred constraints failed:', e.name, '- trying facingMode ideal');
        // 尝试 ideal 模式
        try {
          stream = await navigator.mediaDevices.getUserMedia({
            video: { facingMode: { ideal: 'environment' } },
            audio: false
          });
        } catch (e2) {
          console.warn('[QR-SCAN] facingMode ideal failed:', e2.name, '- trying any camera');
          // 最后尝试任意摄像头
          stream = await navigator.mediaDevices.getUserMedia({ video: true });
        }
      }
      
      // 设置 video 元素
      video.setAttribute('playsinline', 'true');
      video.setAttribute('autoplay', 'true');
      video.muted = true;
      video.srcObject = stream;
      
      // 等待视频加载
      await new Promise((resolve, reject) => {
        video.onloadedmetadata = () => {
          video.play().then(resolve).catch(reject);
        };
        setTimeout(() => reject(new Error('Video load timeout')), 5000);
      });
      
      console.log('[QR-SCAN] Camera started, dimensions:', video.videoWidth, 'x', video.videoHeight);
      
      if (video.videoWidth === 0 || video.videoHeight === 0) {
        throw new Error('Video dimensions are zero');
      }
      
      updateStatus('dashboard.scanQrHint');
      
      // 开始扫描（优先 BarcodeDetector，否则用 jsQR�?
      if (hasBarcodeDetector) {
        startBarcodeDetection();
      } else if (hasJsQR) {
        startJsQRDetection();
      } else {
        updateStatus('dashboard.scanQrNotSupported', 'error');
      }
    } catch (error) {
      console.error('[QR-SCAN] Camera error:', error.name, error.message);
      updateStatus('dashboard.scanQrCameraError', 'error');
    }
  }
  
  /**
   * 使用 BarcodeDetector API 扫描
   */
  async function startBarcodeDetection() {
    if (isCleanedUp || detectionStarted) return;
    detectionStarted = true;
    
    try {
      const barcodeDetector = new BarcodeDetector({ formats: ['qr_code'] });
      console.log('[QR-SCAN] Using BarcodeDetector');
      
      const detectFrame = async () => {
        if (isCleanedUp) return;
        
        if (!video.videoWidth || !video.videoHeight || video.paused || video.ended) {
          animationId = requestAnimationFrame(detectFrame);
          return;
        }
        
        try {
          const barcodes = await barcodeDetector.detect(video);
          if (barcodes.length > 0) {
            const qrData = barcodes[0].rawValue;
            console.log('[QR-SCAN] QR detected (BarcodeDetector)');
            handleQrCodeScanned(qrData);
            return;
          }
        } catch (e) {
          // 继续下一�?
        }
        
        animationId = requestAnimationFrame(detectFrame);
      };
      
      setTimeout(() => {
        if (!isCleanedUp) {
          animationId = requestAnimationFrame(detectFrame);
        }
      }, 500);
      
    } catch (error) {
      console.error('[QR-SCAN] BarcodeDetector failed:', error);
      // 回退�?jsQR
      if (hasJsQR) {
        detectionStarted = false;
        startJsQRDetection();
      } else {
        updateStatus('dashboard.scanQrNotSupported', 'error');
      }
    }
  }
  
  /**
   * 使用 jsQR 库扫描（兼容 Firefox 等不支持 BarcodeDetector 的浏览器�?
   */
  function startJsQRDetection() {
    if (isCleanedUp || detectionStarted) return;
    detectionStarted = true;
    
    console.log('[QR-SCAN] Using jsQR library');
    
    // 创建离屏 canvas 用于图像处理
    canvas = document.createElement('canvas');
    canvasCtx = canvas.getContext('2d', { willReadFrequently: true });
    
    const detectFrame = () => {
      if (isCleanedUp) return;
      
      if (!video.videoWidth || !video.videoHeight || video.paused || video.ended) {
        animationId = requestAnimationFrame(detectFrame);
        return;
      }
      
      // 设置 canvas 尺寸
      canvas.width = video.videoWidth;
      canvas.height = video.videoHeight;
      
      // 绘制视频帧到 canvas
      canvasCtx.drawImage(video, 0, 0, canvas.width, canvas.height);
      
      // 获取图像数据
      const imageData = canvasCtx.getImageData(0, 0, canvas.width, canvas.height);
      
      // 使用 jsQR 解码
      const code = jsQR(imageData.data, imageData.width, imageData.height, {
        inversionAttempts: 'dontInvert'
      });
      
      if (code) {
        console.log('[QR-SCAN] QR detected (jsQR)');
        handleQrCodeScanned(code.data);
        return;
      }
      
      animationId = requestAnimationFrame(detectFrame);
    };
    
    setTimeout(() => {
      if (!isCleanedUp) {
        animationId = requestAnimationFrame(detectFrame);
      }
    }, 500);
  }
  
  /**
   * 验证二维码数据格式是否合�?
   * 防止恶意二维码注入（XSS、URL 跳转等）
   * @param {string} data - 二维码内�?
   * @returns {boolean} 是否合法
   */
  function isValidQrToken(data) {
    // 1. 必须是字符串且非�?
    if (typeof data !== 'string' || !data.trim()) return false;
    
    // 2. 长度限制（AES-256-GCM 加密后的 token 格式：base64.base64.base64�?
    if (data.length < 50 || data.length > 500) return false;
    
    // 3. 必须符合加密 token 格式（三�?base64，用点分隔）
    const parts = data.split('.');
    if (parts.length !== 3) return false;
    
    // 4. 每段必须是有效的 base64 字符
    const base64Regex = /^[A-Za-z0-9+/=]+$/;
    for (const part of parts) {
      if (!part || !base64Regex.test(part)) return false;
    }
    
    // 5. 不能包含危险字符（防�?XSS�?
    const dangerousPatterns = [
      /<script/i, /<\/script/i, /javascript:/i, /data:/i,
      /on\w+=/i, /<iframe/i, /<img/i, /eval\(/i
    ];
    for (const pattern of dangerousPatterns) {
      if (pattern.test(data)) return false;
    }
    
    return true;
  }
  
  /**
   * 处理扫描到的二维�?
   * @param {string} data - 二维码内�?
   */
  async function handleQrCodeScanned(data) {
    if (isCleanedUp) return;
    
    // 安全验证：检查二维码数据格式
    if (!isValidQrToken(data)) {
      console.warn('[QR-SCAN] Invalid QR code format, rejected');
      updateStatus('dashboard.scanQrInvalid', 'error');
      setTimeout(() => cleanup(), 2000);
      return;
    }
    
    updateStatus('dashboard.scanQrProcessing', 'normal');
    console.log('[QR-SCAN] Scanned data:', data.substring(0, 50) + '...');
    
    try {
      // 1. 调用后端获取 PC 端信�?
      const scanResponse = await fetch('/api/qr-login/scan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token: data })
      });
      
      const scanResult = await scanResponse.json();
      
      if (!scanResult.success) {
        console.error('[QR-SCAN] Scan failed:', scanResult.errorCode);
        updateStatus('dashboard.scanQrInvalid', 'error');
        setTimeout(() => cleanup(), 2000);
        return;
      }
      
      // 2. 关闭扫码弹窗，显示确认弹�?
      cleanup();
      showQrLoginConfirmModal(data, scanResult.pcInfo);
      
    } catch (error) {
      console.error('[QR-SCAN] ERROR:', error);
      updateStatus('dashboard.scanQrFailed', 'error');
      setTimeout(() => cleanup(), 2000);
    }
  }
  
  /**
   * 停止摄像�?
   */
  function stopCamera() {
    console.log('[QR-SCAN] Stopping camera');
    
    // 停止动画�?
    if (animationId) {
      cancelAnimationFrame(animationId);
      animationId = null;
    }
    
    // 先暂停视�?
    if (video) {
      video.pause();
      video.srcObject = null;
      // 移除所有事件监�?
      video.onloadedmetadata = null;
      video.onerror = null;
    }
    
    // 停止所有轨�?
    if (stream) {
      const tracks = stream.getTracks();
      tracks.forEach(track => {
        track.stop();
        console.log('[QR-SCAN] Track stopped:', track.kind, track.readyState);
      });
      stream = null;
    }
    
    // 清理 canvas
    canvas = null;
    canvasCtx = null;
    detectionStarted = false;
  }
  
  /**
   * 清理弹窗
   */
  const cleanup = () => {
    if (isCleanedUp) return;
    isCleanedUp = true;
    console.log('[QR-SCAN] Cleanup started');
    
    stopCamera();
    modal.classList.add('is-hidden');
    closeBtn.removeEventListener('click', cleanup);
    modal.removeEventListener('click', handleOverlayClick);
    
    // 延迟调用关闭回调，确保资源完全释�?
    setTimeout(() => {
      if (onClose) onClose();
    }, 300);
  };
  
  const handleOverlayClick = (e) => { 
    if (e.target === modal) cleanup(); 
  };
  
  closeBtn.addEventListener('click', cleanup);
  modal.addEventListener('click', handleOverlayClick);
  
  modal.classList.remove('is-hidden');
  updateStatus('dashboard.scanQrHint');
  
  setTimeout(() => {
    if (!isCleanedUp) {
      startCamera();
    }
  }, 100);
}

// ==================== 扫码登录确认弹窗 ====================

/**
 * 显示扫码登录确认弹窗
 * @param {string} token - 加密后的 token
 * @param {Object} pcInfo - PC 端信�?
 */
function showQrLoginConfirmModal(token, pcInfo) {
  const modal = document.getElementById('qr-login-confirm-modal');
  const ipEl = document.getElementById('pc-info-ip');
  const browserEl = document.getElementById('pc-info-browser');
  const osEl = document.getElementById('pc-info-os');
  const confirmBtn = document.getElementById('qr-login-confirm-btn');
  const cancelBtn = document.getElementById('qr-login-cancel-btn');
  
  if (!modal) return;
  
  // 显示 PC 端信�?
  if (ipEl) ipEl.textContent = pcInfo.ip || '-';
  if (browserEl) browserEl.textContent = pcInfo.browser || '-';
  if (osEl) osEl.textContent = pcInfo.os || '-';
  
  // 重置按钮状�?
  if (confirmBtn) confirmBtn.disabled = false;
  if (cancelBtn) cancelBtn.disabled = false;
  
  /**
   * 确认登录
   */
  const handleConfirm = async () => {
    if (confirmBtn) confirmBtn.disabled = true;
    
    try {
      const response = await fetch('/api/qr-login/mobile-confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token })
      });
      
      const result = await response.json();
      
      if (result.success) {
        cleanup();
        showAlert(t('dashboard.qrLoginSuccess'));
      } else {
        if (confirmBtn) confirmBtn.disabled = false;
        showAlert(t('dashboard.qrLoginFailed'));
      }
    } catch (error) {
      console.error('[QR-LOGIN] ERROR:', error);
      if (confirmBtn) confirmBtn.disabled = false;
      showAlert(t('error.networkError'));
    }
  };
  
  /**
   * 取消登录
   */
  const handleCancel = async () => {
    if (cancelBtn) cancelBtn.disabled = true;
    
    try {
      // 通知后端取消登录
      await fetch('/api/qr-login/mobile-cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token })
      });
    } catch (error) {
      console.error('[QR-LOGIN] Cancel error:', error);
    }
    
    cleanup();
  };
  
  /**
   * 清理弹窗
   */
  const cleanup = () => {
    modal.classList.add('is-hidden');
    confirmBtn?.removeEventListener('click', handleConfirm);
    cancelBtn?.removeEventListener('click', handleCancel);
    modal.removeEventListener('click', handleOverlayClick);
  };
  
  const handleOverlayClick = (e) => {
    if (e.target === modal) handleCancel();
  };
  
  // 绑定事件
  confirmBtn?.addEventListener('click', handleConfirm);
  cancelBtn?.addEventListener('click', handleCancel);
  modal.addEventListener('click', handleOverlayClick);
  
  // 显示弹窗
  modal.classList.remove('is-hidden');
}
