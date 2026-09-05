<script setup lang="ts">
// 全站按钮组件（迁移自原站 .button-primary / .button-secondary，值不变、命名现代化）。
// primary：白底黑字 + 右端箭头（弹窗 footer 等居中场景传 :arrow="false"）
// secondary：透明底 + 描边
// size sm 对应原站 .oauth-grant-revoke 一类的小按钮
// disabled / 原生属性直接透传到 <button>
withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary'
    type?: 'button' | 'submit'
    arrow?: boolean
    block?: boolean
    size?: 'md' | 'sm'
  }>(),
  { variant: 'primary', type: 'button', arrow: true, block: true, size: 'md' },
)
</script>

<template>
  <button
    :type="type"
    class="app-button"
    :class="[
      `app-button--${variant}`,
      `app-button--${size}`,
      { 'app-button--block': block, 'app-button--arrow': variant === 'primary' && arrow },
    ]"
  >
    <slot />
    <svg
      v-if="variant === 'primary' && arrow"
      class="app-button__arrow"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2.5"
    >
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  </button>
</template>

<style scoped>
.app-button {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s, transform 0.1s;
}

/* ---- primary：白底黑字 ---- */
.app-button--primary {
  justify-content: center;
  height: 48px;
  padding: 0 20px;
  background: var(--fg);
  border: none;
  color: var(--bg);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.app-button--primary:hover {
  background: var(--fg-hover);
}
.app-button--arrow {
  justify-content: space-between;
}
.app-button__arrow {
  flex-shrink: 0;
  transition: transform 0.25s;
}
.app-button--arrow:hover .app-button__arrow {
  transform: translateX(4px);
}

/* ---- secondary：透明底描边 ---- */
.app-button--secondary {
  justify-content: center;
  height: 48px;
  padding: 0 20px;
  background: transparent;
  border: 1px solid var(--dim);
  color: var(--fg);
  font-family: var(--font-mono);
  font-weight: 400;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
}
.app-button--secondary:hover {
  border-color: var(--mid);
  background: var(--dim);
}

/* ---- 尺寸 / 布局 ---- */
.app-button--block {
  width: 100%;
}
.app-button--sm {
  width: auto;
  height: auto;
  min-width: 60px;
  padding: 6px 12px;
  font-size: var(--text-xs);
  letter-spacing: 0.12em;
}

.app-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>