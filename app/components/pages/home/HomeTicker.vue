<script setup lang="ts">
// 首页跑马灯。页面私有件，显式 import（`app/components/pages/` 不在自动导入扫描内）。
import { ref } from 'vue'
import { useTicker } from '~/composables/useTicker'

const rootRef = ref<HTMLElement | null>(null)
const { duration, itemCount } = useTicker(rootRef)
</script>

<template>
  <div
    ref="rootRef"
    class="ticker"
    :style="{ '--ticker-duration': duration }"
    aria-hidden="true"
  >
    <div v-for="track in 2" :key="track" class="ticker__track">
      <span v-for="item in itemCount" :key="item" class="ticker__item" translate="no">NEBULA</span>
    </div>
  </div>
</template>

<style scoped>
/*
 * 两条等宽轨道并排，各自平移自身宽度的 -100%：复位瞬间第一条恰好完全移出、
 * 第二条顶到左边缘，接缝位置与第二条起点重合，误差为零。
 * 时长由 useTicker 写在 --ticker-duration 上，回退值只服务首帧与字体加载前。
 */
.ticker {
  position: relative;
  z-index: 2;
  display: flex;
  overflow: hidden;
  padding: var(--space-3) 0;
  white-space: nowrap;
  background: var(--bg);
  border-block: 1px solid var(--line);
}

.ticker__track {
  display: flex;
  flex-shrink: 0;
  will-change: transform;
  animation: ticker-scroll var(--ticker-duration, 120s) linear infinite;
}

@keyframes ticker-scroll {
  to {
    transform: translateX(-100%);
  }
}

/* 条目左右等距：轨道首尾相接时，接缝处的间距与条目之间完全一致 */
.ticker__item {
  padding: 0 var(--space-12);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-xs);
  letter-spacing: 0.35em;
  text-transform: uppercase;
  color: var(--fg-muted);
  white-space: nowrap;
}

@media (prefers-reduced-motion: reduce) {
  .ticker__track {
    animation: none;
  }
}
</style>
