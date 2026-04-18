/**
 * 设备检测工具（基于 UAParser.js）
 */

let cachedParser: UAParserConstructor | null = null;

function getParser(): UAParserConstructor {
  if (!cachedParser) {
    cachedParser = new window.UAParser!();
  }
  return cachedParser;
}

export function isMobileDevice(): boolean {
  if (!window.UAParser) {
    return /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini|Mobile|mobile/i.test(navigator.userAgent);
  }
  const device = getParser().getDevice();
  return device.type === 'mobile' || device.type === 'tablet' || device.type === 'wearable';
}

export function getDeviceType(): string {
  if (!window.UAParser) { return 'unknown'; }
  return getParser().getDevice().type || 'desktop';
}

export function getOS(): UAParserOS {
  if (!window.UAParser) { return {}; }
  return getParser().getOS();
}

export function getBrowser(): UAParserBrowser {
  if (!window.UAParser) { return {}; }
  return getParser().getBrowser();
}
