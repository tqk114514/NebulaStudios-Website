<script setup lang="ts">
// 宣言区。左栏大字在滚动时钉住，右栏三段带强调的整句文案。
// 强调写在消息文本里用 ** 包裹，而不是拆成 before/strong/after 三个键——语序属于
// 语言本身，拆开后换语言就散架。也不用 <i18n-t>：它按 useScope: 'parent' 沿组件树
// 找局部 scope，而 Nuxt 的消息全在全局 scope，dev 下每个实例警告一次。
import { useI18n } from '#imports'

interface Segment {
  s: string
  em: boolean
}

const EMPHASIS = /^\*\*[^*]+\*\*$/

const { t } = useI18n()

const PARAGRAPHS = ['home.manifesto.text1', 'home.manifesto.text2', 'home.manifesto.text3'] as const

function segments(sentence: string): Segment[] {
  return sentence
    .split(/(\*\*[^*]+\*\*)/)
    .filter((part) => part !== '')
    .map((part) => ({ s: part.replace(/^\*\*|\*\*$/g, ''), em: EMPHASIS.test(part) }))
}
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
      <p
        v-for="(key, i) in PARAGRAPHS"
        :key="key"
        class="manifesto__text"
        :data-reveal="String(i)"
      >
        <template v-for="(seg, j) in segments(t(key))" :key="j">
          <strong v-if="seg.em">{{ seg.s }}</strong>
          <template v-else>{{ seg.s }}</template>
        </template>
      </p>
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
