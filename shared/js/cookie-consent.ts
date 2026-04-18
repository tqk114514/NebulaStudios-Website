/**
 * shared/js/cookie-consent.ts
 * Cookie 同意横幅模块
 *
 * 功能：
 * - 显示 Cookie 同意横幅
 * - 保存用户同意状态
 * - 提供检查函数供其他模块使用
 */

// Cookie 函数内联（此文件单独构建为 IIFE，无法 import shared/js/utils/cookie.ts）
// 重复实现 getCookie / setCookie / deleteCookie 是有意为之，详见 shared/js/utils/cookie.ts 中的完整版本
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

function deleteCookie(name: string): void {
  document.cookie = name + '=;expires=Thu, 01 Jan 1970 00:00:00 UTC;path=/;';
}

const CONSENT_COOKIE_NAME = 'cookieConsent';
const CONSENT_EXPIRY_DAYS = 365;

const OPTIONAL_COOKIES = ['selectedLanguage'];

export type ConsentType = 'accepted' | 'rejected' | null;

let cachedConsent: ConsentType = null;

function clearOptionalCookies(): void {
  for (const name of OPTIONAL_COOKIES) {
    if (getCookie(name) !== null) {
      deleteCookie(name);
    }
  }
}

function setCookie(name: string, value: string, days: number): void {
  const date = new Date();
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000));
  const expires = 'expires=' + date.toUTCString();
  document.cookie = name + '=' + value + ';' + expires + ';path=/;';
}

export function getConsent(): ConsentType {
  if (cachedConsent !== null) {
    return cachedConsent;
  }

  const saved = getCookie(CONSENT_COOKIE_NAME);
  if (saved === 'accepted' || saved === 'rejected') {
    cachedConsent = saved;
  } else {
    cachedConsent = null;
  }

  return cachedConsent;
}

export function isCookieAllowed(): boolean {
  return getConsent() === 'accepted';
}

function setConsent(value: 'accepted' | 'rejected'): void {
  cachedConsent = value;
  setCookie(CONSENT_COOKIE_NAME, value, CONSENT_EXPIRY_DAYS);
}

function createBanner(): void {
  const consent = getConsent();
  if (consent !== null) {
    return;
  }

  const banner = document.createElement('div');
  banner.id = 'cookie-consent-banner';
  banner.className = 'cookie-consent-banner';
  banner.innerHTML = `
    <div class="cookie-consent-content">
      <span class="cookie-consent-text" data-i18n="cookieConsent.message"></span>
      <div class="cookie-consent-buttons">
        <button id="cookie-accept" class="button-primary" data-i18n="cookieConsent.accept"></button>
        <button id="cookie-reject" class="button-secondary" data-i18n="cookieConsent.reject"></button>
      </div>
    </div>
  `;

  document.body.appendChild(banner);

  function translateBanner(): void {
    if (typeof window !== 'undefined' && typeof (window as unknown as { updatePageTranslations: unknown }).updatePageTranslations === 'function') {
      (window as unknown as { updatePageTranslations: () => void }).updatePageTranslations();
    }
  }

  if (typeof window !== 'undefined' && (window as unknown as { translationsLoaded: boolean }).translationsLoaded) {
    translateBanner();
  } else {
    window.addEventListener('translationsReady', translateBanner);
  }

  document.getElementById('cookie-accept')?.addEventListener('click', () => {
    setConsent('accepted');
    hideBanner();
  });

  document.getElementById('cookie-reject')?.addEventListener('click', () => {
    setConsent('rejected');
    clearOptionalCookies();
    hideBanner();
  });
}

function hideBanner(): void {
  const banner = document.getElementById('cookie-consent-banner');
  if (banner) {
    banner.classList.add('cookie-consent-hidden');
    setTimeout(() => banner.remove(), 300);
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', createBanner);
} else {
  createBanner();
}
