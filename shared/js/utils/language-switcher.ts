/**
 * 语言切换器模块
 *
 * 功能：
 * - 初始化语言切换事件
 * - 应用翻译到页面元素
 * - 更新页面标题
 */

/** 语言显示名称映射（方便快速查找） */
const LANG_NAMES: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  'en': 'English',
  'ja': '日本語',
  'ko': '한국어'
};

let prevDestroy: (() => void) | null = null;

/**
 * 初始化语言切换器（下拉列表式）
 * @param onLanguageChange - 语言切换后的回调函数
 * @returns 销毁函数，调用后移除所有事件监听器
 */
export function initLanguageSwitcher(onLanguageChange?: (lang: string) => void): () => void {
  // 销毁上一次实例的所有事件监听器
  if (prevDestroy) {
    prevDestroy();
    prevDestroy = null;
  }

  const languageSwitcher = document.querySelector('.language-switcher');
  const currentBtn = document.querySelector('.language-current');
  const langText = document.querySelector('.language-current .lang-text');
  const languageOptions = document.querySelectorAll('.language-dropdown .language-option');

  if (!languageSwitcher || !currentBtn || !languageOptions.length) {
    console.warn('[I18N] WARN: Language switcher elements not found');
    return () => {};
  }

  // 设置当前语言状态
  const currentLang = window.currentLanguage || 'zh-CN';
  updateCurrentDisplay(currentLang);

  // 更新选项激活状态
  languageOptions.forEach(option => {
    const optionLang = option.getAttribute('data-lang');
    const isActive = optionLang === currentLang;
    option.classList.toggle('active', isActive);
    option.setAttribute('aria-selected', String(isActive));
  });

  // 点击当前按钮切换下拉菜单
  const handleCurrentBtnClick = (e: Event) => {
    e.preventDefault();
    e.stopPropagation();
    const isOpen = languageSwitcher.classList.toggle('is-open');
    currentBtn.setAttribute('aria-expanded', String(isOpen));
  };

  // 点击选项切换语言
  const handleOptionClicks: Map<Element, (e: Event) => void> = new Map();

  languageOptions.forEach(option => {
    const handler = async (e: Event) => {
      e.preventDefault();
      e.stopPropagation();

      const selectedLang = option.getAttribute('data-lang');

      // 关闭下拉菜单
      languageSwitcher.classList.remove('is-open');
      currentBtn.setAttribute('aria-expanded', 'false');

      if (!selectedLang || selectedLang === window.currentLanguage) {return;}

      // 切换语言
      if (window.switchLanguage) {
        await window.switchLanguage(selectedLang);
      }

      // 更新显示
      updateCurrentDisplay(selectedLang);

      // 更新选项激活状态
      languageOptions.forEach(opt => {
        const isActive = opt.getAttribute('data-lang') === selectedLang;
        opt.classList.toggle('active', isActive);
        opt.setAttribute('aria-selected', String(isActive));
      });

      // 执行回调
      if (typeof onLanguageChange === 'function') {
        onLanguageChange(selectedLang);
      }
    };
    handleOptionClicks.set(option, handler);
    option.addEventListener('click', handler);
  });

  // 点击外部关闭下拉菜单
  const handleDocumentClick = (e: MouseEvent) => {
    if (!languageSwitcher.contains(e.target as Node)) {
      languageSwitcher.classList.remove('is-open');
      currentBtn.setAttribute('aria-expanded', 'false');
    }
  };

  // ESC 键关闭下拉菜单
  const handleDocumentKeydown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && languageSwitcher.classList.contains('is-open')) {
      languageSwitcher.classList.remove('is-open');
      currentBtn.setAttribute('aria-expanded', 'false');
    }
  };

  currentBtn.addEventListener('click', handleCurrentBtnClick);
  document.addEventListener('click', handleDocumentClick);
  document.addEventListener('keydown', handleDocumentKeydown);

  // 销毁函数：移除所有事件监听器
  const destroy = () => {
    currentBtn.removeEventListener('click', handleCurrentBtnClick);
    handleOptionClicks.forEach((handler, option) => {
      option.removeEventListener('click', handler);
    });
    handleOptionClicks.clear();
    document.removeEventListener('click', handleDocumentClick);
    document.removeEventListener('keydown', handleDocumentKeydown);
    prevDestroy = null;
  };

  prevDestroy = destroy;

  return destroy;

  function updateCurrentDisplay(lang: string): void {
    if (langText) {
      langText.textContent = LANG_NAMES[lang] || lang;
    }
  }
}

/**
 * 手动应用翻译到页面元素
 * @returns 是否成功
 */
export function applyTranslations(): boolean {
  if (typeof window.t !== 'function') {
    console.warn('[I18N] WARN: Translation function not available');
    return false;
  }

  // 翻译文本内容
  document.querySelectorAll('[data-i18n]').forEach(element => {
    const key = element.getAttribute('data-i18n');
    if (!key) {return;}

    try {
      const translation = window.t?.(key);
      if (translation && translation !== key) {
        element.textContent = translation;
      }
    } catch (e) {
      console.warn(`[I18N] WARN: Failed to translate key "${key}":`, (e as Error).message);
    }
  });

  // 翻译占位符
  document.querySelectorAll<HTMLInputElement>('[data-i18n-placeholder]').forEach(element => {
    const key = element.getAttribute('data-i18n-placeholder');
    if (!key) {return;}

    try {
      const translation = window.t?.(key);
      if (translation && translation !== key) {
        element.placeholder = translation;
      }
    } catch (e) {
      console.warn(`[I18N] WARN: Failed to translate placeholder key "${key}":`, (e as Error).message);
    }
  });

  updatePageTitle();
  return true;
}

/**
 * 更新页面标题
 */
export function updatePageTitle(): void {
  if (typeof window.t !== 'function') {return;}

  const titleKey = document.documentElement.getAttribute('data-i18n-title');
  if (titleKey) {
    const translation = window.t(titleKey);
    if (translation && translation !== titleKey) {
      document.title = translation;
    }
  }
}

/**
 * 等待翻译系统就绪
 */
export function waitForTranslations(): Promise<void> {
  return new Promise((resolve) => {
    // 如果已就绪，直接返回
    if (window.t && typeof window.t === 'function') {
      resolve();
      return;
    }

    // 监听就绪事件
    const onReady = (): void => {
      window.removeEventListener('translationsReady', onReady);
      resolve();
    };
    window.addEventListener('translationsReady', onReady);

    // 超时保护（3秒）
    setTimeout(() => {
      window.removeEventListener('translationsReady', onReady);
      if (!window.t || typeof window.t !== 'function') {
        console.warn('[I18N] WARN: Translation system not ready after 3s, continuing with default texts');
      }
      resolve();
    }, 3000);
  });
}

/**
 * 获取当前语言
 */
export function getCurrentLanguage(): string {
  return window.currentLanguage || 'zh-CN';
}

/**
 * 隐藏页面加载遮罩
 * @param delay - 延迟时间（毫秒）
 */
export function hidePageLoader(delay: number = 500): void {
  setTimeout(() => {
    const loader = document.getElementById('page-loader');
    if (loader) {
      loader.classList.add('is-hidden');
    }
  }, delay);
}

/** 延迟执行工具函数 */
