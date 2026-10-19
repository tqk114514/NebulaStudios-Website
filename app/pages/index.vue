<script setup lang="ts">
import { useI18n } from '#imports'

const { t } = useI18n()
</script>

<template>
  <main>
    <section class="hero">
      <NebulaHero />

      <div class="hero__content">
        <h1 class="hero__title" translate="no">
          ONE<br>IDENTITY<br><em>Everywhere</em>
        </h1>
        <p class="hero__subtitle">{{ t('home.subtitle') }}</p>
        <div class="hero__actions">
          <BaseButton class="hero__cta" to="/account/login">
            {{ t('home.cta') }}
          </BaseButton>
        </div>
      </div>
    </section>
  </main>
</template>

<style scoped>
.hero {
  position: relative;
  display: grid;
  align-items: center;
  min-height: calc(100svh - var(--header-h));
  padding: var(--space-16) var(--page-pad);
}

.hero__content {
  position: relative;
  z-index: 1;
  /* 轨道不跟随最长词，正文的行长由 .hero__subtitle 的 --measure 约束 */
  grid-template-columns: minmax(0, 1fr);
  display: grid;
  gap: var(--space-6);
}

.hero__title {
  font-family: var(--font-display);
  font-size: var(--text-hero);
  font-weight: 900;
  line-height: var(--lh-display);
  letter-spacing: var(--track-title);
  text-transform: uppercase;
}

/* 第三行换衬线斜体：这行混排是 hero 的主要识别特征 */
.hero__title em {
  display: block;
  font-family: var(--font-serif);
  font-size: 0.85em;
  font-weight: 300;
  font-style: italic;
  letter-spacing: var(--track-none);
  text-transform: none;
}

.hero__subtitle {
  max-width: var(--measure);
  color: var(--fg-muted);
}

.hero__actions {
  display: flex;
  align-items: center;
  gap: var(--space-6);
  margin-top: var(--space-2);
}

.hero__cta {
  min-width: 200px;
}
</style>
