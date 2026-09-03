<script setup lang="ts">
// Cookie 同意横幅（迁移自 shared/js/cookie-consent.ts，行为一致）：
// - 无 cookieConsent cookie 时显示（accepted/rejected 任一存在则不显示）
// - 同意/拒绝均写入 365 天 cookie
// - 拒绝时清除可选 cookie（selectedLanguage），必要 cookie 不受影响
// - 管理后台不展示（App.vue 按路由门控，与旧版 admin 页不引入横幅一致）
import { ref, onMounted } from 'vue'
import AppButton from '@/components/AppButton.vue'

const CONSENT_COOKIE_NAME = 'cookieConsent'
const CONSENT_EXPIRY_DAYS = 365
const OPTIONAL_COOKIES = ['selectedLanguage']

const visible = ref(false)

function getCookie(name: string): string | null {
  const entry = document.cookie
    .split('; ')
    .find((s) => s.startsWith(name + '='))
  return entry ? decodeURIComponent(entry.slice(name.length + 1)) : null
}

function setCookie(name: string, value: string, days: number): void {
  const expires = new Date(Date.now() + days * 24 * 60 * 60 * 1000).toUTCString()
  document.cookie = `${name}=${encodeURIComponent(value)};expires=${expires};path=/`
}

function deleteCookie(name: string): void {
  document.cookie = `${name}=;expires=Thu, 01 Jan 1970 00:00:00 UTC;path=/`
}

function clearOptionalCookies(): void {
  for (const name of OPTIONAL_COOKIES) {
    if (getCookie(name) !== null) deleteCookie(name)
  }
}

function setConsent(value: 'accepted' | 'rejected'): void {
  setCookie(CONSENT_COOKIE_NAME, value, CONSENT_EXPIRY_DAYS)
  if (value === 'rejected') clearOptionalCookies()
  visible.value = false
}

onMounted(() => {
  const saved = getCookie(CONSENT_COOKIE_NAME)
  if (saved !== 'accepted' && saved !== 'rejected') {
    visible.value = true
  }
})
</script>

<template>
  <transition name="cookie-consent">
    <div v-if="visible" class="cookie-consent" role="dialog" aria-live="polite">
      <p class="cookie-consent__text">{{ $t('cookieConsent.message') }}</p>
      <div class="cookie-consent__buttons">
        <AppButton variant="primary" :arrow="false" @click="setConsent('accepted')">
          {{ $t('cookieConsent.accept') }}
        </AppButton>
        <AppButton variant="secondary" :arrow="false" @click="setConsent('rejected')">
          {{ $t('cookieConsent.reject') }}
        </AppButton>
      </div>
    </div>
  </transition>
</template>

<style scoped>
/* 迁移自 shared/css/general.css 的 .cookie-consent-*（值不变，命名 BEM 化）。
   唯一例外：z-index 不沿用旧站的 3000——新站层级约定为顶栏 100 / 页面遮罩 110 / 弹窗 200+，
   横幅取 105（站头之上、加载遮罩与弹窗之下），避免首访加载期间盖住遮罩。 */
.cookie-consent {
  position: fixed;
  bottom: 24px;
  right: 24px;
  left: 24px;
  max-width: 360px;
  margin-left: auto;
  background: var(--bg);
  border: 1px solid var(--line);
  padding: 24px;
  z-index: 105;
}

.cookie-consent__text {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
  display: block;
  margin-bottom: 20px;
}

.cookie-consent__buttons {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 旧站横幅按钮不做大写变换（.cookie-consent-buttons .button-primary { text-transform: none }） */
.cookie-consent__buttons :deep(.app-button--primary) {
  text-transform: none;
}

/* 淡入淡出（迁移自 .cookie-consent-hidden 的过渡定义；离场期间停止拦截点击） */
.cookie-consent-enter-active,
.cookie-consent-leave-active {
  transition: opacity 0.3s, transform 0.3s;
}
.cookie-consent-enter-from,
.cookie-consent-leave-to {
  opacity: 0;
  transform: translateY(20px);
}
.cookie-consent-leave-active {
  pointer-events: none;
}
</style>
