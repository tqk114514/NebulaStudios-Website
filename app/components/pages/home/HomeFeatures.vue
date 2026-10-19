<script setup lang="ts">
// 能力一览。取词写成 `() => t(键)` 而不是把键存进变量再间接传入：
// §3 第 4 条的缺项检查靠扫 t() 调用的字面量实参，间接引用会让键从检查眼里消失。
import { useI18n } from '#imports'

const { t } = useI18n()

const FEATURES = [
  { n: '01', name: () => t('home.feature.identitySecurity.name'), desc: () => t('home.feature.identitySecurity.desc') },
  { n: '02', name: () => t('home.feature.profile.name'), desc: () => t('home.feature.profile.desc') },
  { n: '03', name: () => t('home.feature.thirdParty.name'), desc: () => t('home.feature.thirdParty.desc') },
  { n: '04', name: () => t('home.feature.activityLog.name'), desc: () => t('home.feature.activityLog.desc') },
  { n: '05', name: () => t('home.feature.oauth.name'), desc: () => t('home.feature.oauth.desc') },
  { n: '06', name: () => t('home.feature.dataExport.name'), desc: () => t('home.feature.dataExport.desc') },
] as const

const COLUMNS = 3
</script>

<template>
  <section class="features">
    <div class="features__head">
      <div data-reveal>
        <p class="features__label">{{ t('home.features.label') }}</p>
        <h2 class="features__title" translate="no">
          Everything<br>
          You <em>Need</em>
        </h2>
      </div>
      <p class="features__body" data-reveal="1">{{ t('home.features.description') }}</p>
    </div>

    <div class="features__grid">
      <article
        v-for="(f, i) in FEATURES"
        :key="f.n"
        class="features__card"
        :data-reveal="String(i % COLUMNS)"
      >
        <p class="features__num" translate="no">{{ f.n }}</p>
        <h3 class="features__name">{{ f.name() }}</h3>
        <p class="features__desc">{{ f.desc() }}</p>
      </article>
    </div>
  </section>
</template>

<style scoped>
.features {
  padding: var(--space-30) var(--space-16);
  border-top: 1px solid var(--line);
}

.features__head {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-16);
  align-items: end;
  margin-bottom: var(--space-20);
}

.features__label {
  margin-bottom: var(--space-5);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-caps);
  text-transform: uppercase;
  color: var(--fg-muted);
}

/* 分节前缀由内容生成，不是写进文案的标点 */
.features__label::before {
  content: '//';
  margin-right: var(--space-2);
  opacity: 0.5;
}

.features__title {
  font-family: var(--font-display);
  font-size: var(--text-2xl);
  font-weight: 700;
  line-height: var(--lh-section);
  letter-spacing: var(--track-title);
  text-transform: uppercase;
  color: var(--fg);
}

.features__title em {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 300;
}

.features__body {
  max-width: 400px;
  font-size: var(--text-md);
  line-height: var(--lh-prose);
  color: var(--fg-muted);
}

/*
 * 分隔线由容器底色透过 1px gap 形成：网格少一列时不会留下断线，
 * 也不需要 v3 那三条 nth-child 边界规则。
 */
.features__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1px;
  background: var(--line);
  border-top: 1px solid var(--line);
}

.features__card {
  position: relative;
  overflow: hidden;
  padding: var(--space-12) var(--space-10);
  background: var(--bg);
}

/* 底色从底部涨起，文字浮在其上（z-index 让内容压过 ::before） */
.features__card::before {
  content: '';
  position: absolute;
  inset: 0;
  background: var(--surface-raised);
  transform: scaleY(0);
  transform-origin: bottom;
  transition: transform var(--dur-panel) var(--ease-standard);
}

.features__card:hover::before {
  transform: scaleY(1);
}

.features__num,
.features__name,
.features__desc {
  position: relative;
}

.features__num {
  margin-bottom: var(--space-8);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-wide);
  color: var(--fg-muted);
}

.features__name {
  margin-bottom: var(--space-3);
  font-family: var(--font-display);
  font-size: var(--text-md);
  font-weight: 700;
  letter-spacing: var(--track-none);
  text-transform: uppercase;
  color: var(--fg);
}

.features__desc {
  font-size: var(--text-base);
  line-height: var(--lh-prose);
  color: var(--fg-muted);
}

@media (max-width: 900px) {
  .features {
    padding: var(--space-20) var(--space-8);
  }

  .features__head {
    grid-template-columns: 1fr;
    gap: var(--space-6);
  }

  .features__grid {
    grid-template-columns: 1fr;
  }
}
</style>
