/**
 * assets/js/dashboard.ts
 * Dashboard 页面逻辑
 *
 * 功能：
 * - 用户信息展示
 * - 头像管理（更换头像、使用微软头像）
 * - 微软账户绑定/解绑
 * - 修改密码
 * - 删除账户（需验证码和密码确认）
 * - 登出
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';
import { verifySession, logout } from './lib/api/auth.ts';
import { loadCaptchaConfig, getCaptchaSiteKey, getCaptchaType, initCaptcha, clearCaptcha, getCaptchaToken } from './lib/captcha.ts';
import { showAlert as showAlertBase, showConfirm as showConfirmBase, createModalController } from './lib/ui/feedback.ts';
import { validateAvatarUrl, validatePassword } from './lib/validators.ts';
import { startCountdown, resumeCountdown, clearCountdown } from './lib/utils/countdown.ts';
import type { User, PcInfo } from '../../../../shared/js/types/auth.ts';

// 翻译函数（从全局获取，若不存在则返回原始 key）
const t = window.t || ((key: string): string => key);

// ==================== 设备检测 ====================

/**
 * 检测是否为移动设备
 */
function isMobileDevice(): boolean {
  const userAgent = navigator.userAgent.toLowerCase();
  const mobileKeywords = [
    'android', 'webos', 'iphone', 'ipad', 'ipod', 'blackberry',
    'windows phone', 'opera mini', 'iemobile', 'mobile'
  ];
  return mobileKeywords.some(keyword => userAgent.includes(keyword));
}

// ==================== 弹窗封装 ====================

/**
 * 显示提示弹窗（封装，自动传入翻译函数）
 */
function showAlert(message: string): void {
  showAlertBase(message, '', t);
}

/**
 * 显示确认弹窗（封装，自动传入翻译函数）
 */
function showConfirm(message: string, title: string | null = null): Promise<boolean> {
  return showConfirmBase(message, title, t);
}

// ==================== 头像相关 ====================

/**
 * 更新头像显示
 */
function updateAvatarDisplay(avatarEl: HTMLElement | null, avatarUrl: string | null, username: string): void {
  if (!avatarEl) {return;}
  avatarEl.innerHTML = '';

  if (avatarUrl) {
    // 有头像 URL，显示图片
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
 * 显示更改头像弹窗
 */
function showAvatarModal(user: User, onSuccess: (newAvatarUrl: string) => void): void {
  // 获取弹窗相关元素
  const currentPreview = document.getElementById('current-avatar-preview');
  const newPreview = document.getElementById('new-avatar-preview');
  const urlInput = document.getElementById('avatar-url-input') as HTMLInputElement | null;
  const errorEl = document.getElementById('avatar-error');
  const microsoftBtn = document.getElementById('use-microsoft-avatar-btn');
  const confirmBtn = document.getElementById('avatar-confirm-btn') as HTMLButtonElement | null;

  if (!urlInput || !confirmBtn || !currentPreview || !newPreview || !errorEl) {return;}

  let validatedUrl: string | null = null; // 已验证的头像 URL

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'avatar-modal',
    confirmBtnId: 'avatar-confirm-btn',
    cancelBtnId: 'avatar-cancel-btn',
    onCleanup: () => {
      urlInput.removeEventListener('blur', handleBlur);
      urlInput.removeEventListener('focus', handleFocus);
      microsoftBtn?.removeEventListener('click', handleMicrosoftClick);
    }
  });

  if (!controller.modal) {return;}

  // 重置弹窗状态
  urlInput.value = '';
  urlInput.readOnly = false;
  urlInput.classList.remove('is-error', 'readonly-placeholder');
  errorEl.classList.add('is-hidden');
  errorEl.textContent = '';
  confirmBtn.disabled = true;
  newPreview.innerHTML = '';
  newPreview.classList.remove('is-loaded');

  // 显示当前头像预览
  currentPreview.innerHTML = '';
  // 如果是 "microsoft"，用实际的微软头像 URL
  const currentAvatarUrl = user.avatar_url === 'microsoft' ? user.microsoft_avatar_url : user.avatar_url;
  if (currentAvatarUrl) {
    const img = document.createElement('img');
    img.src = currentAvatarUrl;
    img.alt = user.username;
    currentPreview.appendChild(img);
  } else if (user.username) {
    currentPreview.textContent = user.username.charAt(0).toUpperCase();
  }

  // 微软头像按钮（只有绑定了微软账户且有头像时才显示）
  const hasMicrosoftAvatar = user.microsoft_avatar_url && user.microsoft_avatar_url.trim();
  if (hasMicrosoftAvatar) {
    microsoftBtn?.classList.remove('is-hidden');
  } else {
    microsoftBtn?.classList.add('is-hidden');
  }

  /**
   * 加载新头像预览
   */
  function loadNewAvatar(url: string): void {
    newPreview!.innerHTML = '';
    newPreview!.classList.remove('is-loaded');
    errorEl!.classList.add('is-hidden');
    urlInput!.classList.remove('is-error');
    confirmBtn!.disabled = true;
    validatedUrl = null;

    if (!url || !url.trim()) {return;}

    // 验证 URL 格式（前端验证）
    const urlValidation = validateAvatarUrl(url);
    if (!urlValidation.valid) {
      errorEl!.textContent = t(urlValidation.errorKey || 'dashboard.invalidUrl');
      errorEl!.classList.remove('is-hidden');
      urlInput!.classList.add('is-error');
      return;
    }

    // 尝试加载图片验证可用性
    const img = document.createElement('img');
    img.onload = (): void => {
      newPreview!.innerHTML = '';
      newPreview!.appendChild(img);
      newPreview!.classList.add('is-loaded');
      confirmBtn!.disabled = false;
      validatedUrl = url;
    };
    img.onerror = (): void => {
      errorEl!.textContent = t('dashboard.avatarLoadFailed');
      errorEl!.classList.remove('is-hidden');
      urlInput!.classList.add('is-error');
    };
    img.src = url;
  }

  // 输入框失焦时加载预览
  const handleBlur = (): void => {
    if (urlInput!.readOnly) {return;}
    loadNewAvatar(urlInput!.value.trim());
  };

  // 输入框获得焦点时，如果是微软头像占位符则清除
  const handleFocus = (): void => {
    if (urlInput!.readOnly && urlInput!.value === '[Microsoft Avatar]') {
      urlInput!.value = '';
      urlInput!.readOnly = false;
      urlInput!.classList.remove('readonly-placeholder');
      newPreview!.innerHTML = '';
      newPreview!.classList.remove('is-loaded');
      confirmBtn!.disabled = true;
      validatedUrl = null;
    }
  };

  // 使用微软头像按钮点击
  const handleMicrosoftClick = (): void => {
    const msAvatarUrl = user.microsoft_avatar_url;
    if (!msAvatarUrl) {return;}
    
    // 显示占位符，实际发送 "microsoft" 给后端
    urlInput!.value = '[Microsoft Avatar]';
    urlInput!.readOnly = true;
    urlInput!.classList.add('readonly-placeholder');
    
    // 预览使用实际 URL
    newPreview!.innerHTML = '';
    newPreview!.classList.remove('is-loaded');
    errorEl!.classList.add('is-hidden');
    urlInput!.classList.remove('is-error');
    
    const img = document.createElement('img');
    img.onload = (): void => {
      newPreview!.innerHTML = '';
      newPreview!.appendChild(img);
      newPreview!.classList.add('is-loaded');
      confirmBtn!.disabled = false;
      validatedUrl = 'microsoft'; // 发送 "microsoft" 而不是完整 URL
    };
    img.onerror = (): void => {
      errorEl!.textContent = t('dashboard.avatarLoadFailed');
      errorEl!.classList.remove('is-hidden');
      urlInput!.classList.add('is-error');
    };
    img.src = msAvatarUrl;
  };

  // 确认更换头像
  controller.onConfirm(async () => {
    if (!validatedUrl || controller.isCleanedUp()) {return;}

    confirmBtn!.disabled = true;
    try {
      const response = await fetch('/api/user/avatar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ avatar_url: validatedUrl })
      });
      const result = await response.json();

      if (result.success) {
        controller.close();
        onSuccess(result.avatar_url);
        showAlert(t('dashboard.avatarUpdateSuccess'));
      } else {
        const errorMessages: Record<string, string> = {
          'INVALID_IMAGE_URL': 'dashboard.invalidImageUrl',
          'INVALID_URL': 'dashboard.invalidUrl',
          'URL_TOO_LONG': 'dashboard.invalidUrl'
        };
        const errorKey = errorMessages[result.errorCode] || 'dashboard.avatarUpdateFailed';
        errorEl!.textContent = t(errorKey);
        errorEl!.classList.remove('is-hidden');
        confirmBtn!.disabled = false;
      }
    } catch {
      errorEl!.textContent = t('dashboard.avatarUpdateFailed');
      errorEl!.classList.remove('is-hidden');
      confirmBtn!.disabled = false;
    }
  });

  // 绑定额外事件
  urlInput.addEventListener('blur', handleBlur);
  urlInput.addEventListener('focus', handleFocus);
  microsoftBtn?.addEventListener('click', handleMicrosoftClick);

  // 显示弹窗并聚焦输入框
  controller.open();
  urlInput.focus();
}

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();

    // 加载验证码配置
    await loadCaptchaConfig();

    // 验证用户会话
    const sessionResult = await verifySession();

    if (!sessionResult.success) {
      console.warn('[DASHBOARD] WARN: Session invalid:', sessionResult.errorCode);
      window.location.href = '/account/login';
      return;
    }

    // 隐藏页面加载遮罩
    hidePageLoader();

    const user = sessionResult.data as unknown as User;

    // 检查 URL 参数（处理绑定结果提示）
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

    // 头部欢迎区元素
    const usernameEl = document.getElementById('display-username');
    const emailEl = document.getElementById('display-email');
    const avatarEl = document.getElementById('user-avatar');

    // 显示用户名
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

    if (infoUsername) {infoUsername.textContent = user.username;}
    if (infoEmail) {infoEmail.textContent = user.email;}

    const microsoftLinkItem = document.getElementById('microsoft-link-item');

    /**
     * 更新微软账户绑定状态显示
     */
    function updateMicrosoftStatus(isLinked: boolean, microsoftName: string | null): void {
      if (infoMicrosoft) {
        if (isLinked && microsoftName) {
          // 已绑定且有名称
          infoMicrosoft.textContent = microsoftName;
          infoMicrosoft.classList.add('is-linked');
          infoMicrosoft.classList.remove('is-not-linked');
          infoMicrosoft.removeAttribute('data-i18n');
        } else if (isLinked) {
          // 已绑定但无名称
          infoMicrosoft.textContent = t('dashboard.linked');
          infoMicrosoft.classList.add('is-linked');
          infoMicrosoft.classList.remove('is-not-linked');
          infoMicrosoft.removeAttribute('data-i18n');
        } else {
          // 未绑定
          infoMicrosoft.textContent = t('dashboard.notLinked');
          infoMicrosoft.classList.remove('is-linked');
          infoMicrosoft.classList.add('is-not-linked');
        }
      }
    }

    // 初始化微软账户状态
    updateMicrosoftStatus(!!user.microsoft_id, user.microsoft_name || null);

    // ==================== 微软账户绑定/解绑 ====================

    if (microsoftLinkItem) {
      microsoftLinkItem.addEventListener('click', async () => {
        if (user.microsoft_id) {
          // 解绑流程
          const confirmed = await showConfirm(t('dashboard.confirmUnlink'), t('dashboard.unlinkThirdParty'));
          if (!confirmed) {return;}

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
          } catch {
            showAlert(t('dashboard.unlinkFailed'));
          }
        } else {
          // 绑定流程
          const confirmed = await showConfirm(t('dashboard.confirmLink'), t('dashboard.linkThirdParty'));
          if (!confirmed) {return;}
          window.location.href = '/api/auth/microsoft?action=link';
        }
      });
    }

    // 更新页面标题
    updatePageTitle();

    const dashboardMain = document.querySelector('.dashboard-main');

    /**
     * 调整信息列表高度（带动画效果）
     */
    function adjustInfoListHeight(): void {
      document.querySelectorAll('.info-list').forEach(list => {
        const listEl = list as HTMLElement;
        const currentHeight = listEl.offsetHeight;
        listEl.style.height = 'auto';
        const targetHeight = listEl.scrollHeight;
        listEl.style.height = `${currentHeight}px`;
        listEl.offsetHeight; // 强制重绘
        listEl.style.height = `${targetHeight}px`;
      });
    }

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      updatePageTitle();
      // 语言切换后重新应用微软账户状态和按钮文本
      updateMicrosoftStatus(!!user.microsoft_id, user.microsoft_name || null);
      // 触发高度过渡动画
      requestAnimationFrame(() => requestAnimationFrame(adjustInfoListHeight));
    });

    // ==================== 功能按钮事件 ====================

    // 登出按钮
    const logoutBtn = document.getElementById('logout-btn');
    if (logoutBtn) {
      logoutBtn.addEventListener('click', async () => {
        const confirmed = await showConfirm(t('dashboard.confirmLogout'), t('dashboard.logout'));
        if (confirmed) {logout();}
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
          // 更新用户数据和页面显示
          user.avatar_url = newAvatarUrl;
          // 如果是 "microsoft"，用实际的微软头像 URL 显示
          const displayUrl = newAvatarUrl === 'microsoft' ? user.microsoft_avatar_url : newAvatarUrl;
          updateAvatarDisplay(avatarEl, displayUrl || null, user.username);
        });
      });
    }

    // 修改用户名
    const changeUsernameItem = document.getElementById('change-username-item');
    if (changeUsernameItem) {
      changeUsernameItem.addEventListener('click', () => {
        showChangeUsernameModal(user, (newUsername) => {
          // 更新用户数据和页面显示
          user.username = newUsername;
          if (usernameEl) {usernameEl.textContent = newUsername;}
          if (infoUsername) {infoUsername.textContent = newUsername;}
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
            // 弹窗关闭回调，重置状态
            isScanning = false;
          });
        });
      }
    }

    /**
     * 检测内容是否溢出，决定是否允许滚动
     */
    function checkOverflow(): void {
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
    console.error('[DASHBOARD] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
  }
});

// ==================== 删除账户弹窗 ====================

/**
 * 显示删除账户弹窗
 * 需要验证码和密码双重确认
 */
function showDeleteAccountModal(): void {
  // 获取弹窗相关元素
  const codeInput = document.getElementById('delete-code-input') as HTMLInputElement | null;
  const passwordInput = document.getElementById('delete-password-input') as HTMLInputElement | null;
  const sendCodeBtn = document.getElementById('delete-send-code-btn') as HTMLButtonElement | null;
  const confirmBtn = document.getElementById('delete-confirm-btn') as HTMLButtonElement | null;
  const codeError = document.getElementById('delete-code-error');
  const passwordError = document.getElementById('delete-password-error');
  const captchaContainer = document.getElementById('delete-captcha-container');

  if (!codeInput || !passwordInput || !sendCodeBtn || !confirmBtn || !codeError || !passwordError || !captchaContainer) {return;}

  const DELETE_COUNTDOWN_KEY = 'delete_countdown_end';
  let codeSent = false;

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'delete-account-modal',
    confirmBtnId: 'delete-confirm-btn',
    cancelBtnId: 'delete-cancel-btn',
    onCleanup: () => {
      clearCountdown(DELETE_COUNTDOWN_KEY);
      clearCaptcha('delete-captcha-container');
      captchaContainer.classList.add('is-hidden');
      sendCodeBtn.removeEventListener('click', handleSendCodeClick);
      codeInput.removeEventListener('input', updateConfirmState);
      passwordInput.removeEventListener('input', updateConfirmState);
    }
  });

  if (!controller.modal) {return;}

  // 先清理可能残留的验证组件
  clearCaptcha('delete-captcha-container');
  captchaContainer.classList.add('is-hidden');

  // 重置弹窗状态
  codeInput.value = '';
  passwordInput.value = '';
  codeInput.classList.remove('is-error');
  passwordInput.classList.remove('is-error');
  codeError.classList.add('is-hidden');
  passwordError.classList.add('is-hidden');
  confirmBtn.disabled = true;
  sendCodeBtn.disabled = false;
  sendCodeBtn.textContent = t('dashboard.sendCode');

  // 尝试从 cookie 恢复倒计时状态（用户可能刷新了页面）
  const resumed = resumeCountdown(sendCodeBtn, {
    cookieKey: DELETE_COUNTDOWN_KEY,
    completeText: t('dashboard.sendCode')
  });
  if (resumed) {
    codeSent = true;
  }

  /**
   * 更新确认按钮状态
   */
  function updateConfirmState(): void {
    const hasCode = codeInput!.value.trim().length > 0;
    const hasPassword = passwordInput!.value.length > 0;
    confirmBtn!.disabled = !(codeSent && hasCode && hasPassword);
  }

  codeInput.addEventListener('input', updateConfirmState);
  passwordInput.addEventListener('input', updateConfirmState);

  /**
   * 发送删除账户验证码
   */
  async function handleSendCode(): Promise<void> {
    if (controller.isCleanedUp()) {return;}

    try {
      const response = await fetch('/api/auth/send-delete-code', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          captchaToken: getCaptchaToken(),
          captchaType: getCaptchaType(),
          language: document.documentElement.lang || 'zh-CN'
        })
      });
      const result = await response.json();

      if (controller.isCleanedUp()) {return;}

      if (result.success) {
        codeSent = true;
        showAlert(t('dashboard.codeSent'));
        startCountdown(sendCodeBtn!, {
          seconds: 60,
          cookieKey: DELETE_COUNTDOWN_KEY,
          completeText: t('dashboard.sendCode')
        });
      } else {
        sendCodeBtn!.disabled = false;
        if (result.errorCode === 'CAPTCHA_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else if (result.errorCode === 'RATE_LIMIT') {
          showAlert(t('error.rateLimitExceeded') || '请求过于频繁，请稍后再试');
        } else {
          showAlert(t('dashboard.sendCodeFailed') || '发送验证码失败，请稍后再试');
        }
      }
    } catch {
      if (controller.isCleanedUp()) {return;}
      sendCodeBtn!.disabled = false;
      showAlert(t('error.networkError'));
    }

    clearCaptcha('delete-captcha-container');
  }

  /**
   * 发送验证码按钮点击处理
   */
  const handleSendCodeClick = async (): Promise<void> => {
    sendCodeBtn!.disabled = true;
    codeError!.classList.add('is-hidden');

    if (!getCaptchaSiteKey()) {
      await handleSendCode();
    } else {
      captchaContainer!.classList.remove('is-hidden');
      await initCaptcha(
        async () => {
          if (controller.isCleanedUp()) {return;}
          captchaContainer!.classList.add('is-hidden');
          await handleSendCode();
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          showAlert(t('register.humanVerifyFailed'));
          sendCodeBtn!.disabled = false;
          clearCaptcha('delete-captcha-container');
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          sendCodeBtn!.disabled = false;
          clearCaptcha('delete-captcha-container');
        },
        'delete-captcha-container'
      );
    }
  };

  // 确认删除账户
  controller.onConfirm(async () => {
    if (controller.isCleanedUp()) {return;}

    const code = codeInput!.value.trim();
    const password = passwordInput!.value;

    if (!code) {
      codeError!.textContent = t('dashboard.codeRequired');
      codeError!.classList.remove('is-hidden');
      codeInput!.classList.add('is-error');
      return;
    }

    if (!password) {
      passwordError!.textContent = t('dashboard.passwordRequired');
      passwordError!.classList.remove('is-hidden');
      passwordInput!.classList.add('is-error');
      return;
    }

    confirmBtn!.disabled = true;

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
        setTimeout(() => {
          window.location.href = '/account/login';
        }, 1500);
      } else {
        if (result.errorCode === 'INVALID_CODE' || result.errorCode === 'CODE_EXPIRED') {
          codeError!.textContent = t(result.errorCode === 'CODE_EXPIRED' ? 'dashboard.codeExpired' : 'dashboard.invalidCode');
          codeError!.classList.remove('is-hidden');
          codeInput!.classList.add('is-error');
        } else if (result.errorCode === 'WRONG_PASSWORD') {
          passwordError!.textContent = t('dashboard.wrongPassword');
          passwordError!.classList.remove('is-hidden');
          passwordInput!.classList.add('is-error');
        } else {
          showAlert(t('dashboard.deleteFailed'));
        }
        confirmBtn!.disabled = false;
      }
    } catch {
      showAlert(t('error.networkError'));
      confirmBtn!.disabled = false;
    }
  });

  // 绑定发送验证码按钮事件
  sendCodeBtn.addEventListener('click', handleSendCodeClick);

  // 显示弹窗
  controller.open();
}

// ==================== 修改密码弹窗 ====================

/**
 * 显示修改密码弹窗
 * 需要验证当前密码，新密码需满足强度要求，点击确认后触发人机验证
 */
function showChangePasswordModal(): void {
  // 获取弹窗相关元素
  const currentPasswordInput = document.getElementById('current-password-input') as HTMLInputElement | null;
  const newPasswordInput = document.getElementById('new-password-input') as HTMLInputElement | null;
  const confirmPasswordInput = document.getElementById('confirm-new-password-input') as HTMLInputElement | null;
  const confirmBtn = document.getElementById('change-password-confirm-btn') as HTMLButtonElement | null;
  const currentPasswordError = document.getElementById('current-password-error');
  const confirmPasswordError = document.getElementById('confirm-password-error');
  const captchaContainer = document.getElementById('change-password-captcha-container');

  // 密码强度指示器
  const reqLength = document.getElementById('pwd-req-length');
  const reqNumber = document.getElementById('pwd-req-number');
  const reqSpecial = document.getElementById('pwd-req-special');
  const reqCase = document.getElementById('pwd-req-case');

  if (!currentPasswordInput || !newPasswordInput || !confirmPasswordInput || !confirmBtn || !currentPasswordError || !confirmPasswordError || !captchaContainer) {return;}

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'change-password-modal',
    confirmBtnId: 'change-password-confirm-btn',
    cancelBtnId: 'change-password-cancel-btn',
    onCleanup: () => {
      clearCaptcha('change-password-captcha-container');
      captchaContainer.classList.add('is-hidden');
      currentPasswordInput.removeEventListener('input', handleCurrentInput);
      newPasswordInput.removeEventListener('input', handleNewInput);
      confirmPasswordInput.removeEventListener('input', handleConfirmInput);
    }
  });

  if (!controller.modal) {return;}

  // 先清理可能残留的验证组件
  clearCaptcha('change-password-captcha-container');
  captchaContainer.classList.add('is-hidden');

  // 重置弹窗状态
  currentPasswordInput.value = '';
  newPasswordInput.value = '';
  confirmPasswordInput.value = '';
  currentPasswordInput.classList.remove('is-error');
  confirmPasswordInput.classList.remove('is-error');
  currentPasswordError.classList.add('is-hidden');
  confirmPasswordError.classList.add('is-hidden');
  confirmBtn.disabled = true;

  // 重置密码强度指示器
  reqLength?.classList.remove('is-valid');
  reqNumber?.classList.remove('is-valid');
  reqSpecial?.classList.remove('is-valid');
  reqCase?.classList.remove('is-valid');

  /**
   * 更新密码强度指示器
   */
  function updatePasswordRequirements(password: string): boolean {
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
   * 更新确认按钮状态
   */
  function updateConfirmState(): void {
    const hasCurrent = currentPasswordInput!.value.length > 0;
    const hasNew = newPasswordInput!.value.length > 0;
    const hasConfirm = confirmPasswordInput!.value.length > 0;
    const isPasswordValid = updatePasswordRequirements(newPasswordInput!.value);

    confirmBtn!.disabled = !(hasCurrent && hasNew && hasConfirm && isPasswordValid);
  }

  // 监听输入
  const handleCurrentInput = (): void => {
    currentPasswordError!.classList.add('is-hidden');
    currentPasswordInput!.classList.remove('is-error');
    updateConfirmState();
  };

  const handleNewInput = (): void => {
    updateConfirmState();
  };

  const handleConfirmInput = (): void => {
    confirmPasswordError!.classList.add('is-hidden');
    confirmPasswordInput!.classList.remove('is-error');
    updateConfirmState();
  };

  currentPasswordInput.addEventListener('input', handleCurrentInput);
  newPasswordInput.addEventListener('input', handleNewInput);
  confirmPasswordInput.addEventListener('input', handleConfirmInput);

  /**
   * 执行修改密码请求
   */
  async function doChangePassword(): Promise<void> {
    if (controller.isCleanedUp()) {return;}

    const currentPassword = currentPasswordInput!.value;
    const newPassword = newPasswordInput!.value;

    try {
      const response = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          currentPassword,
          newPassword,
          captchaToken: getCaptchaToken(),
          captchaType: getCaptchaType()
        })
      });
      const result = await response.json();

      if (controller.isCleanedUp()) {return;}

      if (result.success) {
        controller.close();
        showAlert(t('dashboard.changePasswordSuccess'));
      } else {
        if (result.errorCode === 'WRONG_PASSWORD') {
          currentPasswordError!.textContent = t('dashboard.wrongPassword');
          currentPasswordError!.classList.remove('is-hidden');
          currentPasswordInput!.classList.add('is-error');
        } else if (result.errorCode === 'SAME_PASSWORD') {
          confirmPasswordError!.textContent = t('dashboard.samePassword');
          confirmPasswordError!.classList.remove('is-hidden');
        } else if (result.errorCode === 'CAPTCHA_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else {
          showAlert(t('dashboard.changePasswordFailed'));
        }
        confirmBtn!.disabled = false;
      }
    } catch {
      if (controller.isCleanedUp()) {return;}
      showAlert(t('error.networkError'));
      confirmBtn!.disabled = false;
    }

    clearCaptcha('change-password-captcha-container');
    captchaContainer!.classList.add('is-hidden');
  }

  // 确认修改密码
  controller.onConfirm(async () => {
    if (controller.isCleanedUp()) {return;}

    const newPassword = newPasswordInput!.value;
    const confirmPassword = confirmPasswordInput!.value;

    // 验证新密码强度
    const passwordValidation = validatePassword(newPassword);
    if (!passwordValidation.valid) {
      showAlert(t(passwordValidation.errorKey || 'register.passwordInvalid'));
      return;
    }

    // 验证确认密码
    if (newPassword !== confirmPassword) {
      confirmPasswordError!.textContent = t('register.passwordMismatch');
      confirmPasswordError!.classList.remove('is-hidden');
      confirmPasswordInput!.classList.add('is-error');
      return;
    }

    confirmBtn!.disabled = true;

    if (!getCaptchaSiteKey()) {
      await doChangePassword();
    } else {
      captchaContainer!.classList.remove('is-hidden');
      await initCaptcha(
        async () => {
          if (controller.isCleanedUp()) {return;}
          await doChangePassword();
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          showAlert(t('register.humanVerifyFailed'));
          confirmBtn!.disabled = false;
          clearCaptcha('change-password-captcha-container');
          captchaContainer!.classList.add('is-hidden');
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          confirmBtn!.disabled = false;
          clearCaptcha('change-password-captcha-container');
          captchaContainer!.classList.add('is-hidden');
        },
        'change-password-captcha-container'
      );
    }
  });

  // 显示弹窗并聚焦输入框
  controller.open();
  currentPasswordInput.focus();
}

// ==================== 修改用户名弹窗 ====================

/**
 * 显示修改用户名弹窗
 */
function showChangeUsernameModal(user: User, onSuccess: (newUsername: string) => void): void {
  // 获取弹窗相关元素
  const usernameInput = document.getElementById('new-username-input') as HTMLInputElement | null;
  const usernameError = document.getElementById('username-error');
  const confirmBtn = document.getElementById('change-username-confirm-btn') as HTMLButtonElement | null;
  const captchaContainer = document.getElementById('change-username-captcha-container');

  if (!usernameInput || !usernameError || !confirmBtn || !captchaContainer) {return;}

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'change-username-modal',
    confirmBtnId: 'change-username-confirm-btn',
    cancelBtnId: 'change-username-cancel-btn',
    onCleanup: () => {
      clearCaptcha('change-username-captcha-container');
      captchaContainer.classList.add('is-hidden');
      usernameInput.removeEventListener('input', handleInput);
    }
  });

  if (!controller.modal) {return;}

  // 先清理可能残留的验证组件
  clearCaptcha('change-username-captcha-container');
  captchaContainer.classList.add('is-hidden');

  // 重置弹窗状态
  usernameInput.value = user.username;
  usernameInput.classList.remove('is-error');
  usernameError.classList.add('is-hidden');
  confirmBtn.disabled = true;

  /**
   * 更新确认按钮状态
   */
  function updateConfirmState(): void {
    const newUsername = usernameInput!.value.trim();
    const hasValue = newUsername.length > 0;
    const isChanged = newUsername !== user.username;
    const isValidLength = newUsername.length >= 1 && newUsername.length <= 15;

    confirmBtn!.disabled = !(hasValue && isChanged && isValidLength);
  }

  // 监听输入
  const handleInput = (): void => {
    usernameError!.classList.add('is-hidden');
    usernameInput!.classList.remove('is-error');

    const username = usernameInput!.value.trim();
    if (username.length > 15) {
      usernameError!.textContent = t('register.usernameTooLong');
      usernameError!.classList.remove('is-hidden');
      usernameInput!.classList.add('is-error');
    }

    updateConfirmState();
  };

  usernameInput.addEventListener('input', handleInput);

  /**
   * 执行修改用户名请求
   */
  async function doChangeUsername(): Promise<void> {
    if (controller.isCleanedUp()) {return;}

    const newUsername = usernameInput!.value.trim();

    try {
      const response = await fetch('/api/user/username', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          username: newUsername,
          captchaToken: getCaptchaToken(),
          captchaType: getCaptchaType()
        })
      });
      const result = await response.json();

      if (controller.isCleanedUp()) {return;}

      if (result.success) {
        controller.close();
        onSuccess(result.username);
        showAlert(t('dashboard.usernameUpdateSuccess'));
      } else {
        if (result.errorCode === 'USERNAME_ALREADY_EXISTS') {
          usernameError!.textContent = t('register.usernameExists');
          usernameError!.classList.remove('is-hidden');
          usernameInput!.classList.add('is-error');
        } else if (result.errorCode === 'USERNAME_TOO_LONG') {
          usernameError!.textContent = t('register.usernameTooLong');
          usernameError!.classList.remove('is-hidden');
          usernameInput!.classList.add('is-error');
        } else if (result.errorCode === 'CAPTCHA_FAILED') {
          showAlert(t('register.humanVerifyFailed'));
        } else {
          showAlert(t('dashboard.usernameUpdateFailed'));
        }
        confirmBtn!.disabled = false;
      }
    } catch {
      if (controller.isCleanedUp()) {return;}
      showAlert(t('error.networkError'));
      confirmBtn!.disabled = false;
    }

    clearCaptcha('change-username-captcha-container');
    captchaContainer!.classList.add('is-hidden');
  }

  // 确认修改用户名
  controller.onConfirm(async () => {
    if (controller.isCleanedUp()) {return;}

    const newUsername = usernameInput!.value.trim();

    // 验证用户名长度
    if (newUsername.length < 1 || newUsername.length > 15) {
      usernameError!.textContent = t('register.usernameLength');
      usernameError!.classList.remove('is-hidden');
      usernameInput!.classList.add('is-error');
      return;
    }

    confirmBtn!.disabled = true;

    if (!getCaptchaSiteKey()) {
      await doChangeUsername();
    } else {
      captchaContainer!.classList.remove('is-hidden');
      await initCaptcha(
        async () => {
          if (controller.isCleanedUp()) {return;}
          await doChangeUsername();
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          showAlert(t('register.humanVerifyFailed'));
          confirmBtn!.disabled = false;
          clearCaptcha('change-username-captcha-container');
          captchaContainer!.classList.add('is-hidden');
        },
        () => {
          if (controller.isCleanedUp()) {return;}
          confirmBtn!.disabled = false;
          clearCaptcha('change-username-captcha-container');
          captchaContainer!.classList.add('is-hidden');
        },
        'change-username-captcha-container'
      );
    }
  });

  // 显示弹窗并聚焦输入框
  controller.open();
  usernameInput.focus();
  usernameInput.select();
}

// ==================== 扫码登录弹窗 ====================

/**
 * 显示扫码登录弹窗
 * 优先使用 BarcodeDetector API，不支持时使用 jsQR 库
 */
function showQrScanModal(onClose: () => void): void {
  const video = document.getElementById('qr-scanner-video') as HTMLVideoElement | null;
  const statusEl = document.getElementById('qr-scan-status');

  if (!video) {
    if (onClose) {onClose();}
    return;
  }

  let stream: MediaStream | null = null;
  let animationId: number | null = null;
  let detectionStarted = false;
  let canvas: HTMLCanvasElement | null = null;
  let canvasCtx: CanvasRenderingContext2D | null = null;

  // 检查浏览器是否支持 BarcodeDetector
  const hasBarcodeDetector = 'BarcodeDetector' in window;
  const hasJsQR = typeof (window as unknown as { jsQR?: unknown }).jsQR === 'function';

  console.log('[QR-SCAN] BarcodeDetector:', hasBarcodeDetector, 'jsQR:', hasJsQR);

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'qr-scan-modal',
    cancelBtnId: 'qr-scan-close-btn',
    onCleanup: () => {
      stopCamera();
      setTimeout(() => {
        if (onClose) {onClose();}
      }, 300);
    }
  });

  if (!controller.modal) {
    if (onClose) {onClose();}
    return;
  }

  /**
   * 更新状态文本
   */
  function updateStatus(key: string, type: 'normal' | 'error' | 'success' = 'normal'): void {
    if (statusEl) {
      statusEl.textContent = t(key);
      statusEl.className = 'qr-scan-status';
      if (type === 'error') {statusEl.classList.add('error');}
      if (type === 'success') {statusEl.classList.add('success');}
    }
  }

  /**
   * 获取后置摄像头设备 ID
   */
  async function getBackCameraId(): Promise<string | null> {
    try {
      const devices = await navigator.mediaDevices.enumerateDevices();
      const cameras = devices.filter(d => d.kind === 'videoinput');
      console.log('[QR-SCAN] Available cameras:', cameras.map(c => ({ id: c.deviceId.substring(0, 8), label: c.label })));

      const backCamera = cameras.find(c => {
        const label = c.label.toLowerCase();
        return label.includes('back') || label.includes('rear') || label.includes('environment');
      });

      if (backCamera) {
        console.log('[QR-SCAN] Found back camera:', backCamera.label);
        return backCamera.deviceId;
      }

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
   * 启动摄像头
   */
  async function startCamera(): Promise<void> {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      updateStatus('dashboard.scanQrNotSupported', 'error');
      return;
    }

    try {
      updateStatus('dashboard.scanQrRequesting');

      const backCameraId = await getBackCameraId();

      let constraints: MediaStreamConstraints;
      if (backCameraId) {
        constraints = {
          video: { deviceId: { exact: backCameraId }, width: { ideal: 1280 }, height: { ideal: 720 } },
          audio: false
        };
      } else {
        constraints = {
          video: { facingMode: { exact: 'environment' }, width: { ideal: 1280 }, height: { ideal: 720 } },
          audio: false
        };
      }

      try {
        stream = await navigator.mediaDevices.getUserMedia(constraints);
        console.log('[QR-SCAN] Got stream with preferred constraints');
      } catch (e) {
        console.warn('[QR-SCAN] Preferred constraints failed:', (e as Error).name, '- trying facingMode ideal');
        try {
          stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: 'environment' } }, audio: false });
        } catch (e2) {
          console.warn('[QR-SCAN] facingMode ideal failed:', (e2 as Error).name, '- trying any camera');
          stream = await navigator.mediaDevices.getUserMedia({ video: true });
        }
      }

      video!.setAttribute('playsinline', 'true');
      video!.setAttribute('autoplay', 'true');
      video!.muted = true;
      video!.srcObject = stream;

      await new Promise<void>((resolve, reject) => {
        video!.onloadedmetadata = (): void => {
          video!.play().then(resolve).catch(reject);
        };
        setTimeout(() => reject(new Error('Video load timeout')), 5000);
      });

      console.log('[QR-SCAN] Camera started, dimensions:', video!.videoWidth, 'x', video!.videoHeight);

      if (video!.videoWidth === 0 || video!.videoHeight === 0) {
        throw new Error('Video dimensions are zero');
      }

      updateStatus('dashboard.scanQrHint');

      if (hasBarcodeDetector) {
        startBarcodeDetection();
      } else if (hasJsQR) {
        startJsQRDetection();
      } else {
        updateStatus('dashboard.scanQrNotSupported', 'error');
      }
    } catch (error) {
      console.error('[QR-SCAN] Camera error:', (error as Error).name, (error as Error).message);
      updateStatus('dashboard.scanQrCameraError', 'error');
    }
  }

  /**
   * 使用 BarcodeDetector API 扫描
   */
  async function startBarcodeDetection(): Promise<void> {
    if (controller.isCleanedUp() || detectionStarted) {return;}
    detectionStarted = true;

    try {
      const barcodeDetector = new (window as unknown as { BarcodeDetector: new (options: { formats: string[] }) => { detect: (source: HTMLVideoElement) => Promise<Array<{ rawValue: string }>> } }).BarcodeDetector({ formats: ['qr_code'] });
      console.log('[QR-SCAN] Using BarcodeDetector');

      const detectFrame = async (): Promise<void> => {
        if (controller.isCleanedUp()) {return;}

        if (!video!.videoWidth || !video!.videoHeight || video!.paused || video!.ended) {
          animationId = requestAnimationFrame(detectFrame);
          return;
        }

        try {
          const barcodes = await barcodeDetector.detect(video!);
          if (barcodes.length > 0) {
            const qrData = barcodes[0].rawValue;
            console.log('[QR-SCAN] QR detected (BarcodeDetector)');
            handleQrCodeScanned(qrData);
            return;
          }
        } catch {
          // 继续下一帧
        }

        animationId = requestAnimationFrame(detectFrame);
      };

      setTimeout(() => {
        if (!controller.isCleanedUp()) {
          animationId = requestAnimationFrame(detectFrame);
        }
      }, 500);

    } catch (error) {
      console.error('[QR-SCAN] BarcodeDetector failed:', error);
      if (hasJsQR) {
        detectionStarted = false;
        startJsQRDetection();
      } else {
        updateStatus('dashboard.scanQrNotSupported', 'error');
      }
    }
  }

  /**
   * 使用 jsQR 库扫描
   */
  function startJsQRDetection(): void {
    if (controller.isCleanedUp() || detectionStarted) {return;}
    detectionStarted = true;

    console.log('[QR-SCAN] Using jsQR library');

    canvas = document.createElement('canvas');
    canvasCtx = canvas.getContext('2d', { willReadFrequently: true });

    const detectFrame = (): void => {
      if (controller.isCleanedUp()) {return;}

      if (!video!.videoWidth || !video!.videoHeight || video!.paused || video!.ended) {
        animationId = requestAnimationFrame(detectFrame);
        return;
      }

      canvas!.width = video!.videoWidth;
      canvas!.height = video!.videoHeight;
      canvasCtx!.drawImage(video!, 0, 0, canvas!.width, canvas!.height);

      const imageData = canvasCtx!.getImageData(0, 0, canvas!.width, canvas!.height);
      const jsQRFunc = (window as unknown as { jsQR: (data: Uint8ClampedArray, width: number, height: number, options?: { inversionAttempts?: string }) => { data: string } | null }).jsQR;
      const code = jsQRFunc(imageData.data, imageData.width, imageData.height, { inversionAttempts: 'dontInvert' });

      if (code) {
        console.log('[QR-SCAN] QR detected (jsQR)');
        handleQrCodeScanned(code.data);
        return;
      }

      animationId = requestAnimationFrame(detectFrame);
    };

    setTimeout(() => {
      if (!controller.isCleanedUp()) {
        animationId = requestAnimationFrame(detectFrame);
      }
    }, 500);
  }

  /**
   * 验证二维码数据格式是否合法
   */
  function isValidQrToken(data: string): boolean {
    if (typeof data !== 'string' || !data.trim()) {return false;}
    if (data.length < 50 || data.length > 500) {return false;}

    const parts = data.split('.');
    if (parts.length !== 3) {return false;}

    const base64Regex = /^[A-Za-z0-9+/=]+$/;
    for (const part of parts) {
      if (!part || !base64Regex.test(part)) {return false;}
    }

    const dangerousPatterns = [
      /<script/i, /<\/script/i, /javascript:/i, /data:/i,
      /on\w+=/i, /<iframe/i, /<img/i, /eval\(/i
    ];
    for (const pattern of dangerousPatterns) {
      if (pattern.test(data)) {return false;}
    }

    return true;
  }

  /**
   * 处理扫描到的二维码
   */
  async function handleQrCodeScanned(data: string): Promise<void> {
    if (controller.isCleanedUp()) {return;}

    if (!isValidQrToken(data)) {
      console.warn('[QR-SCAN] Invalid QR code format, rejected');
      updateStatus('dashboard.scanQrInvalid', 'error');
      setTimeout(() => controller.close(), 2000);
      return;
    }

    updateStatus('dashboard.scanQrProcessing', 'normal');
    console.log('[QR-SCAN] Scanned data:', data.substring(0, 50) + '...');

    try {
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
        setTimeout(() => controller.close(), 2000);
        return;
      }

      controller.close();
      showQrLoginConfirmModal(data, scanResult.pcInfo);

    } catch (error) {
      console.error('[QR-SCAN] ERROR:', error);
      updateStatus('dashboard.scanQrFailed', 'error');
      setTimeout(() => controller.close(), 2000);
    }
  }

  /**
   * 停止摄像头
   */
  function stopCamera(): void {
    console.log('[QR-SCAN] Stopping camera');

    if (animationId) {
      cancelAnimationFrame(animationId);
      animationId = null;
    }

    if (video) {
      video.pause();
      video.srcObject = null;
      video.onloadedmetadata = null;
      video.onerror = null;
    }

    if (stream) {
      const tracks = stream.getTracks();
      tracks.forEach(track => {
        track.stop();
        console.log('[QR-SCAN] Track stopped:', track.kind, track.readyState);
      });
      stream = null;
    }

    canvas = null;
    canvasCtx = null;
    detectionStarted = false;
  }

  // 显示弹窗
  controller.open();
  updateStatus('dashboard.scanQrHint');

  setTimeout(() => {
    if (!controller.isCleanedUp()) {
      startCamera();
    }
  }, 100);
}

// ==================== 扫码登录确认弹窗 ====================

/**
 * 显示扫码登录确认弹窗
 */
function showQrLoginConfirmModal(token: string, pcInfo: PcInfo): void {
  const ipEl = document.getElementById('pc-info-ip');
  const browserEl = document.getElementById('pc-info-browser');
  const osEl = document.getElementById('pc-info-os');
  const confirmBtn = document.getElementById('qr-login-confirm-btn') as HTMLButtonElement | null;
  const cancelBtn = document.getElementById('qr-login-cancel-btn') as HTMLButtonElement | null;

  // 创建弹窗控制器
  const controller = createModalController({
    modalId: 'qr-login-confirm-modal',
    confirmBtnId: 'qr-login-confirm-btn',
    cancelBtnId: 'qr-login-cancel-btn',
    closeOnOverlay: false  // 点击遮罩层不关闭，需要明确选择
  });

  if (!controller.modal) {return;}

  // 显示 PC 端信息
  if (ipEl) {ipEl.textContent = pcInfo.ip || '-';}
  if (browserEl) {browserEl.textContent = pcInfo.browser || '-';}
  if (osEl) {osEl.textContent = pcInfo.os || '-';}

  // 重置按钮状态
  if (confirmBtn) {confirmBtn.disabled = false;}
  if (cancelBtn) {cancelBtn.disabled = false;}

  // 确认登录
  controller.onConfirm(async () => {
    if (confirmBtn) {confirmBtn.disabled = true;}

    try {
      const response = await fetch('/api/qr-login/mobile-confirm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token })
      });

      const result = await response.json();

      if (result.success) {
        controller.close();
        showAlert(t('dashboard.qrLoginSuccess'));
      } else {
        if (confirmBtn) {confirmBtn.disabled = false;}
        showAlert(t('dashboard.qrLoginFailed'));
      }
    } catch (error) {
      console.error('[QR-LOGIN] ERROR:', error);
      if (confirmBtn) {confirmBtn.disabled = false;}
      showAlert(t('error.networkError'));
    }
  });

  // 取消登录
  controller.onCancel(async () => {
    if (cancelBtn) {cancelBtn.disabled = true;}

    try {
      await fetch('/api/qr-login/mobile-cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ token })
      });
    } catch (error) {
      console.error('[QR-LOGIN] Cancel error:', error);
    }
  });

  // 显示弹窗
  controller.open();
}
