<script setup lang="ts">
import { ref } from 'vue'
import { useNebulaField } from '~/composables/useNebulaField'

const heroRef = ref<HTMLElement | null>(null)
const canvasRef = ref<HTMLCanvasElement | null>(null)
const anchorRef = ref<HTMLElement | null>(null)

useNebulaField({ heroRef, canvasRef, anchorRef })
</script>

<template>
  <div ref="heroRef" class="nebula">
    <canvas ref="canvasRef" class="nebula__canvas" aria-hidden="true" />
    <!-- 锚点尺寸即雕塑的比例参照；隐藏时雕塑退化为居中、缩小、压暗 -->
    <div ref="anchorRef" class="nebula__anchor" />
  </div>
</template>

<style scoped>
.nebula {
  position: absolute;
  inset: 0;
  overflow: hidden;
}

.nebula__canvas {
  display: block;
  width: 100%;
  height: 100%;
}

/* 指针穿透到 DOM 内容上，画布不接管交互 */
.nebula,
.nebula__canvas {
  pointer-events: none;
}

.nebula__anchor {
  position: absolute;
  top: 50%;
  right: 10%;
  width: clamp(180px, 22vw, 320px);
  aspect-ratio: 1;
  translate: 0 -50%;
}

@media (max-width: 900px) {
  .nebula__anchor {
    display: none;
  }
}
</style>
