/**
 * 公示期横幅组件（account 模块专用）
 *
 * 功能：
 * - 调用 API 获取当前公示期政策
 * - 检查 cookie 判断是否已关闭
 * - 创建横幅 DOM 并插入到指定位置
 * - 关闭按钮写入 cookie（公示期结束后自动过期）
 */

import { setCookie, getCookie } from '../../../../../../shared/js/utils/cookie.ts';

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

// 模块级状态
let allNotices: PublicNoticePolicy[] = [];

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
    allNotices = (data.data as PublicNoticePolicy[]).filter(p => {
      const cookieName = buildCookieName(p.policy_type, p.update_date);
      return !getCookie(cookieName);
    });

    if (allNotices.length === 0) return;

    const banner = createBanner();
    target.insertAdjacentElement(position, banner);
    fillBannerContent(banner, allNotices);
  } catch (error) {
    console.error('[PublicNotice] Failed to load:', error);
  }
}

/**
 * 创建横幅（含关闭按钮）
 */
function createBanner(): HTMLElement {
  const t: (key: string) => string = (window as any).t ?? ((k: string): string => k);

  const banner = document.createElement('div');
  banner.className = 'notice-banner is-closable';

  const content = document.createElement('div');
  content.className = 'notice-banner__content';
  banner.appendChild(content);

  const closeBtn = document.createElement('button');
  closeBtn.className = 'notice-banner__close';
  closeBtn.setAttribute('aria-label', t('policy.publicNotice.close'));
  closeBtn.innerHTML = '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12"/></svg>';
  closeBtn.addEventListener('click', () => {
    allNotices.forEach(notice => {
      const cookieName = buildCookieName(notice.policy_type, notice.update_date);
      const seconds = calculateCookieSeconds(notice.effective_date);
      setCookie(cookieName, 'dismissed', seconds, true);
    });
    banner.classList.add('is-hidden');
  });
  banner.appendChild(closeBtn);

  return banner;
}

/**
 * 填充横幅内容（前缀 + 政策链接 + 后缀）
 */
function fillBannerContent(banner: HTMLElement, notices: PublicNoticePolicy[]): void {
  const content = banner.querySelector('.notice-banner__content');
  if (!content) return;

  content.innerHTML = '';
  const t: (key: string) => string = (window as any).t ?? ((k: string): string => k);

  // 前缀
  const prefix = document.createElement('span');
  prefix.setAttribute('data-i18n', 'policy.publicNotice.prefix');
  prefix.textContent = t('policy.publicNotice.prefix');
  content.appendChild(prefix);

  // 政策链接
  notices.forEach((notice, index) => {
    if (index > 0) {
      const separator = document.createElement('span');
      separator.setAttribute('data-i18n', 'policy.publicNotice.separator');
      separator.textContent = t('policy.publicNotice.separator');
      content.appendChild(separator);
    }
    const link = document.createElement('a');
    const nameKey = policyNameKeys[notice.policy_type] || notice.policy_type;
    link.setAttribute('data-i18n', nameKey);
    link.textContent = t(nameKey);
    link.href = `/policy#${notice.policy_type}/public-notice-period`;
    content.appendChild(link);
  });

  // 后缀
  const suffix = document.createElement('span');
  suffix.setAttribute('data-i18n', 'policy.publicNotice.suffix');
  suffix.textContent = t('policy.publicNotice.suffix');
  content.appendChild(suffix);
}
