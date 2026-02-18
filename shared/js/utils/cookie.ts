/**
 * Cookie 操作工具模块
 *
 * 功能：
 * - Cookie 读写删除
 * - 用于倒计时状态持久化等
 */

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
 * @param required - 是否为必需 cookie（默认 false，可选 cookie 需要用户同意）
 */
export function setCookie(name: string, value: unknown, seconds: number, required: boolean = false): void {
  if (!name || typeof name !== 'string') {
    console.warn('[COOKIE] WARN: Invalid cookie name');
    return;
  }

  if (!required && !hasCookieConsent()) {
    return;
  }

  try {
    const date = new Date();
    date.setTime(date.getTime() + ((seconds || 0) * 1000));
    const expires = 'expires=' + date.toUTCString();
    document.cookie = name + '=' + (value ?? '') + ';' + expires + ';path=/';
  } catch (error) {
    console.error('[COOKIE] ERROR: Failed to set cookie:', (error as Error).message);
  }
}

/**
 * 获取 Cookie
 * @param name - Cookie 名称
 * @returns Cookie 值
 */
export function getCookie(name: string): string | null {
  if (!name || typeof name !== 'string') {
    console.warn('[COOKIE] WARN: Invalid cookie name');
    return null;
  }

  try {
    const nameEQ = name + '=';
    const ca = document.cookie.split(';');
    for (let i = 0; i < ca.length; i++) {
      let c = ca[i];
      while (c.charAt(0) === ' ') {c = c.substring(1, c.length);}
      if (c.indexOf(nameEQ) === 0) {return c.substring(nameEQ.length, c.length);}
    }
    return null;
  } catch (error) {
    console.error('[COOKIE] ERROR: Failed to get cookie:', (error as Error).message);
    return null;
  }
}

/**
 * 删除 Cookie
 * @param name - Cookie 名称
 */
export function deleteCookie(name: string): void {
  if (!name || typeof name !== 'string') {
    console.warn('[COOKIE] WARN: Invalid cookie name');
    return;
  }

  try {
    document.cookie = name + '=;expires=Thu, 01 Jan 1970 00:00:00 UTC;path=/;';
  } catch (error) {
    console.error('[COOKIE] ERROR: Failed to delete cookie:', (error as Error).message);
  }
}
