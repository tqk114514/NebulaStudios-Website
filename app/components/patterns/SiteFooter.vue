<script setup lang="ts">
// 站点头尾中的尾。政策链接等 /policy 落地后再加：无目标的锚链不如不放。
import { computed } from 'vue'
import { useI18n } from '#imports'

const { t } = useI18n()
const year = computed(() => new Date().getFullYear())
</script>

<template>
  <footer class="site-footer">
    <span class="site-footer__logo" translate="no">Nebula Studios</span>
    <span class="site-footer__copy">{{ t('footer.copyright', { year }) }}</span>
  </footer>
</template>

<style scoped>
.site-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-8) var(--space-16);
  border-top: 1px solid var(--line);
}

.site-footer__logo {
  font-family: var(--font-display);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  text-transform: uppercase;
  color: var(--fg-muted);
}

.site-footer__copy {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--fg-muted);
}

@media (max-width: 900px) {
  .site-footer {
    padding: var(--space-6) var(--space-8);
  }
}
</style>
