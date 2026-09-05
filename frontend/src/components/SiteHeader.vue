<script setup lang="ts">
// 顶部导航栏 + 语言切换器（迁移自原站 shared/components/header.html，样式自持）。
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { persistLocale, type LocaleCode } from '@/i18n'

const { locale } = useI18n()

const LANGS: { code: string; label: string }[] = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
]

const open = ref(false)

// 点击切换器外部时收起下拉
const langSwitchRef = ref<HTMLElement | null>(null)
function onDocClick(e: MouseEvent) {
  if (langSwitchRef.value && !langSwitchRef.value.contains(e.target as Node)) {
    open.value = false
  }
}
onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))

const current = computed(() => LANGS.find((l) => l.code === locale.value) ?? LANGS[0])

function setLang(code: string) {
  locale.value = code
  persistLocale(code as LocaleCode)
  open.value = false
}
</script>

<template>
  <header class="site-header" translate="no">
    <RouterLink to="/" class="site-header__brand" role="banner" aria-label="Nebula Studios">
      <!-- N 字标内联：currentColor 跟随头部前景色（--fg 暖白） -->
      <svg class="brand-mark" viewBox="0 0 256 256" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
        <path d="M56 48H96L144 96L112 128L96 112V208H56V48Z" />
        <path d="M200 208H160L112 160L144 128L160 144V48H200V208Z" />
      </svg>
    </RouterLink>

    <div ref="langSwitchRef" class="lang-switch" :class="{ 'is-open': open }">
      <button
        class="lang-switch__current"
        type="button"
        aria-haspopup="listbox"
        :aria-expanded="open"
        @click="open = !open"
      >
        <svg class="lang-switch__icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10" />
          <line x1="2" y1="12" x2="22" y2="12" />
          <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
        <span class="lang-switch__text">{{ current.label }}</span>
        <svg class="lang-switch__arrow" width="12" height="12" viewBox="0 0 12 12" fill="none">
          <path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>

      <div class="lang-switch__dropdown" role="listbox">
        <button
          v-for="l in LANGS"
          :key="l.code"
          class="lang-switch__option"
          :class="{ active: l.code === locale }"
          role="option"
          :aria-selected="l.code === locale"
          @click="setLang(l.code)"
        >
          <span>{{ l.label }}</span>
          <svg class="lang-switch__check" width="14" height="14" viewBox="0 0 14 14" fill="none">
            <path d="M2.5 7L5.5 10L11.5 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
      </div>
    </div>
  </header>
</template>

<style scoped>
.site-header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 40px;
  background: var(--bg);
  border-bottom: 1px solid var(--line);
  /* 全站层级约定：顶栏 100、页面遮罩 110、普通弹窗 200、提示/确认二级弹窗 210 */
  z-index: 100;
}

.site-header__brand {
  display: inline-flex;
  align-items: center;
  color: var(--fg);
}

/* N 字标：28px 匹配 60px 高的头部 */
.brand-mark {
  width: 28px;
  height: 28px;
  fill: currentColor;
}

/* ---- 语言切换器 ---- */
.lang-switch {
  position: relative;
}

.lang-switch__current {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: transparent;
  border: 1px solid var(--line);
  color: var(--fg);
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: 0.02em;
  cursor: pointer;
  transition: border-color 0.2s;
}

.lang-switch__current:hover {
  border-color: var(--mid);
}

.lang-switch__text {
  min-width: 60px;
  text-align: left;
}

.lang-switch__arrow {
  transition: transform 0.2s;
}

.lang-switch.is-open .lang-switch__arrow {
  transform: rotate(180deg);
}

.lang-switch__dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  min-width: 140px;
  background: var(--bg);
  border: 1px solid var(--line);
  opacity: 0;
  visibility: hidden;
  transform: translateY(-10px);
  transition:
    opacity 0.2s,
    visibility 0.2s,
    transform 0.2s;
}

.lang-switch.is-open .lang-switch__dropdown {
  opacity: 1;
  visibility: visible;
  transform: translateY(0);
}

.lang-switch__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 10px 12px;
  background: transparent;
  border: none;
  color: var(--mid);
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: 0.02em;
  text-align: left;
  cursor: pointer;
  transition: color 0.2s, background 0.2s;
}

.lang-switch__option:hover {
  color: var(--fg);
  background: var(--dim);
}

.lang-switch__option.active {
  color: var(--fg);
}

.lang-switch__check {
  flex-shrink: 0;
  opacity: 0;
}

.lang-switch__option.active .lang-switch__check {
  opacity: 1;
}
</style>