/**
 * assets/js/link.ts
 * 微软账户绑定确认页面逻辑
 *
 * 功能：
 * - 显示待绑定的微软账户和已有账户信息
 * - 用户确认后执行绑定操作
 * - 取消则返回登录页
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';
import { showAlert as showAlertBase } from './lib/ui/feedback.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { fetchApi } from './lib/api/fetch.ts';

// 翻译函数（动态获取，确保 translations.js 加载后也能正确翻译）
const t = (key: string): string => window.t ? window.t(key) : key;

// ==================== 错误码映射 ====================

/**
 * 获取待绑定信息错误码映射
 */
const pendingLinkErrorMap: Record<string, string> = {
  'INVALID_TOKEN': 'linkConfirm.invalidLink',
  'TOKEN_EXPIRED': 'linkConfirm.linkExpired',
  'NETWORK_ERROR': 'error.networkError',
  'SERVER_ERROR': 'error.serverError'
};

/**
 * 确认绑定错误码映射
 */
const confirmLinkErrorMap: Record<string, string> = {
  'INVALID_TOKEN': 'linkConfirm.invalidLink',
  'TOKEN_EXPIRED': 'linkConfirm.linkExpired',
  'MICROSOFT_ALREADY_LINKED': 'dashboard.microsoftAlreadyLinked',
  'USER_NOT_FOUND': 'error.sessionError',
  'USER_BANNED': 'linkConfirm.userBanned',
  'NETWORK_ERROR': 'error.networkError',
  'SERVER_ERROR': 'error.serverError'
};

// ==================== 类型定义 =======================

interface PendingLinkData {
  microsoftName: string;
  microsoftAvatar?: string;
  username: string;
  userAvatar?: string;
}

// ==================== 弹窗封装 ====================

/**
 * 显示提示弹窗
 */
function showAlert(message: string): void {
  showAlertBase(message, '', t);
}

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();

    // 隐藏页面加载遮罩
    hidePageLoader();

    // 获取卡片元素
    const card = document.querySelector('.card') as HTMLElement | null;

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      updatePageTitle();
      if (card) {delayedExecution(() => adjustCardHeight(card));}
    });

    // 启用卡片自动调整大小
    if (card) {enableCardAutoResize(card);}

    // 更新页面标题
    updatePageTitle();

    // 获取待绑定信息
    const microsoftNameEl = document.getElementById('microsoft-name');
    const microsoftAvatarEl = document.getElementById('microsoft-avatar');
    const userUsernameEl = document.getElementById('user-username');
    const userAvatarEl = document.getElementById('user-avatar');

    try {
      const result = await fetchApi<{ data: PendingLinkData }>('/api/auth/microsoft/pending-link');

      if (!result.success) {
        showAlert(t(pendingLinkErrorMap[result.errorCode || ''] || 'linkConfirm.linkFailed'));
        setTimeout(() => {
          window.location.href = '/account/login';
        }, 2000);
        return;
      }

      if (!result.data) {
        showAlert(t('linkConfirm.linkFailed'));
        setTimeout(() => {
          window.location.href = '/account/login';
        }, 2000);
        return;
      }

      const { microsoftName, microsoftAvatar, username, userAvatar } = result.data;

      // 显示账户信息
      if (microsoftNameEl) {microsoftNameEl.textContent = microsoftName || '-';}
      if (userUsernameEl) {userUsernameEl.textContent = username || '-';}

      // 显示用户头像
      if (userAvatarEl) {
        if (userAvatar) {
          const img = document.createElement('img');
          img.src = userAvatar;
          img.alt = username;
          userAvatarEl.textContent = '';
          userAvatarEl.appendChild(img);
        } else if (username) {
          userAvatarEl.textContent = username.charAt(0).toUpperCase();
        }
      }

      // 显示微软账户头像
      if (microsoftAvatarEl) {
        if (microsoftAvatar) {
          const img = document.createElement('img');
          img.src = microsoftAvatar;
          img.alt = microsoftName;
          microsoftAvatarEl.textContent = '';
          microsoftAvatarEl.appendChild(img);
        } else if (microsoftName) {
          microsoftAvatarEl.textContent = microsoftName.charAt(0).toUpperCase();
        }
      }
    } catch {
      showAlert(t('error.networkError'));
      setTimeout(() => {
        window.location.href = '/account/login';
      }, 2000);
      return;
    }

    // ==================== 按钮事件 ====================

    const confirmBtn = document.getElementById('confirm-btn') as HTMLButtonElement | null;
    const cancelBtn = document.getElementById('cancel-btn');

    // 确认绑定
    if (confirmBtn) {
      confirmBtn.addEventListener('click', async () => {
        confirmBtn.disabled = true;

        const result = await fetchApi('/api/auth/microsoft/confirm-link', {
          method: 'POST'
        });

        if (result.success) {
          window.location.href = '/account/dashboard';
        } else {
          const errorKey = confirmLinkErrorMap[result.errorCode || ''] || 'linkConfirm.linkFailed';
          showAlert(t(errorKey));
          confirmBtn.disabled = false;
        }
      });
    }

    // 取消，返回登录页
    if (cancelBtn) {
      cancelBtn.addEventListener('click', () => {
        window.location.href = '/account/login';
      });
    }
  } catch (error) {
    console.error('[LINK-CONFIRM] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
    showAlert(t('error.networkError'));
    setTimeout(() => {
      window.location.href = '/account/login';
    }, 2000);
  }
});
