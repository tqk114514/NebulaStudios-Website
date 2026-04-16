/**
 * 登录页面逻辑
 *
 * 功能：
 * - 用户登录表单处理
 * - 人机验证（Turnstile/hCaptcha）
 * - OAuth 错误处理
 * - 会话检查（已登录自动跳转）
 */

import { initializeModals, showAlert } from './lib/ui/feedback.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { validateLoginForm } from './lib/validators.ts';
import { login, errorCodeMap } from './lib/api/auth.ts';
import { initLanguageSwitcher, waitForTranslations, updatePageTitle, hidePageLoader } from '../../../../shared/js/utils/language-switcher.ts';
import { loadCaptchaConfig, getCaptchaSiteKey, getCaptchaType, initCaptcha, clearCaptcha, getCaptchaToken } from './lib/captcha.ts';
import { initQrLogin } from './lib/qr.ts';

// ==================== 类型定义 ====================

interface PendingLoginData {
  email: string;
  password: string;
}

// ==================== 全局变量 ====================

const t = window.t || ((key: string): string => key);
const showAlertWithTranslation = (message: string, title?: string): void => showAlert(message, title || '', t);

/** 待处理的登录请求 */
let pendingLogin: PendingLoginData | null = null;

// ==================== 工具函数 ====================

/**
 * 重置验证码状态
 */
function resetCaptchaState(container: HTMLElement | null, card: HTMLElement | null, button: HTMLButtonElement | null): void {
  pendingLogin = null;
  clearCaptcha('captcha-container');
  
  if (button) {
    button.disabled = false;
    button.textContent = t('login.submitButton');
  }
  
  if (container) {
    container.classList.add('is-hidden');
    if (card) {
      delayedExecution(() => adjustCardHeight(card));
    }
  }
}

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译系统就绪
    await waitForTranslations();

    // 加载验证码配置
    await loadCaptchaConfig();
    hidePageLoader();

    // 初始化弹窗
    initializeModals(t);

    // 初始化扫码登录（非移动端显示）
    initQrLogin(document.getElementById('qr-login-btn'), {
      showAlert: showAlertWithTranslation,
      t
    });

    // 获取 DOM 元素
    const card = document.querySelector('.card') as HTMLElement | null;
    const loginEmailInput = document.getElementById('login-email') as HTMLInputElement | null;
    const loginPasswordInput = document.getElementById('login-password') as HTMLInputElement | null;
    const loginButton = document.querySelector('#login-form .button-primary') as HTMLButtonElement | null;
    const captchaContainer = document.getElementById('captcha-container');

    // DOM 元素检查
    if (!loginEmailInput || !loginPasswordInput || !loginButton) {
      console.error('[LOGIN] ERROR: Required DOM elements not found');
      return;
    }

    // 类型断言：DOM 检查后这些元素确定存在
    const emailInput = loginEmailInput;
    const passwordInput = loginPasswordInput;
    const submitButton = loginButton;

    /**
     * 执行登录请求
     */
    async function performLogin(): Promise<void> {
      if (!pendingLogin) return;
      
      try {
        const { email, password } = pendingLogin;
        const token = getCaptchaToken('captcha-container');
        const captchaType = getCaptchaType();

        // 禁用按钮，显示加载状态
        submitButton.disabled = true;
        submitButton.textContent = t('login.loggingIn');

        const result = await login(email, password, token || '', captchaType);

        if (result.success) {
          // token 已通过 httpOnly cookie 存储，跳转
          const urlParams = new URLSearchParams(window.location.search);
          const returnUrl = urlParams.get('return');
          if (returnUrl) {
            try {
              const decodedUrl = decodeURIComponent(returnUrl);
              const url = new URL(decodedUrl, window.location.origin);
              if (url.origin === window.location.origin) {
                window.location.href = decodedUrl;
                return;
              }
            } catch {
              // URL 解析失败，继续使用默认跳转
            }
          }
          window.location.href = '/account/dashboard';
        } else {
          const translationKey = errorCodeMap[result.errorCode || ''] || 'login.failed';
          showAlertWithTranslation(t(translationKey));
        }
      } catch (error) {
        console.error('[LOGIN] ERROR: Login failed:', (error as Error).message);
        showAlertWithTranslation(t('login.failed'));
      } finally {
        resetCaptchaState(captchaContainer, card, submitButton);
      }
    }

    /**
     * 处理登录表单提交
     */
    async function handleLogin(e: Event): Promise<void> {
      e.preventDefault();

      try {
        const email = emailInput.value.trim();
        const password = passwordInput.value;

        // 表单验证
        const validation = validateLoginForm(email, password);
        if (!validation.valid) {
          showAlertWithTranslation(t(validation.errorKey!));
          return;
        }

        pendingLogin = { email, password };

        // 如果未配置验证码，直接登录
        if (!getCaptchaSiteKey()) {
          await performLogin();
        } else {
          // 禁用登录按钮，显示验证组件
          submitButton.disabled = true;

          if (captchaContainer) {
            captchaContainer.classList.remove('is-hidden');
            if (card) {delayedExecution(() => adjustCardHeight(card));}
          }

          await initCaptcha(
            'captcha-container',
            async () => { await performLogin(); },
            () => {
              // 验证失败
              showAlertWithTranslation(t('login.humanVerifyFailed'));
              resetCaptchaState(captchaContainer, card, submitButton);
            },
            () => {
              // 验证过期
              resetCaptchaState(captchaContainer, card, submitButton);
            }
          );
        }
      } catch (error) {
        console.error('[LOGIN] ERROR: Handle login failed:', (error as Error).message);
        showAlertWithTranslation(t('login.failed'));
      }
    }

    // 绑定表单提交事件
    document.getElementById('login-form')?.addEventListener('submit', handleLogin);

    // 清空输入框（防止浏览器自动填充残留）
    emailInput.value = '';
    passwordInput.value = '';

    // 检查 OAuth 错误（从 URL 参数）
    const urlParams = new URLSearchParams(window.location.search);
    const oauthError = urlParams.get('error');
    if (oauthError) {
      // 根据错误类型显示不同提示
      if (oauthError === 'no_linked_account') {
        showAlertWithTranslation(t('login.noLinkedAccount'));
      } else {
        showAlertWithTranslation(t('login.oauthError'));
      }
      window.history.replaceState({}, document.title, window.location.pathname);
    }

    // 更新"创建账户"、"忘记密码"和微软登录链接，携带 return 参数
    const returnUrl = urlParams.get('return');
    if (returnUrl) {
      const createAccountLink = document.querySelector('.footer-links a[href="/account/register"]');
      if (createAccountLink) {
        createAccountLink.setAttribute('href', '/account/register?return=' + encodeURIComponent(returnUrl));
      }
      const forgotPasswordLink = document.querySelector('.footer-links a[href="/account/forgot"]');
      if (forgotPasswordLink) {
        forgotPasswordLink.setAttribute('href', '/account/forgot?return=' + encodeURIComponent(returnUrl));
      }
      const microsoftLoginLink = document.querySelector('.oauth-buttons a[href="/api/auth/microsoft"]');
      if (microsoftLoginLink) {
        microsoftLoginLink.setAttribute('href', '/api/auth/microsoft?return=' + encodeURIComponent(returnUrl));
      }
    }

    // 更新页面标题
    updatePageTitle();

    // 调整卡片高度
    if (card) {
      delayedExecution(() => adjustCardHeight(card));
      enableCardAutoResize(card);
    }

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      initializeModals(t);
      updatePageTitle();
      if (card) {delayedExecution(() => adjustCardHeight(card));}
    });

  } catch (error) {
    console.error('[LOGIN] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
  }
});
