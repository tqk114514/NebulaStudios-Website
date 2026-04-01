/**
 * 国际化翻译模块
 *
 * 功能：
 * - 多语言支持（5 种语言：简繁英日韩）
 * - 所有翻译内嵌（构建时合并）
 * - 页面元素自动翻译（data-i18n 属性）
 * - 语言偏好持久化（Cookie）
 * - 防止内容闪烁（FOUC）
 */

// ==================== 类型定义 ====================

/** 翻译数据类型 */
type TranslationData = Record<string, string>;

/** 翻译数据集合类型 */
type TranslationsMap = Record<string, TranslationData>;

/** 支持的语言信息 */
interface LanguageInfo {
  code: string;
  label: string;
}

/** 构建时注入的翻译数据 */
declare const __ALL_TRANSLATIONS__: TranslationsMap | undefined;

// ==================== 数据存储 ====================

/** 翻译数据（构建时注入 __ALL_TRANSLATIONS__） */
const translations: TranslationsMap = typeof __ALL_TRANSLATIONS__ !== 'undefined' ? __ALL_TRANSLATIONS__ : {};

/** 支持的语言列表（简繁英日韩） */
export const supportedLanguages: LanguageInfo[] = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' }
];

/** 语言显示名称映射（方便快速查找） */
export const LANG_NAMES: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  'en': 'English',
  'ja': '日本語',
  'ko': '한국어'
};

/** 有效语言代码列表 */
const validLanguages = ['zh-CN', 'zh-TW', 'en', 'ja', 'ko'] as const;
type ValidLanguage = typeof validLanguages[number];

/** 当前语言 */
let currentLanguage: string = 'zh-CN';

// ==================== Cookie 操作 ====================

/**
 * 检查用户是否同意使用 Cookie
 */
function hasCookieConsent(): boolean {
  const nameEQ = 'cookieConsent=';
  const ca = document.cookie.split(';');
  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];
    while (c.charAt(0) === ' ') { c = c.substring(1, c.length); }
    if (c.indexOf(nameEQ) === 0) {
      return c.substring(nameEQ.length, c.length) === 'accepted';
    }
  }
  return false;
}

/**
 * 设置 Cookie
 * @param name - Cookie 名称
 * @param value - Cookie 值
 * @param seconds - 过期时间（秒）
 */
function setCookie(name: string, value: string, seconds: number): void {
  if (!hasCookieConsent()) {
    return;
  }

  const date = new Date();
  date.setTime(date.getTime() + (seconds * 1000));
  const expires = 'expires=' + date.toUTCString();
  document.cookie = name + '=' + value + ';' + expires + ';path=/';
}

/**
 * 获取 Cookie
 * @param name - Cookie 名称
 * @returns Cookie 值
 */
function getCookie(name: string): string | null {
  const nameEQ = name + '=';
  const ca = document.cookie.split(';');
  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];
    while (c.charAt(0) === ' ') {c = c.substring(1, c.length);}
    if (c.indexOf(nameEQ) === 0) {return c.substring(nameEQ.length, c.length);}
  }
  return null;
}

// ==================== 语言初始化（立即执行） ====================

/**
 * 立即执行的初始化函数
 * 优先使用内联脚本设置的 __INIT_LANG__，避免内容闪烁
 */
(function initializeLanguage(): void {
  // 优先使用内联脚本已经计算好的语言
  let lang = (window as any).__INIT_LANG__ || 'zh-CN';

  // 确保语言值有效
  if (!validLanguages.includes(lang as ValidLanguage)) {
    console.warn('[I18N] WARN: Invalid language:', lang, ', falling back to zh-CN');
    lang = 'zh-CN';
  }

  // 设置当前语言
  currentLanguage = lang;
  window.currentLanguage = lang;

  // 设置 HTML lang 属性（如果还没被内联脚本设置）
  if (!document.documentElement.getAttribute('lang')) {
    document.documentElement.setAttribute('lang', lang);
  }

  // 如果不是默认语言（简体中文），先隐藏内容防止闪烁
  if (lang !== 'zh-CN' && document.documentElement.style.visibility !== 'hidden') {
    document.documentElement.style.visibility = 'hidden';
  }

  // 等待 DOM 解析完成后再设置按钮文字
  const setBtn = (): void => {
    const langText = document.querySelector('.language-current .lang-text');
    if (langText) {
      langText.textContent = LANG_NAMES[lang] || lang;
    }
  };

  if (document.body) {
    setBtn();
  } else {
    document.addEventListener('DOMContentLoaded', setBtn);
  }
})();

// ==================== 翻译加载 ====================

/**
 * 加载翻译数据
 * 生产环境：直接返回内嵌数据
 * @param lang - 语言代码
 * @returns 翻译数据
 */
async function loadTranslation(lang: string): Promise<TranslationData> {
  // 如果已有数据（生产环境内嵌），直接返回
  if (translations[lang]) {
    return translations[lang];
  }

  // 未找到翻译数据时返回空对象
  console.error('[I18N] ERROR: No translation data loaded for:', lang);
  if (lang !== 'zh-CN') {
    return loadTranslation('zh-CN');
  }
  return {};
}

// ==================== 翻译函数 ====================

/**
 * 获取翻译文本
 * @param key - 翻译键
 * @returns 翻译后的文本
 */
function t(key: string): string {
  const langData = translations[currentLanguage];
  if (!langData) {
    console.warn('[I18N] WARN: Translation data not loaded for:', currentLanguage);
    return key;
  }
  let text = langData[key] || key;
  // 版权声明年份动态替换
  if (key === 'footer.copyright') {
    text = text.replace('{year}', String(new Date().getFullYear()));
  }
  return text;
}

/**
 * 切换语言
 * @param lang - 目标语言代码
 */
async function switchLanguage(lang: string): Promise<void> {
  if (!supportedLanguages.some(l => l.code === lang)) {
    console.error('[I18N] ERROR: Unsupported language:', lang);
    return;
  }

  // 先加载翻译文件
  await loadTranslation(lang);

  // 更新当前语言
  currentLanguage = lang;
  window.currentLanguage = lang;
  setCookie('selectedLanguage', lang, 365 * 24 * 60 * 60);

  // 更新页面翻译
  updatePageTranslations();
}

/**
 * 更新页面所有翻译
 */
function updatePageTranslations(): void {
  // 使用 data-i18n 属性标记需要翻译的元素
  document.querySelectorAll('[data-i18n]').forEach(element => {
    const key = element.getAttribute('data-i18n');
    if (!key) {return;}

    const translation = t(key);
    if (translation && translation !== key) {
      element.textContent = translation;
    }
  });

  // 使用 data-i18n-placeholder 属性标记需要翻译的占位符
  document.querySelectorAll<HTMLInputElement>('[data-i18n-placeholder]').forEach(element => {
    const key = element.getAttribute('data-i18n-placeholder');
    if (!key) {return;}

    const translation = t(key);
    if (translation && translation !== key) {
      element.placeholder = translation;
    }
  });

  // 设置 HTML lang 属性
  document.documentElement.setAttribute('lang', currentLanguage);

  // 翻译完成后显示页面
  document.documentElement.style.visibility = 'visible';
}

// ==================== 初始化 ====================

/**
 * DOM 加载完成后初始化翻译
 */
async function initializeTranslations(): Promise<void> {
  // 加载当前语言的翻译文件
  await loadTranslation(currentLanguage);

  // 执行翻译
  updatePageTranslations();

  // 标记翻译已加载，触发事件
  if (typeof window !== 'undefined') {
    window.translationsLoaded = true;
    window.dispatchEvent(new Event('translationsReady'));
  }
}

// 根据 DOM 状态决定初始化时机
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initializeTranslations);
} else {
  initializeTranslations();
}

// ==================== 导出到全局 ====================

if (typeof window !== 'undefined') {
  window.t = t;
  window.switchLanguage = switchLanguage;
  window.updatePageTranslations = updatePageTranslations;
  window.translationsLoaded = false;
}
