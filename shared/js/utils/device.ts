/**
 * 设备检测工具（基于 UAParser.js）
 */

import UAParser from '../lib/faisalman-ua-parser-js@1.0.41/src/ua-parser.js';

let cachedParser: UAParserConstructor | null = null;

function getParser(): UAParserConstructor {
  if (!cachedParser) {
    // ua-parser.js 位于 tsconfig exclude 的 shared/js/lib，TS7 无法为其推断构造签名，
    // 先将构造器类型化再 new（运行时行为不变）
    const ParserCtor = UAParser as unknown as UAParserConstructor;
    cachedParser = new ParserCtor();
  }
  return cachedParser;
}

export function isMobileDevice(): boolean {
  const device = getParser().getDevice();
  return device.type === 'mobile' || device.type === 'tablet' || device.type === 'wearable';
}
