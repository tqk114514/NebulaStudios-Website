/**
 * 全局类型声明
 */

/** 翻译函数类型 */
type TranslateFunction = (key: string) => string;

/** 语言切换函数类型 */
type SwitchLanguageFunction = (lang: string) => Promise<void>;

/** 支持的语言代码 */
type LanguageCode = 'zh-CN' | 'zh-TW' | 'en' | 'ja' | 'ko';

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

/** BarcodeDetector API 类型 */
interface BarcodeDetector {
  new (options?: { formats?: string[] }): BarcodeDetector;
  detect(source: ImageBitmapSource): Promise<Array<{ rawValue: string }>>;
}

/** jsQR 库类型 */
interface JsQRResult {
  data: string;
}

declare function jsQR(
  data: Uint8ClampedArray,
  width: number,
  height: number,
  options?: { inversionAttempts?: string }
): JsQRResult | null;

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
  /** 切换语言函数 */
  switchLanguage?: SwitchLanguageFunction;
  /** 更新页面翻译 */
  updatePageTranslations?: () => void;
  /** 翻译是否已加载 */
  translationsLoaded?: boolean;
  /** QRCode 库 */
  QRCode?: QRCodeStatic;
  /** BarcodeDetector API */
  BarcodeDetector?: BarcodeDetector;
  /** jsQR 库 */
  jsQR?: typeof jsQR;
  /** UAParser.js 库 */
  UAParser?: UAParserConstructor;
}
