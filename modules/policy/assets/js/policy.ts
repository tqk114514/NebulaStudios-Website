/**
 * modules/policy/assets/js/policy.ts
 * Policy SPA 模块
 *
 * 功能：
 * - Hash 路由切换政策页面
 * - 动态加载 Markdown 政策内容
 * - 使用 marked.js 和 DOMPurify 渲染和净化
 * - 支持扩展新政策类型
 */

import { initLanguageSwitcher, updatePageTitle, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';

declare const marked: {
  parse: (markdown: string) => string;
};

declare const DOMPurify: {
  sanitize: (html: string) => string;
};

// ==================== 类型定义 ====================

// 支持的政策类型（可扩展）
type PolicyType = 'privacy' | 'terms' | 'cookies' | string;

// ==================== 类型定义 ====================

interface LoadPolicyResult {
  markdown: string | null;
  isFallback: boolean;
}

// ==================== 状态管理 ====================

let currentPolicy: PolicyType = 'privacy';
// 政策版本结构：{ policyType: { lang: [versions] } }
let policyVersions: Record<string, Record<string, string[]>> = {};
// 缓存键：{policyType}:{lang}
let policyCache: Record<string, LoadPolicyResult> = {};

// 语言显示名称映射
const LANG_NAMES: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  'en': 'English',
  'ja': '日本語',
  'ko': '한국어'
};

// 政策显示名称映射（从翻译获取）
function getPolicyDisplayName(type: PolicyType): string {
  const t = (window as any).t;
  if (!t) return type;
  const key = `policy.${type === 'privacy' ? 'privacyPolicy' : type === 'terms' ? 'termsOfService' : 'cookiePolicy'}`;
  return t(key) || type;
}

// ==================== 数据加载 ====================

async function loadPolicyVersions(): Promise<void> {
  try {
    const response = await fetch('/api/policy/versions');
    if (!response.ok) throw new Error('Failed to load policy versions');
    const data = await response.json();
    if (data.success && data.data) {
      policyVersions = data.data;
    }
  } catch (error) {
    console.error('[POLICY] Failed to load policy versions:', (error as Error).message);
  }
}

// 获取政策的最新版本号（所有语言中的最高版本）
function getLatestVersion(type: PolicyType): string {
  if (!policyVersions[type]) return '';
  
  let latestVersion = '';
  for (const lang in policyVersions[type]) {
    const versions = policyVersions[type][lang];
    if (versions && versions.length > 0 && versions[0] > latestVersion) {
      latestVersion = versions[0];
    }
  }
  return latestVersion;
}

// 加载政策 Markdown 文件（带版本回退逻辑）
async function loadPolicyMarkdown(type: PolicyType): Promise<LoadPolicyResult> {
  const currentLang = (window as any).currentLanguage || 'zh-CN';
  const cacheKey = `${type}:${currentLang}`;
  
  if (policyCache[cacheKey]) {
    return policyCache[cacheKey];
  }
  
  if (!policyVersions[type]) {
    return { markdown: null, isFallback: false };
  }
  
  const latestVersion = getLatestVersion(type);
  if (!latestVersion) {
    return { markdown: null, isFallback: false };
  }
  
  // 尝试加载文件的辅助函数
  const tryLoad = async (lang: string, version: string): Promise<string | null> => {
    try {
      const response = await fetch(`/shared/i18n/policy/${type}/${lang}/${version}.md`);
      if (!response.ok) return null;
      return await response.text();
    } catch {
      return null;
    }
  };
  
  let markdown: string | null = null;
  let isFallback = false;
  
  // 规则1：检查当前语言版本是否等于最新版本
  if (policyVersions[type][currentLang] && policyVersions[type][currentLang].length > 0) {
    const currentLangVersion = policyVersions[type][currentLang][0];
    if (currentLangVersion === latestVersion) {
      markdown = await tryLoad(currentLang, currentLangVersion);
    }
  }
  
  // 规则2：如果规则1失败，尝试使用 zh-CN
  if (!markdown && policyVersions[type]['zh-CN'] && policyVersions[type]['zh-CN'].length > 0) {
    const zhCnVersion = policyVersions[type]['zh-CN'][0];
    markdown = await tryLoad('zh-CN', zhCnVersion);
    if (markdown) {
      isFallback = true;
    }
  }
  
  // 规则3：如果规则2也失败，尝试找到有最新版本的任意语言
  if (!markdown) {
    for (const lang in policyVersions[type]) {
      const versions = policyVersions[type][lang];
      if (versions && versions.length > 0 && versions[0] === latestVersion) {
        markdown = await tryLoad(lang, latestVersion);
        if (markdown) {
          isFallback = true;
          break;
        }
      }
    }
  }
  
  const result: LoadPolicyResult = { markdown, isFallback };
  if (markdown) {
    policyCache[cacheKey] = result;
  }
  
  return result;
}

// ==================== 路由管理 ====================

function getHashRoute(): PolicyType {
  const hash = window.location.hash.slice(1); // 去掉 #
  return hash || 'privacy'; // 默认显示隐私政策
}

function navigateTo(policy: PolicyType): void {
  window.location.hash = policy;
}

function updateNavActive(policy: PolicyType): void {
  document.querySelectorAll('.policy-nav-item').forEach(item => {
    const itemPolicy = item.getAttribute('data-policy');
    item.classList.toggle('active', itemPolicy === policy);
  });
}

// ==================== 内容渲染 ====================

async function renderPolicy(type: PolicyType): Promise<void> {
  const container = document.querySelector('.policy-container');
  const loadingEl = container?.querySelector('.policy-loading');
  const contentEl = container?.querySelector('.policy-content');
  if (!container || !loadingEl || !contentEl) return;

  const currentLang = (window as any).currentLanguage || 'zh-CN';
  const cacheKey = `${type}:${currentLang}`;

  // 先检查缓存，如果没有缓存则显示加载动画
  const hasCache = !!policyCache[cacheKey];
  if (!hasCache) {
    loadingEl.classList.remove('is-hidden');
    contentEl.classList.remove('is-visible');
  }

  const result = await loadPolicyMarkdown(type);

  if (!result.markdown) {
    loadingEl.classList.add('is-hidden');
    contentEl.classList.remove('is-visible');
    contentEl.innerHTML = `<div class="policy-not-found"><h1>404</h1></div>`;
    contentEl.classList.add('is-visible');
    return;
  }

  // 使用 marked.js 转换 Markdown 为 HTML
  let html = marked.parse(result.markdown) as string;

  // 使用 DOMPurify 净化 HTML
  html = DOMPurify.sanitize(html);

  // 如果是回退显示，添加提示信息
  if (result.isFallback) {
    const t = (window as any).t;
    if (t) {
      const policyName = getPolicyDisplayName(type);
      const langName = LANG_NAMES[currentLang] || currentLang;
      const fallbackMessage = t('policy.versionFallback');
      const formattedMessage = fallbackMessage
        .replace('{policy}', policyName)
        .replace('{lang}', langName);
      
      const warningDiv = `<div class="policy-fallback-warning" style="padding: 16px; margin-bottom: 24px; background: var(--dim); border: 1px solid var(--line); font-family: var(--font-mono); font-size: var(--text-md); letter-spacing: 0.12em; color: var(--fg);">${formattedMessage}</div>`;
      html = warningDiv + html;
    }
  }

  // 添加淡入动画
  contentEl.classList.remove('fade-in');
  void (contentEl as HTMLElement).offsetWidth; // 触发 reflow
  contentEl.classList.add('fade-in');
  contentEl.innerHTML = html;

  // 隐藏加载动画，显示内容
  loadingEl.classList.add('is-hidden');
  contentEl.classList.add('is-visible');
}

// ==================== 路由处理 ====================

async function handleRouteChange(): Promise<void> {
  const policy = getHashRoute();

  // 避免重复渲染
  if (policy === currentPolicy && document.querySelector('.policy-title')) return;

  currentPolicy = policy;
  updateNavActive(policy);
  await renderPolicy(policy);

  // 滚动到顶部
  window.scrollTo({ top: 0, behavior: 'smooth' });
}



// ==================== 初始化 ====================

async function init(): Promise<void> {
  try {
    await waitForTranslations();

    // 加载政策版本列表
    await loadPolicyVersions();

    // 初始渲染
    await handleRouteChange();

    // 监听 hash 变化
    window.addEventListener('hashchange', handleRouteChange);

    // 导航点击事件（阻止默认行为，使用 SPA 路由）
    document.querySelectorAll('.policy-nav-item').forEach(item => {
      item.addEventListener('click', (e) => {
        e.preventDefault();
        const policy = item.getAttribute('data-policy');
        if (policy) navigateTo(policy);
      });
    });

    updatePageTitle();

    initLanguageSwitcher(() => {
      updatePageTitle();
      handleRouteChange();
    });

  } catch (error) {
    console.error('[POLICY] Init failed:', (error as Error).message);
  }
}

document.addEventListener('DOMContentLoaded', init);
