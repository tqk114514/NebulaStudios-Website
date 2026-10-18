<script setup lang="ts">
// 认证页面板骨架：序号 + 标题 + 副标题，正文由默认插槽注入。
// num 仅用于真实存在的步骤序列（单步页面不要传），否则退化成装饰编号。
withDefaults(
  defineProps<{
    num?: string
    title: string
    subtitle?: string
  }>(),
  { num: '', subtitle: '' },
)
</script>

<template>
  <section class="auth-panel">
    <span v-if="num" class="auth-panel__num" aria-hidden="true">{{ num }}</span>
    <h1 class="auth-panel__title">{{ title }}</h1>
    <p v-if="subtitle" class="auth-panel__subtitle">{{ subtitle }}</p>
    <slot />
  </section>
</template>

<style scoped>
.auth-panel {
  width: 100%;
  max-width: var(--panel-max);
  margin-inline: auto;
  padding: var(--space-16) var(--space-6) var(--space-6);
  animation: panel-enter var(--dur-reveal) var(--ease-emphasize);
}

.auth-panel__num {
  position: relative;
  z-index: 0;
  display: block;
  margin-bottom: -4px;
  font-family: var(--font-display);
  font-weight: 200;
  font-size: var(--text-num);
  line-height: 1;
  letter-spacing: var(--track-tight);
  color: var(--line);
}

.auth-panel__title {
  position: relative;
  z-index: 1;
  margin-bottom: var(--space-2);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-title);
  line-height: var(--lh-heading);
  letter-spacing: var(--track-title);
  color: var(--fg);
}

.auth-panel__subtitle {
  margin-bottom: var(--space-10);
  font-size: var(--text-base);
  line-height: 1.8;
  letter-spacing: var(--track-wide);
  color: var(--fg-muted);
}

@keyframes panel-enter {
  from {
    opacity: 0;
    transform: translateY(var(--reveal-shift));
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
