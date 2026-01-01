/**
 * assets/js/register.ts
 * 注册页面逻辑
 * 
 * 功能：
 * - 用户注册表单处理
 * - 邮箱验证码发送与倒计时
 * - 用户名长度验证
 * - 密码强度实时验证
 * - 人机验证集成（Turnstile/hCaptcha）
 * - 会话检查（已登录自动跳转）
 */

// ==================== 模块导入 ====================
import { initializeModals, showAlert, showSupportedEmailsModal } from './lib/ui/feedback.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { startCountdown, resumeCountdown, isCountingDown, clearCodeExpiryTimer, getCodeExpiryTime } from './lib/utils/countdown.ts';
import { loadEmailWhitelist, validateEmail, getEmailProviders, isUsernameTooLong, validateRegisterForm } from './lib/validators.ts';
import { loadCaptchaConfig, getCaptchaSiteKey, getCaptchaType, initCaptcha, clearCaptcha, getCaptchaToken } from './lib/captcha.ts';
import { sendVerificationCode, register, verifySession, errorCodeMap } from './lib/api/auth.ts';
import { initLanguageSwitcher, waitForTranslations, updatePageTitle, hidePageLoader } from '../../../../shared/js/utils/language-switcher.ts';

// ==================== 全局变量 ====================

// 翻译函数（从全局获取，若不存在则返回原始 key）
const t = window.t || ((key: string) => key);

/**
 * 显示带翻译的提示弹窗
 */
const showAlertWithTranslation = (message: string, title?: string): void => {
  showAlert(message, title, t);
};

// 待发送验证码的邮箱地址
let pendingEmail = '';

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();
    
    // 检查是否已登录，已登录则跳转 dashboard
    const sessionResult = await verifySession();
    if (sessionResult.success) {
      window.location.href = '/account/dashboard';
      return;
    }

    // 并行加载邮箱白名单、验证码配置
    const [emailWhitelistResult] = await Promise.all([
      loadEmailWhitelist(),
      loadCaptchaConfig()
    ]);
    
    // 邮箱白名单加载失败时提示
    if (!emailWhitelistResult.success) {
      hidePageLoader();
      initializeModals(t);
      showAlertWithTranslation(t('error.loadEmailWhitelistFailed') || '加载邮箱白名单失败，请刷新页面重试');
      return;
    }
    
    // 隐藏页面加载遮罩
    hidePageLoader();
    
    // 初始化弹窗组件
    initializeModals(t);
    
    // ==================== DOM 元素获取 ====================
    
    const registerUsernameInput = document.getElementById('register-username') as HTMLInputElement | null;
    const registerEmailInput = document.getElementById('register-email') as HTMLInputElement | null;
    const registerVerificationCodeInput = document.getElementById('register-verification-code') as HTMLInputElement | null;
    const sendCodeButton = document.getElementById('send-verification-code') as HTMLButtonElement | null;
    const registerPasswordInput = document.getElementById('register-password') as HTMLInputElement | null;
    const registerPasswordConfirmInput = document.getElementById('register-password-confirm') as HTMLInputElement | null;
    const registerButton = document.querySelector('#register-form .button-primary') as HTMLButtonElement | null;
    const usernameError = document.getElementById('username-error');
    const showSupportedEmailsLink = document.getElementById('show-supported-emails');
    const codeExpiryTimerElement = document.getElementById('code-expiry-timer');
    const card = document.querySelector('.card') as HTMLElement | null;
    
    // DOM 元素检查
    if (!registerUsernameInput || !registerEmailInput || !registerPasswordInput || 
        !registerPasswordConfirmInput || !registerButton || !sendCodeButton) {
      console.error('[REGISTER] ERROR: Required DOM elements not found');
      return;
    }

    // ==================== 表单验证函数 ====================

    /**
     * 用户名输入验证（只检查长度）
     */
    function onUsernameInput(): void {
      const username = registerUsernameInput!.value.trim();
      const usernameErrorText = document.getElementById('username-error-text');
      const wasHidden = usernameError?.classList.contains('is-hidden');
      
      // 空输入时隐藏错误
      if (username.length === 0) {
        if (!wasHidden) {
          usernameError?.classList.add('is-hidden');
          delayedExecution(() => adjustCardHeight(card));
        }
        return;
      }
      
      // 检查用户名长度
      if (isUsernameTooLong(username)) {
        if (usernameErrorText) {
          usernameErrorText.setAttribute('data-i18n', 'register.usernameTooLong');
          usernameErrorText.textContent = t('register.usernameTooLong') || '用户名过长';
        }
        if (wasHidden) {
          usernameError?.classList.remove('is-hidden');
          delayedExecution(() => adjustCardHeight(card));
        }
      } else {
        if (!wasHidden) {
          usernameError?.classList.add('is-hidden');
          delayedExecution(() => adjustCardHeight(card));
        }
      }
    }
    
    /**
     * 更新发送验证码按钮状态
     * 根据邮箱验证结果启用/禁用按钮
     */
    function updateSendCodeButtonState(): void {
      const email = registerEmailInput!.value.trim();
      const validation = validateEmail(email);
      const emailError = document.getElementById('email-error');
      const emailErrorText = document.getElementById('email-error-text');
      const wasHidden = emailError?.classList.contains('is-hidden');
      
      // 始终检测邮箱格式并显示错误
      if (!validation.valid) {
        // 邮箱不在白名单时显示错误
        if (email && validation.errorKey === 'register.emailNotSupported') {
          emailError?.classList.remove('is-hidden');
          if (emailErrorText) {
            emailErrorText.setAttribute('data-i18n', validation.errorKey);
            emailErrorText.textContent = t(validation.errorKey);
          }
          if (wasHidden) delayedExecution(() => adjustCardHeight(card));
        } else {
          emailError?.classList.add('is-hidden');
          if (!wasHidden) delayedExecution(() => adjustCardHeight(card));
        }
      } else {
        if (!emailError?.classList.contains('is-hidden')) {
          emailError?.classList.add('is-hidden');
          delayedExecution(() => adjustCardHeight(card));
        }
      }
      
      // 倒计时中或输入框禁用时不更新按钮状态
      if (isCountingDown() || registerEmailInput!.disabled || /^\d+s$/.test(sendCodeButton!.textContent || '')) {
        return;
      }
      
      // 更新按钮状态
      sendCodeButton!.disabled = !validation.valid;
    }

    // ==================== 验证码发送 ====================

    /**
     * 处理发送验证码请求
     */
    async function handleSendCode(): Promise<void> {
      try {
        const email = pendingEmail;
        const token = getCaptchaToken();
        const captchaType = getCaptchaType();
        
        const result = await sendVerificationCode(email, token || '', captchaType || '');
        
        if (result.success) {
          // 发送成功，开始倒计时
          startCountdown(sendCodeButton!, {
            seconds: 60,
            input: registerEmailInput!,
            t,
            onComplete: () => {
              pendingEmail = '';
              if (!getCodeExpiryTime()) {
                updateSendCodeButtonState();
              }
            }
          });
          showAlertWithTranslation(t('register.codeSent'));
        } else {
          // 发送失败，显示错误信息
          const translationKey = errorCodeMap[result.errorCode as keyof typeof errorCodeMap] || 'register.sendFailed';
          showAlertWithTranslation(t(translationKey));
          pendingEmail = '';
          updateSendCodeButtonState();
        }
      } catch (error) {
        console.error('[REGISTER] ERROR: Send code failed:', (error as Error).message);
        showAlertWithTranslation(t('register.sendFailed'));
        pendingEmail = '';
        updateSendCodeButtonState();
      } finally {
        // 清理验证组件
        clearCaptcha();
      }
    }
    
    /**
     * 发送验证码按钮点击处理
     */
    async function onSendCodeClick(): Promise<void> {
      // 倒计时中不处理
      if (isCountingDown()) return;

      try {
        const email = registerEmailInput!.value.trim();
        const validation = validateEmail(email);
        
        // 邮箱验证失败
        if (!validation.valid) {
          showAlertWithTranslation(t(validation.errorKey || 'register.invalidEmail'));
          registerEmailInput!.focus();
          return;
        }
        
        pendingEmail = email;
        sendCodeButton!.disabled = true;
        
        // 显示验证容器
        const captchaContainer = document.getElementById('captcha-container');
        if (captchaContainer) {
          captchaContainer.classList.remove('is-hidden');
          if (card) delayedExecution(() => adjustCardHeight(card));
        }
        
        // 无需人机验证时直接发送
        if (!getCaptchaSiteKey()) {
          await handleSendCode();
          if (captchaContainer) {
            captchaContainer.classList.add('is-hidden');
            if (card) delayedExecution(() => adjustCardHeight(card));
          }
        } else {
          // 初始化人机验证
          await initCaptcha(
            // 验证成功回调
            async () => {
              await handleSendCode();
              if (captchaContainer) {
                captchaContainer.classList.add('is-hidden');
                if (card) delayedExecution(() => adjustCardHeight(card));
              }
            },
            // 验证失败回调
            () => {
              showAlertWithTranslation(t('register.humanVerifyFailed'));
              if (!getCodeExpiryTime()) {
                registerEmailInput!.disabled = false;
                updateSendCodeButtonState();
              }
              pendingEmail = '';
              clearCaptcha();
              if (captchaContainer) {
                captchaContainer.classList.add('is-hidden');
                if (card) delayedExecution(() => adjustCardHeight(card));
              }
            },
            // 验证过期回调
            () => {
              if (!getCodeExpiryTime()) {
                registerEmailInput!.disabled = false;
                updateSendCodeButtonState();
              }
              pendingEmail = '';
              clearCaptcha();
              if (captchaContainer) {
                captchaContainer.classList.add('is-hidden');
                if (card) delayedExecution(() => adjustCardHeight(card));
              }
            }
          );
        }
      } catch (error) {
        console.error('[REGISTER] ERROR: Send code click failed:', (error as Error).message);
        showAlertWithTranslation(t('register.sendFailed'));
        pendingEmail = '';
        updateSendCodeButtonState();
      }
    }
    
    /**
     * 验证码输入过滤（只允许数字和字母）
     */
    function onVerificationCodeInput(): void {
      const code = registerVerificationCodeInput!.value.trim();
      if (/[^0-9a-zA-Z]/.test(code)) {
        registerVerificationCodeInput!.value = code.replace(/[^0-9a-zA-Z]/g, '');
      }
    }

    // ==================== 注册提交 ====================

    /**
     * 处理注册表单提交
     */
    async function handleRegister(e: Event): Promise<void> {
      e.preventDefault();
      
      try {
        // 收集表单数据
        const formData = {
          username: registerUsernameInput!.value.trim(),
          email: registerEmailInput!.value.trim(),
          verificationCode: registerVerificationCodeInput?.value.trim() || '',
          password: registerPasswordInput!.value,
          passwordConfirm: registerPasswordConfirmInput!.value
        };
        
        // 表单验证
        const validation = validateRegisterForm(formData);
        if (!validation.valid) {
          showAlertWithTranslation(t(validation.errorKey || 'register.validationFailed'));
          return;
        }
        
        // 禁用按钮，显示加载状态
        registerButton!.disabled = true;
        registerButton!.textContent = t('register.registering') || '注册中...';
        
        // 发送注册请求
        const result = await register(formData);
        
        if (result.success) {
          showAlertWithTranslation(t('register.success'));
          // 注册成功后跳转到登录页
          setTimeout(() => { window.location.href = '/account/login'; }, 2000);
        } else {
          // 显示错误信息
          const translationKey = errorCodeMap[result.errorCode as keyof typeof errorCodeMap] || 'register.failed';
          showAlertWithTranslation(t(translationKey));
        }
      } catch (error) {
        console.error('[REGISTER] ERROR: Registration failed:', (error as Error).message);
        showAlertWithTranslation(t('register.failed'));
      } finally {
        // 恢复按钮状态
        registerButton!.disabled = false;
        registerButton!.textContent = t('register.submitButton');
      }
    }
    
    // ==================== 事件绑定 ====================
    
    // 用户名输入验证
    registerUsernameInput.addEventListener('input', onUsernameInput);
    
    // 邮箱输入验证
    registerEmailInput.addEventListener('input', updateSendCodeButtonState);
    
    // 验证码输入过滤
    registerVerificationCodeInput?.addEventListener('input', onVerificationCodeInput);
    
    // 发送验证码按钮
    sendCodeButton?.addEventListener('click', onSendCodeClick);
    
    // 显示支持的邮箱列表
    showSupportedEmailsLink?.addEventListener('click', (e) => {
      e.preventDefault();
      showSupportedEmailsModal(getEmailProviders(), t);
    });

    // 密码强度实时验证
    registerPasswordInput?.addEventListener('input', () => {
      const password = registerPasswordInput.value;
      // 长度要求：16-64 字符
      document.getElementById('req-length')?.classList.toggle('is-valid', password.length >= 16 && password.length <= 64);
      // 包含数字
      document.getElementById('req-number')?.classList.toggle('is-valid', /\d/.test(password));
      // 包含特殊字符
      document.getElementById('req-special')?.classList.toggle('is-valid', /[!@#$%^&*(),.?":{}|<>]/.test(password));
      // 包含大小写字母
      document.getElementById('req-case')?.classList.toggle('is-valid', /[A-Z]/.test(password) && /[a-z]/.test(password));
    });
    
    // 注册表单提交
    document.getElementById('register-form')?.addEventListener('submit', handleRegister);
    
    // ==================== 初始化 ====================
    
    // 清空所有输入框
    registerEmailInput.value = '';
    registerUsernameInput.value = '';
    registerPasswordInput.value = '';
    registerPasswordConfirmInput.value = '';
    if (registerVerificationCodeInput) {
      registerVerificationCodeInput.value = '';
    }
    
    // 恢复倒计时状态（页面刷新后）
    resumeCountdown(sendCodeButton, {
      input: registerEmailInput,
      t,
      onComplete: () => {
        pendingEmail = '';
        if (!getCodeExpiryTime()) updateSendCodeButtonState();
      }
    });
    
    // 页面卸载时清理验证码过期定时器
    window.addEventListener('beforeunload', () => {
      clearCodeExpiryTimer(codeExpiryTimerElement);
    });
    
    // 更新页面标题
    updatePageTitle();
    
    // 调整卡片高度
    delayedExecution(() => adjustCardHeight(card));
    
    // 启用卡片自动调整大小
    enableCardAutoResize(card);
    
    // 初始化语言切换器
    initLanguageSwitcher(() => {
      initializeModals(t);
      updateSendCodeButtonState();
      updatePageTitle();
      if (card) delayedExecution(() => adjustCardHeight(card));
    });
  } catch (error) {
    console.error('[REGISTER] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
  }
});
