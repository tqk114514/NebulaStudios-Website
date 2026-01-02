/**
 * modules/policy/assets/js/policy.ts
 * Policy 页面通用逻辑
 *
 * 功能：
 * - 多语言支持
 * - 语言切换器
 * - 动态加载 policy 内容
 */

import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations, getCurrentLanguage } from '../../../../shared/js/utils/language-switcher.js';

// ==================== 类型定义 ====================

interface PolicySection {
  id: string;
  title: string;
  content: string;
}

interface PolicyFooter {
  lastUpdated: string;
  statement?: string;
  notice?: string;
}

interface PolicyContent {
  title: string;
  effectiveDate: string;
  publisher?: string;
  contactEmail?: string;
  sections?: PolicySection[];
  content?: string;
  footer?: PolicyFooter;
}

interface PolicyData {
  [lang: string]: {
    privacy?: PolicyContent;
    terms?: PolicyContent;
    cookies?: PolicyContent;
  };
}

// Policy 数据缓存
let policyData: PolicyData | null = null;

// ==================== 数据加载 ====================

/**
 * 加载 policy 翻译数据
 */
async function loadPolicyData(): Promise<PolicyData | null> {
  if (policyData) {return policyData;}

  try {
    const response = await fetch('/policy/data/i18n-policy.json');
    if (!response.ok) {throw new Error('Failed to load policy data');}
    policyData = await response.json();
    return policyData;
  } catch (error) {
    console.error('[POLICY] ERROR: Failed to load policy data:', (error as Error).message);
    return null;
  }
}

/**
 * 获取当前语言的 policy 数据
 */
function getPolicyContent(type: 'privacy' | 'terms' | 'cookies'): PolicyContent | null {
  if (!policyData) {return null;}

  const lang = getCurrentLanguage();
  // 优先使用当前语言，fallback 到中文
  const langData = policyData[lang] || policyData['zh-CN'];
  return langData?.[type] || null;
}

// ==================== 内容渲染 ====================

/**
 * 渲染隐私政策页面
 */
function renderPrivacyPage(): void {
  const content = getPolicyContent('privacy');
  if (!content) {return;}

  const container = document.querySelector('.policy-container');
  if (!container) {return;}

  // 构建 HTML
  let html = `
    <h1 class="policy-title">${content.title}</h1>
    <p class="policy-meta">${content.effectiveDate}<br>${content.publisher}<br>${content.contactEmail}</p>
  `;

  // 渲染各章节
  if (content.sections) {
    content.sections.forEach(section => {
      html += `
        <section class="policy-section" id="${section.id}">
          <h2>${section.title}</h2>
          ${section.content}
        </section>
      `;
    });
  }

  // 渲染页脚
  if (content.footer) {
    html += `
      <section class="policy-section policy-contact">
        <p><strong>${content.footer.lastUpdated}</strong></p>
        <p>${content.footer.statement}</p>
        <p><em>${content.footer.notice}</em></p>
      </section>
    `;
  }

  html += `<footer class="policy-footer">${window.t?.('footer.copyright') || '© 2025 Nebula Studios'}</footer>`;

  container.innerHTML = html;
}

/**
 * 渲染服务条款页面
 */
function renderTermsPage(): void {
  const content = getPolicyContent('terms');
  if (!content) {return;}

  const container = document.querySelector('.policy-container');
  if (!container) {return;}

  let html = `
    <h1 class="policy-title">${content.title}</h1>
    <p class="policy-meta">${content.effectiveDate}</p>
  `;

  if (content.sections) {
    content.sections.forEach(section => {
      html += `
        <section class="policy-section" id="${section.id}">
          <h2>${section.title}</h2>
          ${section.content}
        </section>
      `;
    });
  } else if (content.content) {
    html += `<section class="policy-section">${content.content}</section>`;
  }

  if (content.footer) {
    html += `
      <section class="policy-section policy-contact">
        <p>${content.footer.lastUpdated}</p>
        <p>${content.footer.statement || ''}</p>
      </section>
    `;
  }

  html += `<footer class="policy-footer">${window.t?.('footer.copyright') || '© 2025 Nebula Studios'}</footer>`;

  container.innerHTML = html;
}

/**
 * 渲染 Cookie 政策页面
 */
function renderCookiesPage(): void {
  const content = getPolicyContent('cookies');
  if (!content) {return;}

  const container = document.querySelector('.policy-container');
  if (!container) {return;}

  let html = `
    <h1 class="policy-title">${content.title}</h1>
    <p class="policy-meta">${content.effectiveDate}</p>
  `;

  if (content.sections) {
    content.sections.forEach(section => {
      html += `
        <section class="policy-section" id="${section.id}">
          <h2>${section.title}</h2>
          ${section.content}
        </section>
      `;
    });
  } else if (content.content) {
    html += `<section class="policy-section">${content.content}</section>`;
  }

  if (content.footer) {
    html += `
      <section class="policy-section policy-contact">
        <p>${content.footer.lastUpdated}</p>
      </section>
    `;
  }

  html += `<footer class="policy-footer">${window.t?.('footer.copyright') || '© 2025 Nebula Studios'}</footer>`;

  container.innerHTML = html;
}

/**
 * 根据当前页面渲染内容
 */
function renderCurrentPage(): void {
  const path = window.location.pathname;

  if (path.includes('/privacy')) {
    renderPrivacyPage();
  } else if (path.includes('/terms')) {
    renderTermsPage();
  } else if (path.includes('/cookies')) {
    renderCookiesPage();
  }
}

// ==================== 页面初始化 ====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();

    // 加载 policy 数据并渲染
    await loadPolicyData();
    renderCurrentPage();

    // 隐藏页面加载遮罩
    hidePageLoader();

    // 更新页面标题
    updatePageTitle();

    // 初始化语言切换器
    initLanguageSwitcher(() => {
      updatePageTitle();
      renderCurrentPage(); // 语言切换时重新渲染
    });

  } catch (error) {
    console.error('[POLICY] ERROR: Page initialization failed:', (error as Error).message);
    hidePageLoader();
  }
});
