<script setup lang="ts">
// 收尾 CTA。按钮复用 BaseButton 的链接形态，不另写一套 cta-btn。
import { useI18n } from '#imports'

const { t } = useI18n()
</script>

<template>
  <section class="cta">
    <h2 class="cta__title" translate="no" data-reveal>
      Start<br>
      <em>Today</em><br>
      Free
    </h2>

    <div class="cta__side" data-reveal="1">
      <p class="cta__body">{{ t('home.cta.description') }}</p>
      <div class="cta__actions">
        <BaseButton :arrow="false" to="/account/register">{{ t('home.cta.registerNow') }}</BaseButton>
        <BaseButton variant="secondary" to="/account/login">
          {{ t('home.cta.alreadyHaveAccount') }}
        </BaseButton>
      </div>
    </div>
  </section>
</template>

<style scoped>
.cta {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-20);
  align-items: center;
  padding: var(--space-30) var(--space-16);
  border-top: 1px solid var(--line);
}

.cta__title {
  font-family: var(--font-display);
  font-size: var(--text-2xl);
  font-weight: 900;
  line-height: var(--lh-display);
  letter-spacing: var(--track-title);
  text-transform: uppercase;
  color: var(--fg);
}

.cta__title em {
  display: block;
  font-family: var(--font-serif);
  font-size: 0.8em;
  font-style: italic;
  font-weight: 300;
}

.cta__side {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
}

.cta__body {
  max-width: 420px;
  font-size: var(--text-md);
  line-height: var(--lh-prose);
  color: var(--fg-muted);
}

.cta__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}

@media (max-width: 900px) {
  .cta {
    grid-template-columns: 1fr;
    gap: var(--space-8);
    padding: var(--space-20) var(--space-8);
  }
}
</style>
