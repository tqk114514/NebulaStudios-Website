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

import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';

declare const marked: {
  parse: (markdown: string) => string;
};

declare const DOMPurify: {
  sanitize: (html: string) => string;
};

// ==================== 类型定义 ====================

// 支持的政策类型（可扩展）
type PolicyType = 'privacy' | 'terms' | 'cookies' | string;

// ==================== 状态管理 ====================

let currentPolicy: PolicyType = 'privacy';
let policyVersions: Record<string, string[]> = {};

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

async function loadPolicyMarkdown(type: PolicyType): Promise<string | null> {
  try {
    let version = '2025-12-18';
    if (policyVersions[type] && policyVersions[type].length > 0) {
      version = policyVersions[type][0];
    }
    const response = await fetch(`/shared/i18n/policy/${type}/${version}.md`);
    if (!response.ok) throw new Error('Failed to load policy markdown');
    return await response.text();
  } catch (error) {
    console.error('[POLICY] Failed to load markdown:', (error as Error).message);
    return null;
  }
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
  if (!container) return;

  // 显示加载中
  container.innerHTML = `
    <div class="policy-loading">
      <div class="loader-spinner"></div>
      <p>加载中...</p>
    </div>
  `;

  const markdown = await loadPolicyMarkdown(type);

  // 政策不存在时显示提示
  if (!markdown) {
    container.innerHTML = `
      <div class="policy-not-found">
        <h1>政策未找到</h1>
        <p>请求的政策页面不存在或正在建设中。</p>
        <a href="#privacy" class="policy-back-link">返回隐私政策</a>
      </div>
    `;
    return;
  }

  // 使用 marked.js 转换 Markdown 为 HTML
  let html = marked.parse(markdown) as string;

  // 使用 DOMPurify 净化 HTML
  html = DOMPurify.sanitize(html);

  // 添加淡入动画
  container.classList.remove('fade-in');
  void (container as HTMLElement).offsetWidth; // 触发 reflow
  container.classList.add('fade-in');
  container.innerHTML = html;
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

// ==================== 滚动隐藏 Header ====================

function initScrollBehavior(): void {
  let lastScrollY = 0;
  let ticking = false;
  const threshold = 50; // 滚动阈值，避免过于敏感
  const progressBar = document.querySelector('.reading-progress') as HTMLElement | null;

  const updateProgress = (): void => {
    if (!progressBar) return;

    const scrollHeight = document.documentElement.scrollHeight - window.innerHeight;
    const progress = scrollHeight > 0 ? (window.scrollY / scrollHeight) * 100 : 0;
    progressBar.style.width = `${Math.min(100, progress)}%`;
  };

  const handleScroll = (): void => {
    const currentScrollY = window.scrollY;

    // 更新进度条
    updateProgress();

    // 在顶部时始终显示 header
    if (currentScrollY < threshold) {
      document.body.classList.remove('header-hidden');
      lastScrollY = currentScrollY;
      return;
    }

    // 向下滚动：隐藏 header
    if (currentScrollY > lastScrollY) {
      document.body.classList.add('header-hidden');
    }
    // 向上滚动：显示 header
    else if (currentScrollY < lastScrollY) {
      document.body.classList.remove('header-hidden');
    }

    lastScrollY = currentScrollY;
  };

  window.addEventListener('scroll', () => {
    if (!ticking) {
      requestAnimationFrame(() => {
        handleScroll();
        ticking = false;
      });
      ticking = true;
    }
  }, { passive: true });

  // 初始化进度
  updateProgress();
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

    // 初始化滚动隐藏 header 行为
    initScrollBehavior();

    hidePageLoader();
    updatePageTitle();

    initLanguageSwitcher(() => {
      updatePageTitle();
      handleRouteChange();
    });

  } catch (error) {
    console.error('[POLICY] Init failed:', (error as Error).message);
    hidePageLoader();
  }
}

document.addEventListener('DOMContentLoaded', init);
