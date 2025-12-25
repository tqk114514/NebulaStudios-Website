/**
 * assets/js/link-confirm.js
 * 微软账户绑定确认页面逻辑
 * 
 * 功能�?
 * - 显示待绑定的微软账户和已有账户信�?
 * - 用户确认后执行绑定操�?
 * - 取消则返回登录页
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, loadLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.js';
import { showAlert as showAlertBase } from './lib/ui-feedback.js';
import { getUrlParameter, adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/helpers.js';

// 翻译函数
const t = window.t || ((key) => key);

// ==================== 弹窗封装 ====================

/**
 * 显示提示弹窗
 * @param {string} message - 提示消息内容
 */
function showAlert(message) {
  showAlertBase(message, '', t);
}

// ==================== 页面初始�?====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();
    await loadLanguageSwitcher();
    
    // 隐藏页面加载遮罩
    hidePageLoader();
    
    // 获取卡片元素
    const card = document.querySelector('.card');
    
    // 初始化语言切换�?
    initLanguageSwitcher(() => {
      updatePageTitle();
      if (card) delayedExecution(() => adjustCardHeight(card));
    });
    
    // 启用卡片自动调整大小
    if (card) enableCardAutoResize(card);
    
    // 更新页面标题
    updatePageTitle();
    
    // 获取 URL 参数
    const token = getUrlParameter('token');
  
  // 如果缺少 token，显示错误并返回
  if (!token) {
    showAlert(t('linkConfirm.invalidLink'));
    setTimeout(() => {
      window.location.href = '/account/login';
    }, 2000);
    return;
  }
  
  // 获取待绑定信�?
  const microsoftNameEl = document.getElementById('microsoft-name');
  const microsoftAvatarEl = document.getElementById('microsoft-avatar');
  const userUsernameEl = document.getElementById('user-username');
  const userAvatarEl = document.getElementById('user-avatar');
  
  try {
    const response = await fetch(`/api/auth/microsoft/pending-link?token=${token}`);
    const result = await response.json();
    
    if (!result.success) {
      const errorMessages = {
        'INVALID_TOKEN': 'linkConfirm.invalidLink',
        'TOKEN_EXPIRED': 'linkConfirm.linkExpired'
      };
      showAlert(t(errorMessages[result.errorCode] || 'linkConfirm.linkFailed'));
      setTimeout(() => {
        window.location.href = '/account/login';
      }, 2000);
      return;
    }
    
    const { microsoftName, microsoftAvatar, username, userAvatar } = result.data;
    
    // 显示账户信息
    if (microsoftNameEl) microsoftNameEl.textContent = microsoftName || '-';
    if (userUsernameEl) userUsernameEl.textContent = username || '-';
    
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
  } catch (e) {
    showAlert(t('error.networkError'));
    setTimeout(() => {
      window.location.href = '/account/login';
    }, 2000);
    return;
  }
  
  // ==================== 按钮事件 ====================
  
  const confirmBtn = document.getElementById('confirm-btn');
  const cancelBtn = document.getElementById('cancel-btn');
  
  // 确认绑定
  if (confirmBtn) {
    confirmBtn.addEventListener('click', async () => {
      confirmBtn.disabled = true;
      
      try {
        const response = await fetch('/api/auth/microsoft/confirm-link', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({ token })
        });
        
        const result = await response.json();
        
        if (result.success) {
          // 绑定成功，跳转到 dashboard
          window.location.href = '/account/dashboard';
        } else {
          // 显示错误
          const errorMessages = {
            'INVALID_TOKEN': 'linkConfirm.invalidLink',
            'TOKEN_EXPIRED': 'linkConfirm.linkExpired',
            'MICROSOFT_ALREADY_LINKED': 'dashboard.microsoftAlreadyLinked',
            'USER_NOT_FOUND': 'error.sessionError'
          };
          const errorKey = errorMessages[result.errorCode] || 'linkConfirm.linkFailed';
          showAlert(t(errorKey));
          confirmBtn.disabled = false;
        }
      } catch (e) {
        showAlert(t('error.networkError'));
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
    console.error('[LINK-CONFIRM] ERROR: Page initialization failed:', error.message);
    hidePageLoader();
    showAlert(t('error.networkError'));
    setTimeout(() => {
      window.location.href = '/account/login';
    }, 2000);
  }
});
