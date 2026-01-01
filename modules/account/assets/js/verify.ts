/**
 * 验证码页面逻辑
 *
 * 功能：
 * - 验证邮件链接中的 token
 * - 显示 6 位验证码（带动画效果）
 * - 支持点击复制验证码
 * - 错误状态处理
 */

import { initLanguageSwitcher, applyTranslations, waitForTranslations, updatePageTitle, hidePageLoader } from '../../../../shared/js/utils/language-switcher.ts';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/ui/card.ts';
import { getUrlParameter } from './lib/utils/url.ts';

// ==================== 类型定义 ====================

type PageState = 'loading' | 'success' | 'error';

/** 验证结果 */
interface VerifyResult {
  success: boolean;
  code?: string;
  email?: string;
  errorCode?: string;
}

/** API 响应 */
interface VerifyResponse {
  success: boolean;
  code?: string;
  email?: string;
  errorCode?: string;
}

// ==================== 错误码映射 ====================

/**
 * 错误码到翻译键的映射
 */
const errorCodeMap: Record<string, string> = {
  'INVALID_TOKEN': 'verify.errorInvalidToken',
  'TOKEN_EXPIRED': 'verify.errorTokenExpired',
  'TOKEN_USED': 'verify.errorTokenUsed',
  'NO_TOKEN': 'verify.errorNoToken',
  'NETWORK_ERROR': 'verify.errorNetwork',
  'VERIFY_FAILED': 'verify.errorDefault'
};

// ==================== API 调用 ====================

/**
 * 验证 token 并获取验证码
 */
async function verifyToken(token: string): Promise<VerifyResult> {
  try {
    const response = await fetch('/api/auth/verify-token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token })
    });

    const data: VerifyResponse = await response.json();

    if (response.ok && data.success) {
      return { success: true, code: data.code, email: data.email };
    } else {
      return { success: false, errorCode: data.errorCode || 'VERIFY_FAILED' };
    }
  } catch (error) {
    console.error('[VERIFY] ERROR: Token verification request failed:', (error as Error).message);
    return { success: false, errorCode: 'NETWORK_ERROR' };
  }
}

// ==================== 全局变量 ====================

let card: HTMLElement | null = null;

// ==================== 状态管理 ====================

/**
 * 切换显示状态（loading/success/error）
 */
function showState(state: PageState): void {
  const loadingState = document.getElementById('loading-state');
  const successState = document.getElementById('success-state');
  const errorState = document.getElementById('error-state');

  if (loadingState) loadingState.style.display = state === 'loading' ? 'block' : 'none';
  if (successState) successState.style.display = state === 'success' ? 'block' : 'none';
  if (errorState) errorState.style.display = state === 'error' ? 'block' : 'none';

  if (card) delayedExecution(() => adjustCardHeight(card));
}

/**
 * 显示错误状态
 */
function showError(errorCode: string): void {
  const translationKey = errorCodeMap[errorCode] || 'verify.errorDefault';
  const errorMessage = window.t ? window.t(translationKey) : translationKey;

  const errorElement = document.getElementById('error-message') as HTMLElement | null;
  if (errorElement) {
    errorElement.textContent = errorMessage;
    errorElement.dataset.errorCode = errorCode;
  }
  showState('error');
}

// ==================== 验证码操作 ====================

/**
 * 复制验证码到剪贴板
 */
function copyCode(): void {
  const codeBoxes = document.querySelectorAll('.code-box');
  const code = Array.from(codeBoxes).map(box => box.textContent).join('');

  if (code && code !== '------') {
    navigator.clipboard.writeText(code).catch(() => {
      // 降级方案：使用 execCommand
      const textArea = document.createElement('textarea');
      textArea.value = code;
      textArea.style.position = 'fixed';
      textArea.style.opacity = '0';
      document.body.appendChild(textArea);
      textArea.select();
      try {
        document.execCommand('copy');
      } catch (err) {
        console.error('[VERIFY] ERROR: Copy failed:', (err as Error).message);
      }
      document.body.removeChild(textArea);
    });
  }
}

/**
 * 加载并验证 token，显示验证码
 */
async function loadVerificationCode(): Promise<void> {
  try {
    const token = getUrlParameter('token');

    if (!token) {
      showError('NO_TOKEN');
      return;
    }

    const result = await verifyToken(token);

    if (result.success && result.code) {
      const code = result.code.toString();
      const codeBoxes = document.querySelectorAll('.code-box');

      // 逐个显示验证码数字（带动画延迟）
      codeBoxes.forEach((box, index) => {
        if (index < code.length) {
          setTimeout(() => {
            box.textContent = code[index];
            box.classList.add('is-filled');
          }, index * 100);
        }
      });

      showState('success');

      // 绑定点击复制事件
      const verificationCodeEl = document.getElementById('verification-code');
      if (verificationCodeEl) {
        verificationCodeEl.addEventListener('click', copyCode);
      }

      // 保存邮箱到 sessionStorage（用于后续注册）
      if (result.email) {
        sessionStorage.setItem('verify_email', result.email);
      }
    } else {
      showError(result.errorCode || 'INVALID_TOKEN');
    }
  } catch (error) {
    console.error('[VERIFY] ERROR: Load verification code failed:', (error as Error).message);
    showError('NETWORK_ERROR');
  }
}

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译系统就绪
    await waitForTranslations();
    hidePageLoader();

    card = document.querySelector('.card') as HTMLElement | null;

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      applyTranslations();
      updatePageTitle();

      // 重新显示错误信息（如果有）
      const errorMessage = document.getElementById('error-message') as HTMLElement | null;
      if (errorMessage && errorMessage.dataset.errorCode) {
        showError(errorMessage.dataset.errorCode);
      }

      if (card) delayedExecution(() => adjustCardHeight(card));
    });

    // 应用翻译
    applyTranslations();
    updatePageTitle();

    // 绑定返回按钮事件（替代内联 onclick）
    const successBackBtn = document.getElementById('success-back-btn');
    const errorBackBtn = document.getElementById('error-back-btn');

    if (successBackBtn) {
      successBackBtn.addEventListener('click', () => window.close());
    }
    if (errorBackBtn) {
      errorBackBtn.addEventListener('click', () => window.close());
    }

    // 加载验证码
    await loadVerificationCode();

    // 调整卡片高度
    if (card) {
      setTimeout(() => adjustCardHeight(card), 100);
      enableCardAutoResize(card);
    }
  } catch (error) {
    console.error('[VERIFY] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
    showError('NETWORK_ERROR');
  }
});
