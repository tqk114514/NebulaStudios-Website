/**
 * assets/js/oauth.ts
 * OAuth 授权页面逻辑
 *
 * 功能：
 * - 显示第三方应用请求的权限
 * - 用户授权或拒绝
 * - 提交授权决定到后端
 * - 错误通过弹窗提示
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';
import { showAlert as showAlertBase } from './lib/ui/feedback.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { getUrlParameter } from './lib/utils/url.ts';

// 翻译函数
const t = window.t || ((key: string): string => key);

// ==================== 类型定义 ====================

interface AuthorizeInfoResponse {
  success: boolean;
  errorCode?: string;
  data?: {
    clientName: string;
    clientDescription: string;
    scopes: string[];
    username: string;
    userAvatar: string;
  };
}

// Scope 图标映射
const scopeIcons: Record<string, string> = {
  openid: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z"/></svg>',
  profile: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>',
  email: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z"/></svg>'
};

// 错误消息映射
const errorMessages: Record<string, string> = {
  'invalid_request': 'oauth.error.invalidRequest',
  'invalid_client': 'oauth.error.invalidClient',
  'invalid_scope': 'oauth.error.invalidScope',
  'access_denied': 'oauth.error.accessDenied',
  'server_error': 'oauth.error.serverError',
  'unsupported_response_type': 'oauth.error.unsupportedResponseType',
  'unauthorized': 'oauth.error.unauthorized'
};

// ==================== 弹窗封装 ====================

/**
 * 显示提示弹窗
 */
function showAlert(message: string): void {
  showAlertBase(message, '', t);
}

/**
 * 显示错误弹窗
 */
function showError(errorCode: string): void {
  const messageKey = errorMessages[errorCode] || 'oauth.error.unknown';
  showAlert(t(messageKey));
}

// ==================== 辅助函数 ====================

/**
 * 渲染权限列表
 */
function renderScopes(scopes: string[]): void {
  const scopeList = document.getElementById('scope-list');
  if (!scopeList) return;

  scopeList.innerHTML = '';

  for (const scope of scopes) {
    const li = document.createElement('li');
    li.className = 'oauth-scope-item';

    const icon = scopeIcons[scope] || scopeIcons.openid;
    const scopeName = t(`oauth.scope.${scope}.name`);
    const scopeDesc = t(`oauth.scope.${scope}.desc`);

    li.innerHTML = `
      <div class="oauth-scope-icon">${icon}</div>
      <div class="oauth-scope-text">
        <div class="oauth-scope-name">${scopeName}</div>
        <div class="oauth-scope-desc">${scopeDesc}</div>
      </div>
    `;

    scopeList.appendChild(li);
  }
}

/**
 * 设置用户头像
 */
function setUserAvatar(avatarUrl: string, username: string): void {
  const avatarEl = document.getElementById('user-avatar');
  if (!avatarEl) return;

  if (avatarUrl) {
    const img = document.createElement('img');
    img.src = avatarUrl;
    img.alt = username;
    avatarEl.textContent = '';
    avatarEl.appendChild(img);
  } else if (username) {
    avatarEl.textContent = username.charAt(0).toUpperCase();
  }
}

/**
 * 提交授权决定
 */
async function submitDecision(decision: 'approve' | 'deny'): Promise<void> {
  const clientId = getUrlParameter('client_id');
  const redirectUri = getUrlParameter('redirect_uri');
  const scope = getUrlParameter('scope');
  const state = getUrlParameter('state');

  // 创建表单并提交
  const form = document.createElement('form');
  form.method = 'POST';
  form.action = '/oauth/authorize';

  const fields = {
    client_id: clientId,
    redirect_uri: redirectUri,
    scope: scope,
    state: state,
    decision: decision
  };

  for (const [name, value] of Object.entries(fields)) {
    if (value) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.appendChild(input);
    }
  }

  document.body.appendChild(form);
  form.submit();
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
      if (card) { delayedExecution(() => adjustCardHeight(card)); }
    });

    // 启用卡片自动调整大小
    if (card) { enableCardAutoResize(card); }

    // 更新页面标题
    updatePageTitle();

    // 检查 URL 中是否有错误参数（从后端重定向过来的错误）
    const urlError = getUrlParameter('error');
    if (urlError) {
      showError(urlError);
      return;
    }

    // 获取 URL 参数
    const clientId = getUrlParameter('client_id');
    const redirectUri = getUrlParameter('redirect_uri');
    const scope = getUrlParameter('scope');

    // 验证必需参数
    if (!clientId || !redirectUri || !scope) {
      showError('invalid_request');
      return;
    }

    // 获取授权信息
    const appNameEl = document.getElementById('app-name');
    const appDescEl = document.getElementById('app-desc');
    const userNameEl = document.getElementById('user-name');

    try {
      const params = new URLSearchParams({
        client_id: clientId,
        redirect_uri: redirectUri,
        scope: scope
      });

      const response = await fetch(`/oauth/authorize/info?${params.toString()}`, {
        credentials: 'include'
      });

      const result: AuthorizeInfoResponse = await response.json();

      if (!result.success) {
        showError(result.errorCode || 'unknown_error');
        return;
      }

      const { clientName, clientDescription, scopes, username, userAvatar } = result.data!;

      // 显示应用信息
      if (appNameEl) appNameEl.textContent = clientName;
      if (appDescEl) appDescEl.textContent = clientDescription || '';
      if (userNameEl) userNameEl.textContent = username;

      // 设置用户头像
      setUserAvatar(userAvatar, username);

      // 渲染权限列表
      renderScopes(scopes);

    } catch {
      showAlert(t('error.networkError'));
      return;
    }

    // ==================== 按钮事件 ====================

    const authorizeBtn = document.getElementById('authorize-btn') as HTMLButtonElement | null;
    const denyBtn = document.getElementById('deny-btn') as HTMLButtonElement | null;

    // 授权
    if (authorizeBtn) {
      authorizeBtn.addEventListener('click', async () => {
        authorizeBtn.disabled = true;
        if (denyBtn) denyBtn.disabled = true;
        await submitDecision('approve');
      });
    }

    // 拒绝
    if (denyBtn) {
      denyBtn.addEventListener('click', async () => {
        if (authorizeBtn) authorizeBtn.disabled = true;
        denyBtn.disabled = true;
        await submitDecision('deny');
      });
    }

  } catch (error) {
    console.error('[OAUTH] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
    showAlert(t('error.networkError'));
  }
});
