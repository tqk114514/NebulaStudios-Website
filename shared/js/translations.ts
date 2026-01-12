/**
 * 国际化翻译模块
 *
 * 功能：
 * - 多语言支持（5种语言：简繁英日韩）
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

/** 翻译数据（构建时注入 __ALL_TRANSLATIONS__，开发时动态加载） */
const translations: TranslationsMap = typeof __ALL_TRANSLATIONS__ !== 'undefined' ? __ALL_TRANSLATIONS__ : {};

/** 支持的语言列表（简繁英日韩） */
const supportedLanguages: LanguageInfo[] = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' }
];

/** 有效语言代码列表 */
const validLanguages = ['zh-CN', 'zh-TW', 'en', 'ja', 'ko'] as const;
type ValidLanguage = typeof validLanguages[number];

/** 当前语言 */
let currentLanguage: string = 'zh-CN';

// ==================== Cookie 操作 ====================

/**
 * 设置 Cookie
 * @param name - Cookie 名称
 * @param value - Cookie 值
 * @param days - 有效天数
 */
function setCookie(name: string, value: string, days: number = 365): void {
  const date = new Date();
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000));
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
 * 在 DOM 解析前确定语言，避免内容闪烁
 */
(function initializeLanguage(): void {
  // 获取保存的语言或浏览器默认语言
  const savedLang = getCookie('selectedLanguage');
  const browserLang = navigator.language || (navigator as unknown as { userLanguage: string }).userLanguage;
  let lang = savedLang;

  // 如果没有保存的语言，根据浏览器语言自动选择
  if (!lang) {
    if (browserLang.startsWith('zh')) {
      lang = (browserLang.includes('TW') || browserLang.includes('HK')) ? 'zh-TW' : 'zh-CN';
    } else if (browserLang.startsWith('ja')) {
      lang = 'ja';
    } else if (browserLang.startsWith('ko')) {
      lang = 'ko';
    } else if (browserLang.startsWith('en')) {
      lang = 'en';
    } else {
      // 其他语言默认使用英语
      lang = 'en';
    }
  }

  // 确保语言值有效
  if (!validLanguages.includes(lang as ValidLanguage)) {
    console.warn('[I18N] WARN: Invalid language:', lang, ', falling back to zh-CN');
    lang = 'zh-CN';
  }

  // 设置 HTML lang 属性
  document.documentElement.setAttribute('lang', lang);

  // 如果不是默认语言（简体中文），先隐藏内容防止闪烁
  if (lang !== 'zh-CN') {
    document.documentElement.style.visibility = 'hidden';
  }

  // 设置当前语言
  currentLanguage = lang;
  window.currentLanguage = lang;
})();

// ==================== 翻译加载 ====================

/** i18n 模块列表 */
const i18nModules = ['general', 'account'];

/**
 * 加载翻译数据
 * 生产环境：直接返回内嵌数据
 * 开发环境：异步加载 JSON 文件并合并
 * @param lang - 语言代码
 * @returns 翻译数据
 */
async function loadTranslation(lang: string): Promise<TranslationData> {
  // 如果已有数据（生产环境内嵌），直接返回
  if (translations[lang]) {
    return translations[lang];
  }

  // 开发环境：从多个子目录异步加载并合并
  const merged: TranslationData = {};

  for (const module of i18nModules) {
    try {
      const response = await fetch(`/shared/i18n/${module}/${lang}.json`);
      if (!response.ok) {
        console.warn(`[I18N] WARN: Failed to load ${module}/${lang}.json`);
        continue;
      }
      const data: TranslationData = await response.json();
      Object.assign(merged, data);
    } catch (error) {
      console.warn(`[I18N] WARN: Failed to load ${module}/${lang}.json:`, (error as Error).message);
    }
  }

  if (Object.keys(merged).length === 0) {
    console.error('[I18N] ERROR: No translation data loaded for:', lang);
    if (lang !== 'zh-CN') {
      return await loadTranslation('zh-CN');
    }
    return {};
  }

  translations[lang] = merged;
  return merged;
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
    text = text.replace(/20\d{2}/, String(new Date().getFullYear()));
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
  setCookie('selectedLanguage', lang);

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
