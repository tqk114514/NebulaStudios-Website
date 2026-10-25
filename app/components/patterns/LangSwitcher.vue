<script setup lang="ts">
// 语言切换器，填入 SiteHeader 的 tools 插槽。popover 机制（点外面/Escape 收起、
// 不被祖先 overflow 裁掉、点选项即收起）都交给浏览器，不手写 document 点击监听。
import { computed, ref } from 'vue'
import { useI18n } from '#imports'

interface Option {
  code: 'zh-CN' | 'en'
  /** 语言名一律用该语言自己书写，不参与翻译 */
  name: string
}

const OPTIONS: Option[] = [
  { code: 'zh-CN', name: '简体中文' },
  { code: 'en', name: 'English' },
]

const { locale, setLocale, t } = useI18n()
const open = ref(false)

const current = computed(() => OPTIONS.find((o) => o.code === locale.value) ?? OPTIONS[0]!)

async function choose(code: Option['code']) {
  if (code !== locale.value) await setLocale(code)
}

// 点外面与按 Escape 都不触发按钮的 click，展开态只能从 popover 自己的事件读
function onToggle(event: Event) {
  if (event.target instanceof HTMLElement) open.value = event.target.matches(':popover-open')
}
</script>

<template>
  <div class="lang">
    <button
      class="lang__trigger"
      type="button"
      popovertarget="lang-menu"
      :aria-label="t('lang.switch')"
      :aria-expanded="open"
    >
      <svg class="lang__icon" viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3a14 14 0 0 1 0 18a14 14 0 0 1 0-18" />
      </svg>
      <span class="lang__current">{{ current.name }}</span>
      <svg class="lang__icon lang__caret" viewBox="0 0 12 12" aria-hidden="true">
        <path d="M3 4.5L6 7.5L9 4.5" />
      </svg>
    </button>

    <div
      id="lang-menu"
      class="lang__menu"
      popover
      @toggle="onToggle">
      <button
        v-for="option in OPTIONS"
        :key="option.code"
        class="lang__option"
        type="button"
        popovertarget="lang-menu"
        popovertargetaction="hide"
        :aria-current="option.code === locale ? 'true' : undefined"
        @click="choose(option.code)"
      >
        <span>{{ option.name }}</span>
        <svg class="lang__icon lang__check" viewBox="0 0 14 14" aria-hidden="true">
          <path d="M2.5 7L5.5 10L11.5 4" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.lang {
  position: relative;
}

.lang__trigger {
  anchor-name: --lang-trigger;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: transparent;
  border: 1px solid var(--line);
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: var(--track-wide);
  color: var(--fg);
  cursor: pointer;
  transition: border-color var(--dur-ui) var(--ease-standard);
}

.lang__trigger:hover {
  border-color: var(--fg-muted);
}

/* 语言名长短不一（简体中文 / English），定宽才不会在切换时把整条头部推得挪动 */
.lang__current {
  min-width: 60px;
  text-align: left;
}

.lang__icon {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
}

.lang__caret {
  width: 10px;
  height: 10px;
  transition: transform var(--dur-ui) var(--ease-standard);
}

.lang:has(.lang__menu:popover-open) .lang__caret {
  transform: rotate(180deg);
}

/*
 * 顶层元素不以 .lang 为包含块，所以用锚点定位贴着按钮（block-end、右对齐，放不下翻上）；
 * top/right 是不支持锚点定位时的回退，margin/inset 清掉 UA 的居中样式。
 * 展开动画：popover 显隐是离散属性，得把 display 列进 transition 并 allow-discrete。
 */
.lang__menu {
  position: fixed;
  inset: auto;
  position-anchor: --lang-trigger;
  position-area: span-block-end span-inline-end;
  position-try-fallbacks: flip-block;
  top: var(--header-h);
  right: var(--space-10);
  margin: 0;
  margin-top: var(--space-2);
  min-width: 140px;
  padding: 0;
  background: var(--bg);
  border: 1px solid var(--line);
  white-space: nowrap;
  opacity: 0;
  transform: translateY(-10px);
  transition: opacity var(--dur-ui) var(--ease-standard), transform var(--dur-ui) var(--ease-standard), display var(--dur-ui) allow-discrete;
}

.lang__menu:popover-open {
  opacity: 1;
  transform: none;
}

@starting-style {
  .lang__menu:popover-open {
    opacity: 0;
    transform: translateY(-10px);
  }
}

.lang__option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  background: transparent;
  border: 0;
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: var(--track-wide);
  text-align: left;
  color: var(--fg-muted);
  cursor: pointer;
  transition: color var(--dur-ui) var(--ease-standard), background-color var(--dur-ui) var(--ease-standard);
}

.lang__option:hover {
  color: var(--fg);
  background: var(--surface);
}

.lang__check {
  width: 14px;
  height: 14px;
  stroke-width: 2;
  opacity: 0;
}

.lang__option[aria-current='true'] {
  color: var(--fg);
}

.lang__option[aria-current='true'] .lang__check {
  opacity: 1;
}
</style>
