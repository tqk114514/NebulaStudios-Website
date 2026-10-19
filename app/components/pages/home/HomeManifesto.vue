<script setup lang="ts">
// 宣言区。左栏大字在滚动时钉住，右栏三段带强调的整句文案。
// 强调部分用 <i18n-t> 的具名插槽而不是把一句拆成 before/strong/after 三个键：
// 语序属于语言本身，拆开后换语言就散架。
import { useI18n } from '#imports'

const { t } = useI18n()
</script>

<template>
  <section class="manifesto">
    <div class="manifesto__sticky" data-reveal>
      <p class="manifesto__quote" translate="no">
        YOUR<br>
        <span class="manifesto__accent">DATA</span><br>
        YOUR<br>
        RULES
      </p>
    </div>

    <div class="manifesto__body">
      <i18n-t
        keypath="home.manifesto.text1"
        tag="p"
        class="manifesto__text"
        data-reveal>
        <template #strong>
          <strong>{{ t('home.manifesto.text1Strong') }}</strong>
        </template>
      </i18n-t>

      <i18n-t
        keypath="home.manifesto.text2"
        tag="p"
        class="manifesto__text"
        data-reveal="1">
        <template #strong>
          <strong>{{ t('home.manifesto.text2Strong') }}</strong>
        </template>
      </i18n-t>

      <i18n-t
        keypath="home.manifesto.text3"
        tag="p"
        class="manifesto__text"
        data-reveal="2">
        <template #infrastructure>
          <strong>{{ t('home.manifesto.text3Infrastructure') }}</strong>
        </template>
        <template #principle>
          <strong>{{ t('home.manifesto.text3Principle') }}</strong>
        </template>
      </i18n-t>
    </div>
  </section>
</template>

<style scoped>
.manifesto {
  display: grid;
  grid-template-columns: 1fr 2fr;
  gap: var(--space-20);
  align-items: start;
  padding: var(--space-30) var(--space-16);
  border-top: 1px solid var(--line);
}

/* 头部 60px 固定，钉住位置在它之下再留 --space-5 */
.manifesto__sticky {
  position: sticky;
  top: calc(var(--header-h) + var(--space-5));
}

.manifesto__quote {
  font-family: var(--font-display);
  font-size: var(--text-display);
  font-weight: 900;
  line-height: var(--lh-display);
  letter-spacing: var(--track-title);
  text-transform: uppercase;
  color: var(--fg);
}

.manifesto__accent {
  color: var(--fg-muted);
}

.manifesto__text {
  margin-bottom: var(--space-8);
  font-size: var(--text-lead);
  line-height: 1.9;
  color: var(--fg-muted);
}

.manifesto__text :deep(strong) {
  font-weight: 600;
  color: var(--fg);
}

@media (max-width: 900px) {
  .manifesto {
    grid-template-columns: 1fr;
    gap: var(--space-10);
    padding: var(--space-20) var(--space-8);
  }

  .manifesto__sticky {
    position: static;
  }
}
</style>
