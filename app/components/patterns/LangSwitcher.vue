<script setup lang="ts">
// 语言切换器，填入 SiteHeader 的 tools 插槽。
// 用 Popover API 而不是手写 document 点击监听：点外面关闭、Escape 关闭、
// 顶层渲染不被祖先 overflow 裁掉，都是浏览器负责的，v3 那套要自己管还漏过焦点。
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
const triggerRef = ref<HTMLElement | null>(null)
const open = ref(false)

const current = computed(() => OPTIONS.find((o) => o.code === locale.value) ?? OPTIONS[0]!)

async function choose(code: Option['code']) {
  open.value = false
  if (code !== locale.value) await setLocale(code)
}

// 点外面与按 Escape 都不触发按钮的 click，展开态只能从 popover 自己的事件读。
// 位置也在这里写：顶层元素不以祖先为包含块（absolute 相对视口解析），
// 只能按触发按钮的实际矩形算出 fixed 偏移。读 CSS 令牌同样只能在这里——
// 放到 setup 顶层会在 SSR 阶段调用到不存在的 getComputedStyle。
function onToggle(event: Event) {
  const trigger = triggerRef.value
  if (!(event.target instanceof HTMLElement) || !trigger) return
  const menu = event.target
  open.value = menu.matches(':popover-open')
  if (!open.value) return

  const gap = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--space-2')) || 8
  const box = trigger.getBoundingClientRect()
  menu.style.setProperty('--menu-top', `${box.bottom + gap}px`)
  // clientWidth 而非 innerWidth：fixed 的 right 从布局视口算，含滚动条的 innerWidth 会偏出一个 scrollbar 宽
  menu.style.setProperty('--menu-right', `${document.documentElement.clientWidth - box.right}px`)
}
</script>

<template>
  <div class="lang">
    <button
      ref="triggerRef"
      class="lang__trigger"
      type="button"
      popovertarget="lang-menu"
      :aria-label="t('lang.switch')"
      :aria-expanded="open"
    >
      <svg
        class="lang__globe"
        width="16"
        height="16"
        viewBox="0 0 24 24"
        aria-hidden="true"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3a14 14 0 0 1 0 18a14 14 0 0 1 0-18" />
      </svg>
      <span class="lang__current">{{ current.name }}</span>
      <svg
        class="lang__caret"
        :class="{ 'lang__caret--open': open }"
        width="10"
        height="10"
        viewBox="0 0 12 12"
        aria-hidden="true"
        fill="none"
        stroke="currentColor"
        stroke-width="1.5">
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
        :aria-current="option.code === locale ? 'true' : undefined"
        @click="choose(option.code)"
      >
        {{ option.name }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.lang {
  position: relative;
}

.lang__trigger {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--target-comfort);
  padding: 0 var(--space-3);
  background: transparent;
  border: 1px solid var(--line);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
  cursor: pointer;
  transition: color var(--dur-ui) var(--ease-standard), border-color var(--dur-ui) var(--ease-standard);
}

.lang__trigger:hover {
  border-color: var(--line-strong);
  color: var(--fg);
}

.lang__globe,
.lang__caret {
  flex-shrink: 0;
}

.lang__caret {
  transition: transform var(--dur-ui) var(--ease-standard);
}

.lang__caret--open {
  transform: rotate(180deg);
}

/*
 * 顶层元素不以 .lang 为包含块，位置由 onToggle 写进来的 --menu-top / --menu-right
 * 决定（触发按钮的实际矩形），var 回退值保证测量前不闪成整屏宽。
 * margin 与 inset 是 UA 那套居中样式的残留，必须清掉。
 */
.lang__menu {
  position: fixed;
  inset: auto;
  top: var(--menu-top, var(--header-h));
  right: var(--menu-right, var(--space-10));
  margin: 0;
  padding: var(--space-1) 0;
  background: var(--bg);
  border: 1px solid var(--line);
  white-space: nowrap;
}

.lang__option {
  display: block;
  width: 100%;
  padding: var(--space-2) var(--space-3);
  background: transparent;
  border: 0;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  text-align: left;
  color: var(--fg-muted);
  cursor: pointer;
}

.lang__option:hover {
  color: var(--fg);
  background: var(--surface);
}

.lang__option[aria-current='true'] {
  color: var(--fg);
}

@media (max-width: 640px) {
  .lang__current {
    display: none;
  }
}
</style>
