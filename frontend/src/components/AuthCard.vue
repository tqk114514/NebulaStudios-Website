<script setup lang="ts">
// 认证页卡片骨架（迁移自原站 .card / .card-num / .card-title / .subtitle）。
// 顶部大号序号 + Unbounded 标题 + 副标题，内容通过默认插槽注入。
defineProps<{
  num?: string
  title?: string
  subtitle?: string
}>()
</script>

<template>
  <section class="auth-card">
    <div v-if="num" class="auth-card__num" translate="no">{{ num }}</div>
    <h1 class="auth-card__title">{{ title }}</h1>
    <p v-if="subtitle" class="auth-card__subtitle">{{ subtitle }}</p>
    <slot />
  </section>
</template>

<style scoped>
.auth-card {
  position: relative;
  z-index: 2;
  width: 100%;
  max-width: 400px;
  margin: 0 auto;
  padding: 0 24px;
  padding-top: calc(60px + 10vh);
  padding-bottom: 24px;
  animation: card-enter 0.6s cubic-bezier(0.16, 1, 0.3, 1);
}

.auth-card__num {
  font-family: var(--font-display);
  font-weight: 200;
  font-size: 64px;
  color: var(--line);
  line-height: 1;
  margin-bottom: -4px;
  letter-spacing: -0.04em;
  position: relative;
  top: 12px;
  z-index: -1;
}

.auth-card__title {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: clamp(24px, 3vw, 32px);
  letter-spacing: -0.02em;
  color: var(--fg);
  margin-bottom: 8px;
}

.auth-card__subtitle {
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  color: var(--mid);
  margin-bottom: 40px;
  line-height: 1.8;
}

@keyframes card-enter {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>