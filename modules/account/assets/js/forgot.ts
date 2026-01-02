/**
 * assets/js/forgot.ts
 * 忘记密码页面逻辑
 *
 * 功能：
 * - 邮箱验证（格式 + 白名单）
 * - 发送重置密码验证码
 * - 密码强度验证
 * - 重置密码
 */

// ==================== 模块导入 ====================
import { initializeModals, showAlert, showSupportedEmailsModal } from './lib/ui/feedback.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { loadEmailWhitelist, validateEmail, validatePassword, getEmailProviders } from './lib/validators.ts';
import { initLanguageSwitcher, waitForTranslations, updatePageTitle, hidePageLoader } from '../../../../shared/js/utils/language-switcher.ts';
import { loadCaptchaConfig, getCaptchaSiteKey, getCaptchaType, initCaptcha, clearCaptcha, getCaptchaToken } from './lib/captcha.ts';

// ==================== 全局变量 ====================

const t = window.t || ((key: string): string => key);
const showAlertWithTranslation = (message: string, title?: string): void => showAlert(message, title || '', t);

/** 当前邮箱 */
let currentEmail: string | null = null;

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译系统就绪
    await waitForTranslations();

    // 并行加载邮箱白名单和验证码配置
    const [emailWhitelistResult] = await Promise.all([
      loadEmailWhitelist(),
      loadCaptchaConfig()
    ]);

    // 邮箱白名单加载失败时提示
    if (!emailWhitelistResult.success) {
      hidePageLoader();
      initializeModals(t);
      showAlertWithTranslation(t('error.loadEmailWhitelistFailed'));
      return;
    }

    hidePageLoader();

    // 初始化弹窗
    initializeModals(t);

    // 获取 DOM 元素
    const card = document.querySelector('.card') as HTMLElement | null;
    const emailStep = document.getElementById('email-step') as HTMLFormElement | null;
    const resetStep = document.getElementById('reset-step') as HTMLFormElement | null;
    const emailInput = document.getElementById('reset-email') as HTMLInputElement | null;
    const codeInput = document.getElementById('reset-code') as HTMLInputElement | null;
    const passwordInput = document.getElementById('reset-password') as HTMLInputElement | null;
    const passwordConfirmInput = document.getElementById('reset-password-confirm') as HTMLInputElement | null;
    const captchaContainer = document.getElementById('captcha-container');
    const emailError = document.getElementById('email-error');
    const emailErrorText = document.getElementById('email-error-text');
    const showSupportedEmailsLink = document.getElementById('show-supported-emails');

    // DOM 元素检查
    if (!emailStep || !resetStep || !emailInput || !codeInput || !passwordInput || !passwordConfirmInput) {
      console.error('[FORGOT-PASSWORD] ERROR: Required DOM elements not found');
      return;
    }

    const emailSubmitBtn = emailStep.querySelector('.button-secondary') as HTMLButtonElement | null;
    const resetSubmitBtn = resetStep.querySelector('.button-primary') as HTMLButtonElement | null;

    if (!emailSubmitBtn || !resetSubmitBtn) {
      console.error('[FORGOT-PASSWORD] ERROR: Submit buttons not found');
      return;
    }

    // 密码强度指示器
    const reqLength = document.getElementById('req-length');
    const reqNumber = document.getElementById('req-number');
    const reqSpecial = document.getElementById('req-special');
    const reqCase = document.getElementById('req-case');

    // ==================== 邮箱验证 ====================

    /**
     * 更新发送验证码按钮状态
     */
    function updateSendCodeButtonState(): void {
      const email = emailInput!.value.trim();
      const validation = validateEmail(email);
      const wasHidden = emailError?.classList.contains('is-hidden');

      if (!validation.valid) {
        emailSubmitBtn!.disabled = true;
        // 邮箱不在白名单时显示错误
        if (email && validation.errorKey === 'register.emailNotSupported') {
          emailError?.classList.remove('is-hidden');
          emailErrorText?.setAttribute('data-i18n', validation.errorKey);
          if (emailErrorText) {emailErrorText.textContent = t(validation.errorKey);}
          if (wasHidden) {delayedExecution(() => adjustCardHeight(card));}
        } else {
          emailError?.classList.add('is-hidden');
          if (!wasHidden) {delayedExecution(() => adjustCardHeight(card));}
        }
      } else {
        emailSubmitBtn!.disabled = false;
        if (!emailError?.classList.contains('is-hidden')) {
          emailError?.classList.add('is-hidden');
          delayedExecution(() => adjustCardHeight(card));
        }
      }
    }

    // 监听邮箱输入
    emailInput.addEventListener('input', updateSendCodeButtonState);

    // 显示支持的邮箱列表
    showSupportedEmailsLink?.addEventListener('click', (e) => {
      e.preventDefault();
      showSupportedEmailsModal(getEmailProviders(), t);
    });

    // ==================== 步骤切换 ====================

    /**
     * 切换到重置密码步骤
     */
    function showResetStep(): void {
      emailStep!.classList.add('is-hidden');
      resetStep!.classList.remove('is-hidden');
      delayedExecution(() => adjustCardHeight(card));
      codeInput!.focus();
    }

    // ==================== 密码验证 ====================

    /**
     * 更新密码强度指示器
     */
    function updatePasswordRequirements(password: string): void {
      const hasLength = password.length >= 16 && password.length <= 64;
      const hasNumber = /\d/.test(password);
      const hasSpecial = /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/.test(password);
      const hasCase = /[a-z]/.test(password) && /[A-Z]/.test(password);

      reqLength?.classList.toggle('is-valid', hasLength);
      reqNumber?.classList.toggle('is-valid', hasNumber);
      reqSpecial?.classList.toggle('is-valid', hasSpecial);
      reqCase?.classList.toggle('is-valid', hasCase);
    }

    // 监听密码输入
    passwordInput.addEventListener('input', () => {
      updatePasswordRequirements(passwordInput!.value);
    });

    // ==================== 发送验证码 ====================

    /**
     * 发送重置密码验证码
     */
    async function sendResetCode(): Promise<void> {
      const email = emailInput!.value.trim().toLowerCase();
      const token = getCaptchaToken();
      const captchaType = getCaptchaType();

      try {
        const response = await fetch('/api/auth/send-reset-code', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            email,
            captchaToken: token,
            captchaType: captchaType,
            language: document.documentElement.lang || 'zh-CN'
          })
        });
        const result = await response.json();

        if (result.success) {
          currentEmail = email;
          showAlertWithTranslation(t('forgotPassword.codeSent'));
          showResetStep();
        } else {
          // 根据错误码显示对应提示
          const errorMessages: Record<string, string> = {
            'EMAIL_NOT_FOUND': 'forgotPassword.emailNotFound',
            'CAPTCHA_FAILED': 'register.humanVerifyFailed',
            'RATE_LIMIT': 'error.rateLimitExceeded',
            'SEND_FAILED': 'forgotPassword.sendFailed'
          };
          const errorKey = errorMessages[result.errorCode] || 'forgotPassword.sendFailed';
          showAlertWithTranslation(t(errorKey));
        }
      } catch {
        showAlertWithTranslation(t('error.networkError'));
      }

      emailSubmitBtn!.disabled = false;
      clearCaptcha();

      if (captchaContainer) {
        captchaContainer.classList.add('is-hidden');
        delayedExecution(() => adjustCardHeight(card));
      }
    }

    /**
     * 处理发送验证码表单提交
     */
    async function handleEmailSubmit(e: Event): Promise<void> {
      e.preventDefault();

      try {
        const email = emailInput!.value.trim();
        const validation = validateEmail(email);

        if (!validation.valid) {
          showAlertWithTranslation(t(validation.errorKey!));
          return;
        }

        // 禁用按钮
        emailSubmitBtn!.disabled = true;

        // 如果未配置验证码，直接发送
        if (!getCaptchaSiteKey()) {
          await sendResetCode();
        } else {
          // 显示验证组件
          if (captchaContainer) {
            captchaContainer.classList.remove('is-hidden');
            if (card) {delayedExecution(() => adjustCardHeight(card));}
          }

          await initCaptcha(
            async () => { await sendResetCode(); },
            () => {
              showAlertWithTranslation(t('register.humanVerifyFailed'));
              emailSubmitBtn!.disabled = false;
              clearCaptcha();
              if (captchaContainer) {
                captchaContainer.classList.add('is-hidden');
                if (card) {delayedExecution(() => adjustCardHeight(card));}
              }
            },
            () => {
              emailSubmitBtn!.disabled = false;
              clearCaptcha();
              if (captchaContainer) {
                captchaContainer.classList.add('is-hidden');
                if (card) {delayedExecution(() => adjustCardHeight(card));}
              }
            }
          );
        }
      } catch (error) {
        console.error('[FORGOT-PASSWORD] ERROR: Email submit failed:', (error as Error).message);
        showAlertWithTranslation(t('forgotPassword.sendFailed'));
        emailSubmitBtn!.disabled = false;
      }
    }

    // ==================== 重置密码 ====================

    /**
     * 处理重置密码表单提交
     */
    async function handleResetSubmit(e: Event): Promise<void> {
      e.preventDefault();

      const code = codeInput!.value.trim();
      const password = passwordInput!.value;
      const passwordConfirm = passwordConfirmInput!.value;

      // 验证码检查
      if (!code) {
        showAlertWithTranslation(t('forgotPassword.codeRequired'));
        return;
      }

      // 密码验证
      const passwordValidation = validatePassword(password);
      if (!passwordValidation.valid) {
        showAlertWithTranslation(t(passwordValidation.errorKey!));
        return;
      }

      // 确认密码
      if (password !== passwordConfirm) {
        showAlertWithTranslation(t('register.passwordMismatch'));
        return;
      }

      resetSubmitBtn!.disabled = true;
      resetSubmitBtn!.textContent = t('forgotPassword.resetting') || '重置中...';

      try {
        const response = await fetch('/api/auth/reset-password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: currentEmail, code, password })
        });
        const result = await response.json();

        if (result.success) {
          showAlertWithTranslation(t('forgotPassword.resetSuccess'));
          // 重置成功后跳转到登录页
          setTimeout(() => {
            window.location.href = '/account/login';
          }, 1500);
        } else {
          const errorMessages: Record<string, string> = {
            'INVALID_CODE': 'forgotPassword.invalidCode',
            'CODE_EXPIRED': 'forgotPassword.codeExpired',
            'USER_NOT_FOUND': 'forgotPassword.emailNotFound',
            'RESET_FAILED': 'forgotPassword.resetFailed'
          };
          const errorKey = errorMessages[result.errorCode] || 'forgotPassword.resetFailed';
          showAlertWithTranslation(t(errorKey));
          resetSubmitBtn!.disabled = false;
          resetSubmitBtn!.textContent = t('forgotPassword.resetPassword');
        }
      } catch {
        showAlertWithTranslation(t('error.networkError'));
        resetSubmitBtn!.disabled = false;
        resetSubmitBtn!.textContent = t('forgotPassword.resetPassword');
      }
    }

    // ==================== 事件绑定 ====================

    // 绑定表单提交事件
    emailStep.addEventListener('submit', handleEmailSubmit);
    resetStep.addEventListener('submit', handleResetSubmit);

    // 更新页面标题
    updatePageTitle();

    // 调整卡片高度
    delayedExecution(() => adjustCardHeight(card));
    if (card) {enableCardAutoResize(card);}

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      initializeModals(t);
      updateSendCodeButtonState();
      updatePageTitle();
      if (card) {delayedExecution(() => adjustCardHeight(card));}
    });
  } catch (error) {
    console.error('[FORGOT-PASSWORD] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
  }
});
