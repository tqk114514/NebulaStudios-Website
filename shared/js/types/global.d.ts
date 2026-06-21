/**
 * 全局类型声明
 */

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 语言切换函数类型 */
type SwitchLanguageFunction = (lang: string) => Promise<void>;

/** 支持的语言代码 */
type LanguageCode = 'zh-CN' | 'zh-TW' | 'en' | 'ja' | 'ko';

/** UAParser.js 库类型 */

interface UAParserDevice {
  vendor?: string;
  model?: string;
  type?: string;
}

interface UAParserConstructor {
  new (ua?: string, extensions?: Record<string, unknown>): UAParserConstructor;
  getDevice(): UAParserDevice;
}

/** 扩展 Window 接口 */
interface Window {
  /** 翻译函数 */
  t?: TranslateFunction;
  /** 当前语言 */
  currentLanguage?: string;
  /** 内联脚本初始化的语言 */
  __INIT_LANG__?: string;
  /** 切换语言函数 */
  switchLanguage?: SwitchLanguageFunction;
  /** 更新页面翻译 */
  updatePageTranslations?: () => void;
  /** 翻译是否已加载 */
  translationsLoaded?: boolean;
}
