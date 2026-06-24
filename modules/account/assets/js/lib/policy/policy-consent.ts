/**
 * 强制同意弹窗组件（account 模块专用）
 *
 * 功能：
 * - 调用 API 检查当前用户是否有未同意的已生效政策
 * - 有则弹出强制同意弹窗（隐私政策 + 服务条款合并展示）
 * - 同意 → 调用 API 写入同意记录 → 返回 true
 * - 拒绝 → 调用登出 → 跳转登录页 → 返回 false
 * - 无需同意 → 直接返回 true
 *
 * 触发条件：政策生效时间已到达，且用户最新同意版本 != 当前最新生效版本
 */

type TranslateFunction = (key: string) => string;

interface PendingConsentPolicy {
  policy_type: string;
  version: string;
  effective_date: string;
}

interface PendingConsentResponse {
  success: boolean;
  data?: { policies: PendingConsentPolicy[] };
  errorCode?: string;
}

interface ConsentResponse {
  success: boolean;
  errorCode?: string;
}

/**
 * 政策类型 → i18n 键映射
 */
const policyNameKeys: Record<string, string> = {
  privacy: 'policy.privacyPolicy',
  terms: 'policy.termsOfService',
};

/**
 * 获取翻译文本（带回退）
 */
function translate(t: TranslateFunction, key: string, fallback: string): string {
  const result = t(key);
  return result === key ? fallback : result;
}

/**
 * 检查并处理政策同意
 *
 * 调用时机：登录成功后、OAuth 授权页加载时、dashboard 加载时
 *
 * @param t 翻译函数
 * @returns true 表示已同意（或无需同意），可继续；false 表示拒绝（已登出并跳转登录页）
 */
export async function checkPolicyConsent(t: TranslateFunction): Promise<boolean> {
  let pending: PendingConsentPolicy[] = [];

  try {
    const response = await fetch('/api/policy/pending-consent', {
      credentials: 'include',
    });

    if (response.status === 401) {
      // 未登录，交由调用方处理
      return true;
    }

    if (!response.ok) {
      console.error('[PolicyConsent] Failed to check pending consent:', response.status);
      return true;
    }

    const data: PendingConsentResponse = await response.json();
    if (!data.success || !data.data || !Array.isArray(data.data.policies)) {
      return true;
    }

    pending = data.data.policies;
  } catch (error) {
    console.error('[PolicyConsent] Error checking pending consent:', error);
    // 网络错误时不阻断流程，避免影响正常登录
    return true;
  }

  if (pending.length === 0) {
    return true;
  }

  return showConsentModal(pending, t);
}

/**
 * 显示强制同意弹窗
 *
 * @returns true 表示同意，false 表示拒绝（已登出）
 */
function showConsentModal(policies: PendingConsentPolicy[], t: TranslateFunction): Promise<boolean> {
  return new Promise((resolve) => {
    const overlay = createModalElement(policies, t);
    document.body.appendChild(overlay);

    requestAnimationFrame(() => {
      overlay.classList.remove('is-hidden');
    });

    const checkbox = overlay.querySelector<HTMLInputElement>('#consent-checkbox')!;
    const acceptBtn = overlay.querySelector<HTMLButtonElement>('#consent-accept-btn')!;
    const declineBtn = overlay.querySelector<HTMLButtonElement>('#consent-decline-btn')!;

    let isResolved = false;

    const cleanup = (): void => {
      overlay.classList.add('is-hidden');
      setTimeout(() => overlay.remove(), 400);
    };

    // 复选框控制同意按钮状态
    checkbox.addEventListener('change', () => {
      acceptBtn.disabled = !checkbox.checked;
    });

    // 同意
    acceptBtn.addEventListener('click', async () => {
      if (isResolved) return;
      isResolved = true;

      acceptBtn.disabled = true;
      declineBtn.disabled = true;
      acceptBtn.textContent = translate(t, 'policy.consent.submitting', '提交中...');

      try {
        const response = await fetch('/api/policy/consent', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({
            policies: policies.map(p => ({
              policy_type: p.policy_type,
              policy_version: p.version,
            })),
          }),
        });

        const data: ConsentResponse = await response.json();

        if (data.success) {
          cleanup();
          resolve(true);
        } else {
          // 写入失败，恢复按钮状态
          isResolved = false;
          acceptBtn.disabled = !checkbox.checked;
          declineBtn.disabled = false;
          acceptBtn.textContent = translate(t, 'policy.consent.accept', '同意并继续');
          const errorMsg = translate(t, 'policy.consent.failed', '同意记录失败，请稍后重试');
          const errorEl = overlay.querySelector<HTMLElement>('#consent-error')!;
          errorEl.textContent = errorMsg;
          errorEl.classList.remove('is-hidden');
        }
      } catch (error) {
        console.error('[PolicyConsent] Error recording consent:', error);
        isResolved = false;
        acceptBtn.disabled = !checkbox.checked;
        declineBtn.disabled = false;
        acceptBtn.textContent = translate(t, 'policy.consent.accept', '同意并继续');
        const errorMsg = translate(t, 'policy.consent.failed', '同意记录失败，请稍后重试');
        const errorEl = overlay.querySelector<HTMLElement>('#consent-error')!;
        errorEl.textContent = errorMsg;
        errorEl.classList.remove('is-hidden');
      }
    });

    // 拒绝 → 登出并跳转登录页
    declineBtn.addEventListener('click', async () => {
      if (isResolved) return;
      isResolved = true;

      declineBtn.disabled = true;
      acceptBtn.disabled = true;

      try {
        await fetch('/api/auth/logout', {
          method: 'POST',
          credentials: 'include',
        });
      } catch {
        // 忽略登出错误，强制跳转
      }

      cleanup();
      window.location.href = '/account/login';
      resolve(false);
    });
  });
}

/**
 * 创建弹窗 DOM 元素
 */
function createModalElement(policies: PendingConsentPolicy[], t: TranslateFunction): HTMLElement {
  const overlay = document.createElement('div');
  overlay.id = 'policy-consent-modal';
  overlay.className = 'modal-overlay policy-consent-overlay is-hidden';

  const container = document.createElement('div');
  container.className = 'modal-container policy-consent-container';

  const content = document.createElement('div');
  content.className = 'modal-content policy-consent-content';

  // 标题
  const title = document.createElement('h2');
  title.className = 'modal-title';
  title.textContent = translate(t, 'policy.consent.title', '政策更新');
  content.appendChild(title);

  // 提示信息
  const message = document.createElement('p');
  message.className = 'modal-message policy-consent-message';
  message.textContent = translate(t, 'policy.consent.message', '以下政策已更新并生效，请阅读并同意后继续：');
  content.appendChild(message);

  // 政策链接列表
  const list = document.createElement('div');
  list.className = 'policy-consent-list';
  policies.forEach(p => {
    const item = document.createElement('div');
    item.className = 'policy-consent-item';

    const link = document.createElement('a');
    link.className = 'policy-consent-link';
    const nameKey = policyNameKeys[p.policy_type] || p.policy_type;
    link.textContent = translate(t, nameKey, p.policy_type);
    link.href = `/policy#${p.policy_type}`;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    item.appendChild(link);

    // 生效日期
    if (p.effective_date) {
      const date = document.createElement('span');
      date.className = 'policy-consent-date';
      date.textContent = translate(t, 'policy.consent.effectiveDate', '生效于') + ' ' + p.effective_date;
      item.appendChild(date);
    }

    list.appendChild(item);
  });
  content.appendChild(list);

  // 复选框
  const checkboxLabel = document.createElement('label');
  checkboxLabel.className = 'policy-consent-checkbox';
  const checkbox = document.createElement('input');
  checkbox.type = 'checkbox';
  checkbox.id = 'consent-checkbox';
  const checkboxText = document.createElement('span');
  checkboxText.textContent = translate(t, 'policy.consent.agree', '我已阅读并同意上述政策');
  checkboxLabel.appendChild(checkbox);
  checkboxLabel.appendChild(checkboxText);
  content.appendChild(checkboxLabel);

  // 错误提示（默认隐藏）
  const errorEl = document.createElement('p');
  errorEl.id = 'consent-error';
  errorEl.className = 'policy-consent-error is-hidden';
  content.appendChild(errorEl);

  // 按钮组
  const footer = document.createElement('div');
  footer.className = 'modal-footer modal-footer-buttons';

  const acceptBtn = document.createElement('button');
  acceptBtn.type = 'button';
  acceptBtn.id = 'consent-accept-btn';
  acceptBtn.className = 'button-primary';
  acceptBtn.textContent = translate(t, 'policy.consent.accept', '同意并继续');
  acceptBtn.disabled = true;

  const declineBtn = document.createElement('button');
  declineBtn.type = 'button';
  declineBtn.id = 'consent-decline-btn';
  declineBtn.className = 'button-secondary';
  declineBtn.textContent = translate(t, 'policy.consent.decline', '拒绝');

  footer.appendChild(acceptBtn);
  footer.appendChild(declineBtn);
  content.appendChild(footer);

  container.appendChild(content);
  overlay.appendChild(container);

  return overlay;
}
