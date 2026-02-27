/**
 * modules/policy/assets/js/policy.ts
 * Policy SPA 模块
 *
 * 功能：
 * - Hash 路由切换政策页面
 * - 动态加载政策内容
 * - AI 聊天助手
 * - 支持扩展新政策类型
 */

import { initLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.ts';
import { initAIChat, updateAIChatLanguage } from './ai-chat.ts';

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
  [key: string]: PolicyContent;
}

// 支持的政策类型（可扩展）
type PolicyType = 'privacy' | 'terms' | 'cookies' | string;

// ==================== 构建时注入的数据 ====================

declare const __POLICY_DATA__: PolicyData | undefined;

// ==================== 状态管理 ====================

let policyData: PolicyData | null = typeof __POLICY_DATA__ !== 'undefined' ? __POLICY_DATA__ : null;
let currentPolicy: PolicyType = 'privacy';

// ==================== 数据加载 ====================

async function loadPolicyData(): Promise<PolicyData | null> {
  // 生产环境：使用构建时注入的数据
  if (policyData) return policyData;

  // 开发环境：动态加载
  try {
    const response = await fetch('/shared/i18n/policy/policy.json');
    if (!response.ok) throw new Error('Failed to load policy data');
    policyData = await response.json();
    return policyData;
  } catch (error) {
    console.error('[POLICY] Failed to load data:', (error as Error).message);
    return null;
  }
}

function getPolicyContent(type: PolicyType): PolicyContent | null {
  return policyData?.[type] || null;
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

function renderPolicy(type: PolicyType): void {
  const content = getPolicyContent(type);
  const container = document.querySelector('.policy-container');
  if (!container) return;

  // 政策不存在时显示提示
  if (!content) {
    container.innerHTML = `
      <div class="policy-not-found">
        <h1>政策未找到</h1>
        <p>请求的政策页面不存在或正在建设中。</p>
        <a href="#privacy" class="policy-back-link">返回隐私政策</a>
      </div>
    `;
    return;
  }

  // 构建 HTML
  let html = `<h1 class="policy-title">${content.title}</h1>`;
  
  // 元信息
  html += `<p class="policy-meta">${content.effectiveDate}`;
  if (content.publisher) html += `<br>${content.publisher}`;
  if (content.contactEmail) html += `<br>${content.contactEmail}`;
  html += `</p>`;

  // 渲染章节
  if (content.sections?.length) {
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

  // 渲染页脚
  if (content.footer) {
    html += `
      <section class="policy-section policy-contact">
        <p><strong>${content.footer.lastUpdated}</strong></p>
        ${content.footer.statement ? `<p>${content.footer.statement}</p>` : ''}
        ${content.footer.notice ? `<p><em>${content.footer.notice}</em></p>` : ''}
      </section>
    `;
  }

  html += `<footer class="policy-footer">${window.t('footer.copyright')}</footer>`;

  // 添加淡入动画
  container.classList.remove('fade-in');
  void (container as HTMLElement).offsetWidth; // 触发 reflow
  container.classList.add('fade-in');
  container.innerHTML = html;
}

// ==================== 路由处理 ====================

function handleRouteChange(): void {
  const policy = getHashRoute();
  
  // 避免重复渲染
  if (policy === currentPolicy && document.querySelector('.policy-title')) return;
  
  currentPolicy = policy;
  updateNavActive(policy);
  renderPolicy(policy);
  
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
    await loadPolicyData();
    
    // 初始渲染
    handleRouteChange();
    
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

    // 初始化 AI 聊天助手
    initAIChat();

    hidePageLoader();
    updatePageTitle();
    
    initLanguageSwitcher(() => {
      updatePageTitle();
      renderPolicy(currentPolicy);
      updateAIChatLanguage();
    });

  } catch (error) {
    console.error('[POLICY] Init failed:', (error as Error).message);
    hidePageLoader();
  }
}

document.addEventListener('DOMContentLoaded', init);
