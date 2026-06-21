/**
 * 设备检测工具（基于 UAParser.js）
 */

import UAParser from '../../../../../../shared/js/lib/faisalman-ua-parser-js@1.0.41/src/ua-parser.js';

let cachedParser: UAParserConstructor | null = null;

function getParser(): UAParserConstructor {
  if (!cachedParser) {
    cachedParser = new UAParser() as unknown as UAParserConstructor;
  }
  return cachedParser;
}

export function isMobileDevice(): boolean {
  const device = getParser().getDevice();
  return device.type === 'mobile' || device.type === 'tablet' || device.type === 'wearable';
}
