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

import { initLanguageSwitcher, updatePageTitle, waitForTranslations, getCurrentLanguage } from '../../../../shared/js/utils/language-switcher.ts';
import { marked } from '../../../../shared/js/lib/markedjs-marked@18.0.5/src/marked.ts';
import DOMPurify from '../../../../shared/js/lib/cure53-DOMPurify@3.4.11/src/purify.ts';

// ==================== 类型定义 ====================

// 支持的政策类型（可扩展）
type PolicyType = 'privacy' | 'terms' | 'cookies' | string;

// ==================== 类型定义 ====================

// 政策版本元数据（与 /api/policy/versions 响应中每个文件条目对应）
// status 由服务器端基于当前时间计算，前端不自行判断时间
interface PolicyVersionMeta {
  update_date: string;
  effective_date: string;
  languages: string[];
  // effective（已生效）/ public_notice（公示期）/ scheduled（未进入公示期）
  status: 'effective' | 'public_notice' | 'scheduled';
}

interface LoadPolicyResult {
  markdown: string | null;
  isFallback: boolean;
  displayLang: string;
  displayVersion: string;
}

// ==================== 状态管理 ====================

// 政策版本结构（与 /api/policy/versions 响应镜像）：{ policyType: { filename: { update_date, effective_date, languages, status } } }
let policyVersions: Record<string, Record<string, PolicyVersionMeta>> = {};
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

// 获取政策的所有版本（按 effective_date 降序）
// 用于版本切换器选项填充，仅包含已生效版本（公示期版本不列入）
function getAllVersions(type: PolicyType): { version: string; meta: PolicyVersionMeta }[] {
  const versions = policyVersions[type];
  if (!versions) return [];

  return Object.entries(versions)
    .filter(([, meta]) => meta.status === 'effective')
    .map(([filename, meta]) => ({ version: filenameToVersion(filename), meta }))
    .sort((a, b) => b.meta.effective_date.localeCompare(a.meta.effective_date));
}

// ==================== 版本切换器 ====================

// 初始化版本切换器的开关交互（点击按钮 toggle、点击外部关闭、ESC 关闭）
function initVersionSwitcherToggle(): void {
  const switcher = document.querySelector('.version-switcher');
  const currentBtn = document.querySelector('.version-switcher .language-current');
  if (!switcher || !currentBtn) return;

  currentBtn.addEventListener('click', (e: Event) => {
    e.preventDefault();
    e.stopPropagation();
    const isOpen = switcher.classList.toggle('is-open');
    currentBtn.setAttribute('aria-expanded', String(isOpen));
  });

  document.addEventListener('click', (e: MouseEvent) => {
    if (!switcher.contains(e.target as Node)) {
      switcher.classList.remove('is-open');
      currentBtn.setAttribute('aria-expanded', 'false');
    }
  });

  document.addEventListener('keydown', (e: KeyboardEvent) => {
    if (e.key === 'Escape' && switcher.classList.contains('is-open')) {
      switcher.classList.remove('is-open');
      currentBtn.setAttribute('aria-expanded', 'false');
    }
  });
}

// 根据当前政策类型填充版本切换器选项
// 只有一个版本时隐藏切换器；多个版本时显示并填充选项
function updateVersionSwitcher(type: PolicyType, currentVersion: string): void {
  const switcher = document.querySelector('.version-switcher');
  const dropdown = document.querySelector('.version-switcher .language-dropdown');
  const textEl = document.querySelector('.version-switcher .lang-text');
  if (!switcher || !dropdown || !textEl) return;

  const allVersions = getAllVersions(type);

  // 只有一个版本时无需切换器
  if (allVersions.length <= 1) {
    switcher.classList.add('is-hidden');
    return;
  }

  switcher.classList.remove('is-hidden');

  // 填充选项（复用 .language-option 类名以套用 general.css 样式）
  dropdown.innerHTML = allVersions.map(({ version }) => `
    <button class="language-option ${version === currentVersion ? 'active' : ''}" data-version="${version}" role="option" aria-selected="${version === currentVersion}">
      <span>${version}</span>
      <svg class="check-icon" width="14" height="14" viewBox="0 0 14 14" fill="none">
        <path d="M2.5 7L5.5 10L11.5 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
    </button>
  `).join('');

  // 更新当前显示文本
  textEl.textContent = currentVersion;

  // 绑定选项点击事件
  dropdown.querySelectorAll('.language-option').forEach(option => {
    option.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      const version = option.getAttribute('data-version');
      if (!version) return;

      switcher.classList.remove('is-open');
      (document.querySelector('.version-switcher .language-current') as HTMLElement)?.setAttribute('aria-expanded', 'false');

      // 通过更新 hash 触发路由变化，由 handleRouteChange 统一处理
      window.location.hash = `${type}/${version}`;
    });
  });
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

// 从 manifest 中的文件名键提取版本号（去掉 .md 后缀）
function filenameToVersion(filename: string): string {
  return filename.replace(/\.md$/, '');
}

// 获取指定语言下 effective_date 最大的已生效版本条目
// 仅返回 status === 'effective' 且 languages 包含指定语言的版本
// 返回 { version, meta } 或 null（无文件或无该语言翻译时）
function getLatestEntryForLang(type: PolicyType, lang: string): { version: string; meta: PolicyVersionMeta } | null {
  const versions = policyVersions[type];
  if (!versions) return null;

  let latestVersion = '';
  let latestMeta: PolicyVersionMeta | null = null;
  for (const filename in versions) {
    const meta = versions[filename];
    // 仅考虑已生效且包含指定语言翻译的版本
    if (meta.status !== 'effective') continue;
    if (!meta.languages.includes(lang)) continue;
    if (!latestMeta || meta.effective_date > latestMeta.effective_date) {
      latestMeta = meta;
      latestVersion = filenameToVersion(filename);
    }
  }
  return latestMeta ? { version: latestVersion, meta: latestMeta } : null;
}

// 获取政策的最新版本号（所有已生效版本中 effective_date 最大的，不限语言）
function getLatestVersion(type: PolicyType): string {
  const versions = policyVersions[type];
  if (!versions) return '';

  let latestVersion = '';
  let latestEffectiveDate = '';
  for (const filename in versions) {
    const meta = versions[filename];
    if (meta.status !== 'effective') continue;
    if (meta.effective_date > latestEffectiveDate) {
      latestEffectiveDate = meta.effective_date;
      latestVersion = filenameToVersion(filename);
    }
  }
  return latestVersion;
}

// 加载政策 Markdown 文件（带版本回退逻辑）
// specifiedVersion 为 null/undefined 时加载最新生效版本，否则加载指定版本
async function loadPolicyMarkdown(type: PolicyType, specifiedVersion?: string | null): Promise<LoadPolicyResult> {
  const currentLang = getCurrentLanguage();
  const cacheKey = specifiedVersion ? `${type}:${currentLang}:${specifiedVersion}` : `${type}:${currentLang}`;

  if (policyCache[cacheKey]) {
    return policyCache[cacheKey];
  }

  if (!policyVersions[type]) {
    return { markdown: null, isFallback: false, displayLang: '', displayVersion: '' };
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

  // 指定版本：直接按回退顺序加载该版本
  if (specifiedVersion) {
    const filename = `${specifiedVersion}.md`;
    const meta = policyVersions[type][filename];
    if (!meta) {
      return { markdown: null, isFallback: false, displayLang: '', displayVersion: '' };
    }

    let result: LoadPolicyResult | null = null;

    // 规则1：当前语言有该版本的翻译
    if (meta.languages.includes(currentLang)) {
      const markdown = await tryLoad(currentLang, specifiedVersion);
      if (markdown) {
        result = { markdown, isFallback: false, displayLang: currentLang, displayVersion: specifiedVersion };
      }
    }

    // 规则2：zh-CN 有该版本的翻译
    if (!result && meta.languages.includes('zh-CN')) {
      const markdown = await tryLoad('zh-CN', specifiedVersion);
      if (markdown) {
        result = { markdown, isFallback: true, displayLang: 'zh-CN', displayVersion: specifiedVersion };
      }
    }

    // 规则3：该版本的其他语言翻译
    if (!result) {
      for (const lang of meta.languages) {
        if (lang === currentLang || lang === 'zh-CN') continue;
        const markdown = await tryLoad(lang, specifiedVersion);
        if (markdown) {
          result = { markdown, isFallback: true, displayLang: lang, displayVersion: specifiedVersion };
          break;
        }
      }
    }

    if (result) {
      policyCache[cacheKey] = result;
      return result;
    }

    return { markdown: null, isFallback: false, displayLang: '', displayVersion: '' };
  }

  // 未指定版本：加载最新生效版本（按 effective_date 最大）
  const latestVersion = getLatestVersion(type);
  if (!latestVersion) {
    return { markdown: null, isFallback: false, displayLang: '', displayVersion: '' };
  }

  let markdown: string | null = null;
  let isFallback = false;
  let displayLang = '';
  let displayVersion = '';

  // 规则1：检查当前语言版本是否等于最新版本
  const currentLangEntry = getLatestEntryForLang(type, currentLang);
  if (currentLangEntry && currentLangEntry.version === latestVersion) {
    markdown = await tryLoad(currentLang, currentLangEntry.version);
    if (markdown) {
      displayLang = currentLang;
      displayVersion = currentLangEntry.version;
    }
  }

  // 规则2：如果规则1失败，尝试使用 zh-CN
  if (!markdown) {
    const zhCnEntry = getLatestEntryForLang(type, 'zh-CN');
    if (zhCnEntry) {
      markdown = await tryLoad('zh-CN', zhCnEntry.version);
      if (markdown) {
        isFallback = true;
        displayLang = 'zh-CN';
        displayVersion = zhCnEntry.version;
      }
    }
  }

  // 规则3：如果规则2也失败，尝试最新版本的任意语言翻译
  if (!markdown) {
    const meta = policyVersions[type][`${latestVersion}.md`];
    if (meta) {
      for (const lang of meta.languages) {
        if (lang === currentLang || lang === 'zh-CN') continue;
        const md = await tryLoad(lang, latestVersion);
        if (md) {
          markdown = md;
          isFallback = true;
          displayLang = lang;
          displayVersion = latestVersion;
          break;
        }
      }
    }
  }

  const result: LoadPolicyResult = { markdown, isFallback, displayLang, displayVersion };
  if (markdown) {
    policyCache[cacheKey] = result;
  }

  return result;
}

// ==================== 版本信息 ====================

interface VersionInfo {
  serverCommit: string;
  repoCommit: string;
}

let cachedVersionInfo: VersionInfo | null = null;

async function fetchVersionInfo(): Promise<VersionInfo | null> {
  if (cachedVersionInfo) return cachedVersionInfo;

  try {
    const response = await fetch('/api/version');
    if (!response.ok) return null;
    const data = await response.json();
    if (data.success && data.data) {
      cachedVersionInfo = data.data as VersionInfo;
      return cachedVersionInfo;
    }
    return null;
  } catch {
    return null;
  }
}

function createVersionElement(info: VersionInfo): string {
  const t = (window as any).t || ((k: string) => k);
  const same = info.serverCommit === info.repoCommit;
  const pendingHint = same ? '' : `，${t('policy.versionPending')}`;

  return `<div class="version-info">
    <p>${t('policy.versionServer')}：${info.serverCommit}，${t('policy.versionRepo')}：${info.repoCommit}${pendingHint}，${t('policy.versionLag')}</p>
  </div>`;
}

// ==================== 公示期政策 ====================

// 公示期版本信息（从 policyVersions 的 status 字段派生，无需额外 API 调用）
interface PublicNoticeEntry {
  version: string;
  meta: PolicyVersionMeta;
}

// 获取指定政策类型的公示期版本（从 policyVersions 的 status 字段判断）
// 服务器端已在 /api/policy/versions 响应中标记 status，前端直接读取
function getPublicNoticeVersion(type: PolicyType): PublicNoticeEntry | null {
  const versions = policyVersions[type];
  if (!versions) return null;
  for (const filename in versions) {
    const meta = versions[filename];
    if (meta.status === 'public_notice') {
      return { version: filenameToVersion(filename), meta };
    }
  }
  return null;
}

// ==================== 路由管理 ====================

// 解析 hash 路由：{policy}[/{version}]
// 例：#privacy → 最新生效版；#privacy/2025-12-18 → 指定历史版本；#privacy/public-notice-period → 公示期版本
function parseHashRoute(): { policy: PolicyType; version: string | null } {
  const hash = window.location.hash.slice(1); // 去掉 #
  if (!hash) return { policy: 'privacy', version: null };

  const parts = hash.split('/');
  const policy = parts[0] as PolicyType;
  const version = parts[1] || null;
  return { policy, version };
}

function navigateTo(policy: PolicyType): void {
  // 保持当前版本（由 loadPolicyMarkdown 的语言回退逻辑处理）
  const { version } = parseHashRoute();
  window.location.hash = version ? `${policy}/${version}` : policy;
}

function updateNavActive(policy: PolicyType): void {
  document.querySelectorAll('.policy-nav-item').forEach(item => {
    const itemPolicy = item.getAttribute('data-policy');
    item.classList.toggle('active', itemPolicy === policy);
  });
}

// ==================== 内容渲染 ====================

// 显示公示期横幅（与正文一同出现，初始隐藏状态在 init 中设置）
function showNoticeBanner(): void {
  const noticeBanner = document.querySelector('.notice-banner');
  if (noticeBanner) {
    noticeBanner.classList.remove('is-hidden');
  }
}

// 隐藏公示期横幅（加载动画显示期间隐藏）
function hideNoticeBanner(): void {
  const noticeBanner = document.querySelector('.notice-banner');
  if (noticeBanner) {
    noticeBanner.classList.add('is-hidden');
  }
}

// 政策类型 → i18n 键映射（用于公示期横幅）
const policyNameKeys: Record<string, string> = {
  privacy: 'policy.privacyPolicy',
  terms: 'policy.termsOfService',
  cookies: 'policy.cookiePolicy',
};

// 创建公示期横幅（初始隐藏，内容为空，由 updateNoticeBanner 填充）
function createNoticeBanner(): HTMLElement {
  const banner = document.createElement('div');
  banner.className = 'notice-banner is-hidden';

  const content = document.createElement('div');
  content.className = 'notice-banner__content';
  banner.appendChild(content);

  return banner;
}

// 按当前政策类型更新横幅内容
// - 正在查看公示期版本时隐藏横幅（已在看，无需再提示）
// - 当前政策类型无公示期版本时隐藏横幅（display: none）
// - 有公示期版本时填充内容并恢复 display
// - 实际可见性由 is-hidden 类控制（加载动画期间隐藏）
function updateNoticeBanner(policyType: PolicyType, isViewingPublicNotice: boolean = false): void {
  const noticeBanner = document.querySelector('.notice-banner') as HTMLElement | null;
  if (!noticeBanner) return;

  // 正在查看公示期版本时不显示横幅
  if (isViewingPublicNotice) {
    noticeBanner.style.display = 'none';
    return;
  }

  const content = noticeBanner.querySelector('.notice-banner__content');
  if (!content) return;

  // 检查当前政策类型是否有公示期版本
  const publicNotice = getPublicNoticeVersion(policyType);
  if (!publicNotice) {
    noticeBanner.style.display = 'none';
    return;
  }

  // 填充内容
  content.innerHTML = '';
  const t: (key: string) => string = (window as any).t ?? ((k: string): string => k);

  const prefix = document.createElement('span');
  prefix.setAttribute('data-i18n', 'policy.publicNotice.prefix');
  prefix.textContent = t('policy.publicNotice.prefix');
  content.appendChild(prefix);

  const link = document.createElement('a');
  const nameKey = policyNameKeys[policyType] || policyType;
  link.setAttribute('data-i18n', nameKey);
  link.textContent = t(nameKey);
  link.href = `/policy#${policyType}/public-notice-period`;
  content.appendChild(link);

  const suffix = document.createElement('span');
  suffix.setAttribute('data-i18n', 'policy.publicNotice.suffix');
  suffix.textContent = t('policy.publicNotice.suffix');
  content.appendChild(suffix);

  noticeBanner.style.display = '';
}

async function renderPolicy(
  type: PolicyType,
  specifiedVersion?: string | null,
  publicNotice?: PublicNoticeEntry | null
): Promise<void> {
  const container = document.querySelector('.policy-container');
  const loadingEl = container?.querySelector('.policy-loading');
  const contentEl = container?.querySelector('.policy-content');
  if (!container || !loadingEl || !contentEl) return;

  const currentLang = getCurrentLanguage();
  const cacheKey = specifiedVersion ? `${type}:${currentLang}:${specifiedVersion}` : `${type}:${currentLang}`;

  // 先检查缓存，如果没有缓存则显示加载动画
  const hasCache = !!policyCache[cacheKey];
  if (!hasCache) {
    loadingEl.classList.remove('is-hidden');
    contentEl.classList.remove('is-visible');
    // 隐藏公示期横幅（加载动画显示期间不可见）
    hideNoticeBanner();
  }

  const result = await loadPolicyMarkdown(type, specifiedVersion);

  if (!result.markdown) {
    loadingEl.classList.add('is-hidden');
    contentEl.classList.remove('is-visible');
    contentEl.innerHTML = `<div class="policy-not-found"><h1>404</h1></div>`;
    contentEl.classList.add('is-visible');
    // 显示公示期横幅（与正文一同出现）
    showNoticeBanner();
    return;
  }

  // 使用 marked.js 转换 Markdown 为 HTML
  let html = marked.parse(result.markdown) as string;

  // 使用 DOMPurify 净化 HTML
  html = DOMPurify.sanitize(html);

  // 语言回退提示（先加，显示在下方）
  if (result.isFallback) {
    const t = (window as any).t;
    if (t) {
      const policyName = getPolicyDisplayName(type);
      const langName = LANG_NAMES[currentLang] || currentLang;
      const displayLangName = LANG_NAMES[result.displayLang] || result.displayLang;
      const fallbackMessage = t('policy.versionFallback');
      const formattedMessage = fallbackMessage
        .replace('{policy}', policyName)
        .replace('{version}', result.displayVersion)
        .replace('{lang}', langName)
        .replace('{displayLang}', displayLangName);

      const warningDiv = `<div class="notice-banner">${formattedMessage}</div>`;
      html = warningDiv + html;
    }
  }

  // 顶部提示横幅（后加，显示在上方）
  // 公示期版本：显示公示期提示；历史版本：显示历史版本提示；最新生效版：无提示
  const t = (window as any).t;
  if (publicNotice && t) {
    const noticeHtml = `<div class="notice-banner">${t('policy.publicNoticePeriod')}（${publicNotice.version}）<a href="#${type}">${t('policy.historyLatest')}</a></div>`;
    html = noticeHtml + html;
  } else {
    const latestVersion = getLatestVersion(type);
    if (specifiedVersion && latestVersion && specifiedVersion !== latestVersion && t) {
      const noticeHtml = `<div class="notice-banner">${t('policy.historyNotice')}（${specifiedVersion}）<a href="#${type}">${t('policy.historyLatest')}</a></div>`;
      html = noticeHtml + html;
    }
  }

  // 添加淡入动画
  contentEl.classList.remove('fade-in');
  void (contentEl as HTMLElement).offsetWidth; // 触发 reflow
  contentEl.classList.add('fade-in');
  contentEl.innerHTML = html;

  // 仅最新生效版的隐私政策显示服务器/代码库版本信息（占位符，异步加载不阻塞政策显示）
  // 公示期版本和历史版本不显示
  if (type === 'privacy' && !specifiedVersion) {
    contentEl.innerHTML += '<div class="version-info-loading"><div class="loader-spinner"></div></div>';
  }

  // 隐藏加载动画，显示内容
  loadingEl.classList.add('is-hidden');
  contentEl.classList.add('is-visible');
  // 显示公示期横幅（与正文一同出现）
  showNoticeBanner();

  // 异步加载版本信息，替换占位符
  if (type === 'privacy' && !specifiedVersion) {
    const versionInfo = await fetchVersionInfo();
    const placeholder = contentEl.querySelector('.version-info-loading');
    if (placeholder) {
      if (versionInfo) {
        placeholder.outerHTML = createVersionElement(versionInfo);
      } else {
        placeholder.remove();
      }
    }
  }
}

// ==================== 路由处理 ====================

// 当前路由键，用于避免重复渲染
let currentRouteKey = '';

async function handleRouteChange(): Promise<void> {
  let { policy, version } = parseHashRoute();

  // 公示期路由：#privacy/public-notice-period
  // 解析为实际的公示期版本号，传给 renderPolicy 显示公示期提示
  let publicNotice: PublicNoticeEntry | null = null;
  if (version === 'public-notice-period') {
    publicNotice = getPublicNoticeVersion(policy);
    if (publicNotice) {
      version = publicNotice.version;
    } else {
      // 当前无公示期版本，回退到最新生效版
      version = null;
      history.replaceState(null, '', `#${policy}`);
    }
  }

  // 如果指定版本等于最新生效版本，清除版本号
  // #privacy 就代表最新生效版，无需冗余带版本号
  if (version) {
    const latestVersion = getLatestVersion(policy);
    if (latestVersion && version === latestVersion) {
      version = null;
      history.replaceState(null, '', `#${policy}`);
    }
  }

  // 避免重复渲染（包含语言和公示期标记，确保切换语言或进入公示期时重新渲染）
  const currentLang = getCurrentLanguage();
  const routeKey = `${policy}:${version || 'latest'}:${currentLang}:${publicNotice ? 'notice' : 'normal'}`;
  if (routeKey === currentRouteKey && document.querySelector('.policy-content.is-visible')) return;
  currentRouteKey = routeKey;

  updateNavActive(policy);

  // 按当前政策类型更新公示期横幅内容
  // 正在查看公示期版本时隐藏横幅；cookies 页无公示期政策会自动隐藏
  updateNoticeBanner(policy, !!publicNotice);

  // 确定要显示的版本（用于版本切换器高亮）
  const allVersions = getAllVersions(policy);
  const displayVersion = version || (allVersions.length > 0 ? allVersions[0].version : '');

  // 更新版本切换器
  updateVersionSwitcher(policy, displayVersion);

  await renderPolicy(policy, version, publicNotice);

  // 滚动到顶部
  window.scrollTo({ top: 0, behavior: 'smooth' });
}



// ==================== 初始化 ====================

async function init(): Promise<void> {
  try {
    await waitForTranslations();

    // 加载政策版本列表
    await loadPolicyVersions();

    // 创建公示期横幅（插入到加载动画下方，初始隐藏）
    const policyLoading = document.querySelector('.policy-loading');
    if (policyLoading) {
      const banner = createNoticeBanner();
      policyLoading.insertAdjacentElement('afterend', banner);
    }

    // 初始化版本切换器开关交互
    initVersionSwitcherToggle();

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
