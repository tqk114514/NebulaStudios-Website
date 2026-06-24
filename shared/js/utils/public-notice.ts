/**
 * 公示期横幅组件
 *
 * 功能：
 * - 调用 API 获取当前公示期政策
 * - 检查 cookie 判断是否已关闭
 * - 创建横幅 DOM 并插入到指定位置
 * - 关闭按钮写入 cookie（公示期结束后自动过期）
 */

import { setCookie, getCookie } from './cookie.ts';

interface PublicNoticePolicy {
  policy_type: string;
  version: string;
  update_date: string;
  effective_date: string;
}

/**
 * 政策类型 → i18n 键映射
 */
const policyNameKeys: Record<string, string> = {
  privacy: 'policy.privacyPolicy',
  terms: 'policy.termsOfService',
  cookies: 'policy.cookiePolicy',
};

/**
 * 构建 cookie 名称
 * 格式：{政策类型}.public-notice-period.{公示时间}
 * 例：privacy.public-notice-period.2026.3.17
 */
function buildCookieName(policyType: string, updateDate: string): string {
  const [year, month, day] = updateDate.split('-').map(Number);
  return `${policyType}.public-notice-period.${year}.${month}.${day}`;
}

/**
 * 计算从现在到生效日期的秒数（cookie 过期时间）
 */
function calculateCookieSeconds(effectiveDate: string): number {
  const [year, month, day] = effectiveDate.split('-').map(Number);
  const target = new Date(year, month - 1, day, 23, 59, 59);
  const now = new Date();
  return Math.max(1, Math.floor((target.getTime() - now.getTime()) / 1000));
}

/**
 * 获取翻译文本（带回退）
 */
function translate(key: string, fallback: string): string {
  const t = (window as any).t;
  if (!t) return fallback;
  const result = t(key);
  return result === key ? fallback : result;
}

/**
 * 初始化公示期横幅
 * @param target - 目标元素
 * @param position - 插入位置（默认 afterbegin，即容器内部最前面）
 */
export async function initPublicNoticeBanner(
  target: HTMLElement,
  position: InsertPosition = 'afterbegin'
): Promise<void> {
  try {
    const response = await fetch('/api/policy/public-notice');
    if (!response.ok) return;
    const data = await response.json();
    if (!data.success || !data.data || !Array.isArray(data.data)) return;

    // 过滤已关闭的政策（cookie 存在则跳过）
    const notices = (data.data as PublicNoticePolicy[]).filter(p => {
      const cookieName = buildCookieName(p.policy_type, p.update_date);
      return !getCookie(cookieName);
    });

    if (notices.length === 0) return;

    // 创建横幅并插入
    const banner = createBanner(notices);
    target.insertAdjacentElement(position, banner);
  } catch (error) {
    console.error('[PublicNotice] Failed to load:', error);
  }
}

/**
 * 创建横幅 DOM 元素
 */
function createBanner(notices: PublicNoticePolicy[]): HTMLElement {
  const banner = document.createElement('div');
  banner.className = 'public-notice-banner';

  const content = document.createElement('div');
  content.className = 'public-notice-content';

  // 前缀
  const prefix = document.createElement('span');
  prefix.textContent = translate('policy.publicNotice.prefix', '有新的');
  content.appendChild(prefix);

  // 政策链接
  notices.forEach((notice, index) => {
    if (index > 0) {
      const separator = document.createElement('span');
      separator.textContent = translate('policy.publicNotice.separator', '、');
      content.appendChild(separator);
    }
    const link = document.createElement('a');
    link.className = 'public-notice-link';
    const nameKey = policyNameKeys[notice.policy_type] || notice.policy_type;
    link.textContent = translate(nameKey, notice.policy_type);
    link.href = `/policy#${notice.policy_type}/public-notice-period`;
    content.appendChild(link);
  });

  // 后缀
  const suffix = document.createElement('span');
  suffix.textContent = translate('policy.publicNotice.suffix', '在公示期');
  content.appendChild(suffix);

  banner.appendChild(content);

  // 关闭按钮
  const closeBtn = document.createElement('button');
  closeBtn.className = 'public-notice-close';
  closeBtn.setAttribute('aria-label', translate('policy.publicNotice.close', '关闭'));
  closeBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>';
  closeBtn.addEventListener('click', () => {
    // 为每个公示期政策写入 cookie
    notices.forEach(notice => {
      const cookieName = buildCookieName(notice.policy_type, notice.update_date);
      const seconds = calculateCookieSeconds(notice.effective_date);
      setCookie(cookieName, 'dismissed', seconds, true);
    });
    banner.classList.add('is-hidden');
  });
  banner.appendChild(closeBtn);

  return banner;
}
