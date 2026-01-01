/**
 * 全局类型声明
 */

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 语言切换函数类型 */
type SwitchLanguageFunction = (lang: string) => Promise<void>;

/** 支持的语言代码 */
type LanguageCode = 'zh-CN' | 'zh-TW' | 'en' | 'ja' | 'ko' | 'es' | 'fr' | 'de';

/** 扩展 Window 接口 */
interface Window {
  /** 翻译函数 */
  t?: TranslateFunction;
  /** 当前语言 */
  currentLanguage?: string;
  /** 切换语言函数 */
  switchLanguage?: SwitchLanguageFunction;
  /** 更新页面翻译 */
  updatePageTranslations?: () => void;
  /** 翻译是否已加载 */
  translationsLoaded?: boolean;
  /** QRCode 库 */
  QRCode?: QRCodeStatic;
}

/** QRCode 库类型 */
interface QRCodeStatic {
  new (container: HTMLElement, options: QRCodeOptions): void;
  CorrectLevel: {
    L: number;
    M: number;
    Q: number;
    H: number;
  };
}

interface QRCodeOptions {
  text: string;
  width?: number;
  height?: number;
  colorDark?: string;
  colorLight?: string;
  correctLevel?: number;
}
