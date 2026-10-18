<script setup lang="ts">
// 全站唯一按钮外观出处。to / href 缺省时渲染 <button>，用于表单提交与动作；
// 传 to 或 href 时渲染链接，视觉与按钮一致，避免出现第二套 CTA 样式。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary'
    size?: 'md' | 'sm'
    type?: 'button' | 'submit'
    block?: boolean
    arrow?: boolean
    disabled?: boolean
    to?: string
    href?: string
  }>(),
  {
    variant: 'primary',
    size: 'md',
    type: 'button',
    block: true,
    arrow: true,
    disabled: false,
    to: undefined,
    href: undefined,
  },
)

const tag = computed(() => {
  if (props.to) return 'NuxtLink'
  return props.href ? 'a' : 'button'
})

const bind = computed(() => {
  if (props.to) return { to: props.to }
  if (props.href) return { href: props.href }
  return { type: props.type, disabled: props.disabled }
})

const showArrow = computed(() => props.variant === 'primary' && props.arrow)
</script>

<template>
  <component
    :is="tag"
    class="base-button"
    :class="[
      `base-button--${variant}`,
      `base-button--${size}`,
      { 'base-button--arrow': showArrow },
    ]"
    v-bind="bind"
  >
    <span class="base-button__label"><slot /></span>
    <svg
      v-if="showArrow"
      class="base-button__arrow"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2.5"
      aria-hidden="true"
    >
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  </component>
</template>

<style scoped>
.base-button {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  height: var(--target-comfort);
  padding: 0 var(--space-4);
  border: none;
  background: transparent;
  text-align: center;
  cursor: pointer;
  transition: background-color var(--dur-ui) var(--ease-standard),
    border-color var(--dur-ui) var(--ease-standard);
}

.base-button--primary {
  justify-content: center;
  background: var(--fg);
  color: var(--bg);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-base);
  letter-spacing: var(--track-button);
  text-transform: uppercase;
}
.base-button--primary:hover {
  background: var(--fg-hover);
}

.base-button--secondary {
  justify-content: center;
  border: 1px solid var(--line-strong);
  color: var(--fg);
  font-family: var(--font-mono);
  font-weight: 400;
  font-size: var(--text-base);
  letter-spacing: var(--track-button);
}
.base-button--secondary:hover {
  border-color: var(--fg-muted);
  background: var(--surface);
}

/* 箭头靠右缘：primary 带箭头时两端对齐 */
.base-button--arrow {
  justify-content: space-between;
}
.base-button__arrow {
  flex: none;
  transition: transform var(--dur-ui) var(--ease-standard);
}
.base-button--arrow:hover .base-button__arrow {
  transform: translateX(var(--space-1));
}

.base-button--sm {
  width: auto;
  height: auto;
  min-width: var(--space-16);
  padding: var(--space-1) var(--space-3);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
}

.base-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
