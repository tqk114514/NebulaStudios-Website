/**
 * 全局类型声明
 */

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 语言切换函数类型 */
type SwitchLanguageFunction = (lang: string) => Promise<void>;

/** 支持的语言代码 */
type LanguageCode = 'zh-CN' | 'zh-TW' | 'en' | 'ja' | 'ko';

/** UAParser.js 库类型（CDN 全局加载） */
interface UAParserDevice {
  vendor?: string;
  model?: string;
  type?: string;
}

interface UAParserBrowser {
  name?: string;
  version?: string;
  major?: string;
  type?: string;
}

interface UAParserOS {
  name?: string;
  version?: string;
}

interface UAParserCPU {
  architecture?: string;
}

interface UAParserEngine {
  name?: string;
  version?: string;
}

interface UAParserResult {
  ua: string;
  browser: UAParserBrowser;
  cpu: UAParserCPU;
  device: UAParserDevice;
  engine: UAParserEngine;
  os: UAParserOS;
}

interface UAParserConstructor {
  new (ua?: string, extensions?: Record<string, unknown>): UAParserConstructor;
  getResult(): UAParserResult;
  getBrowser(): UAParserBrowser;
  getCPU(): UAParserCPU;
  getDevice(): UAParserDevice;
  getEngine(): UAParserEngine;
  getOS(): UAParserOS;
  getUA(): string;
  setUA(ua: string): UAParserConstructor;
  DEVICE_TYPE: Record<string, string>;
  BROWSER: Record<string, string>;
  CPU: Record<string, string>;
  OS: Record<string, string>;
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
  /** UAParser.js 库 */
  UAParser?: UAParserConstructor;
}
