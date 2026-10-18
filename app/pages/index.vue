<script setup lang="ts">
import { useI18n } from '#imports'

const { t } = useI18n()
</script>

<template>
  <main>
    <section class="hero">
      <NebulaHero />

      <div class="hero__content">
        <h1 class="hero__title">{{ t('home.title') }}</h1>
        <p class="hero__subtitle">{{ t('home.subtitle') }}</p>
        <div class="hero__actions">
          <BaseButton class="hero__cta" to="/account/login">
            {{ t('home.cta') }}
          </BaseButton>
          <a class="hero__link" href="#capabilities">{{ t('home.howItWorks') }}</a>
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
  max-width: var(--measure);
  display: grid;
  gap: var(--space-6);
}

.hero__title {
  font-family: var(--font-display);
  font-size: var(--text-hero);
  font-weight: 700;
  line-height: var(--lh-heading);
  letter-spacing: var(--track-none);
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
  width: auto;
  min-width: 200px;
}

.hero__link {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
}

.hero__link:hover {
  color: var(--fg);
}
</style>
