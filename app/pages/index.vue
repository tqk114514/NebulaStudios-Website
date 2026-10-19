<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from '#imports'
import HomeCta from '~/components/pages/home/HomeCta.vue'
import HomeFeatures from '~/components/pages/home/HomeFeatures.vue'
import HomeManifesto from '~/components/pages/home/HomeManifesto.vue'
import HomeTicker from '~/components/pages/home/HomeTicker.vue'
import { useNebulaField } from '~/composables/useNebulaField'
import { useReveal } from '~/composables/useReveal'

const { t } = useI18n()

const pageRef = ref<HTMLElement | null>(null)
const heroRef = ref<HTMLElement | null>(null)
const canvasRef = ref<HTMLCanvasElement | null>(null)
const anchorRef = ref<HTMLElement | null>(null)

useNebulaField({ heroRef, canvasRef, anchorRef })
useReveal(pageRef)
</script>

<template>
  <main ref="pageRef">
    <section ref="heroRef" class="hero">
      <canvas ref="canvasRef" class="hero__canvas" aria-hidden="true" />

      <div class="hero__left">
        <h1 class="hero__title enter" style="--i: 2" translate="no">
          ONE<br>IDENTITY<br><em>Everywhere</em>
        </h1>
        <p class="hero__desc enter" style="--i: 3">{{ t('home.hero.description') }}</p>
        <div class="hero__cta enter" style="--i: 4">
          <BaseButton :arrow="false" to="/account/register">{{ t('home.hero.createAccount') }}</BaseButton>
          <BaseButton variant="secondary" to="/account/login">
            {{ t('home.hero.login') }}
          </BaseButton>
        </div>
      </div>

      <div class="hero__right enter" style="--i: 3">
        <!-- 雕塑中心投影到此方块，尺寸随它变化；窄屏隐藏后雕塑退化为居中缩小 -->
        <div ref="anchorRef" class="hero__anchor" aria-hidden="true" />
      </div>
    </section>

    <HomeTicker />
    <HomeFeatures />
    <HomeManifesto />
    <HomeCta />
  </main>
</template>

<style scoped>
.hero {
  position: relative;
  display: grid;
  /* 两栏必须严格等宽；1fr 的最小尺寸是 auto，会被标题最长词撑偏 */
  grid-template-columns: repeat(2, minmax(0, 1fr));
  /* 顶栏偏移由 layouts/default.vue 承担，这里只填满剩余视口 */
  min-height: calc(100svh - var(--header-h));
  overflow: hidden;
}

/*
 * 分栏中线。v3 从 top: 60px 起画，是因为它的 hero 自己吞掉头部偏移；
 * v4 的偏移在 layouts/default.vue，hero 顶边即内容顶边，所以从 0 起。
 * 画布是不透明的 WebGL 层且为 hero 首个子元素，定位元素按树序绘制，
 * 中线取 z-index 1 才压在画布之上、文字之下。
 */
.hero::before {
  content: '';
  position: absolute;
  left: 50%;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--line);
  z-index: 1;
}

.hero__canvas {
  position: absolute;
  inset: 0;
  display: block;
  width: 100%;
  height: 100%;
  pointer-events: none;
  /*
   * WebGL 在首次绘制前是透明的，首帧整帧落下会读作"啪"地亮起来。淡入从样式生效
   * 就开始计时而不是等出帧，正好把 hydration 到首帧之间那段启动耗时接进同一段
   * 入场节奏里；reduced-motion 由 base.css 的全局规则统一收掉。
   */
  animation: canvas-enter var(--dur-enter) var(--ease-brand) both;
}

@keyframes canvas-enter {
  from {
    opacity: 0;
  }
}

/* 画布是不透明的第一层，文字需抬到它之上 */
.hero__left,
.hero__right {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
}

.hero__left {
  justify-content: flex-end;
  padding: var(--space-30) var(--space-16) var(--space-20) var(--space-20);
}

.hero__right {
  align-items: flex-end;
  justify-content: center;
  padding: var(--space-30) var(--space-20) var(--space-20) var(--space-16);
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
}

.hero__desc {
  max-width: 380px;
  margin-top: var(--space-10);
  font-size: var(--text-lead);
  line-height: 1.8;
  color: var(--fg-muted);
}

.hero__cta {
  display: flex;
  gap: var(--space-4);
  margin-top: var(--space-14);
}

.hero__anchor {
  width: min(480px, 42vw);
  aspect-ratio: 1;
  position: relative;
  pointer-events: none;
}

@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
  }

  .hero::before,
  .hero__right {
    display: none;
  }

  .hero__left {
    padding: var(--space-16) var(--page-pad) var(--space-16);
  }
}
</style>
