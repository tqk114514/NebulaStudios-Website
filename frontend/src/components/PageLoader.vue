<script setup lang="ts">
// 页面加载遮罩（原 .page-loader：fixed 全屏覆盖）。
// 加载动效：Logo 两半 180° 旋转对称——快转半圈停顿再转，"顿挫旋转"。
// 全覆盖含顶栏区域：/admin 无全局站头，且切换期间必须完全遮住页面本身。
// 显示/隐藏逻辑见 composables/usePageLoader.ts（路由懒加载 + 页面数据 + 字体就绪）。
import { pageLoaderVisible } from '@/composables/usePageLoader'

const visible = pageLoaderVisible()
</script>

<template>
  <Transition name="page-loader">
    <div v-if="visible" class="page-loader">
      <svg class="page-loader__logo" viewBox="0 0 256 256" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
        <path d="M56 48H96L144 96L112 128L96 112V208H56V48Z" fill="currentColor" />
        <path d="M200 208H160L112 160L144 128L160 144V48H200V208Z" fill="currentColor" />
      </svg>
    </div>
  </Transition>
</template>

<style scoped>
.page-loader {
  position: fixed;
  inset: 0;
  background: var(--bg);
  display: flex;
  justify-content: center;
  align-items: center;
  /* 全站层级约定：顶栏 100、页面遮罩 110、普通弹窗 200、提示/确认二级弹窗 210 */
  z-index: 110;
}

.page-loader__logo {
  width: 40px;
  height: 40px;
  color: var(--fg);
  transform-origin: 50% 50%;
  /* 转 180° 恰好与原图重合：45%-50% 处顿停，节奏"快转-慢停" */
  animation: page-loader-snap 1.5s cubic-bezier(0.87, 0, 0.13, 1) infinite;
}

@keyframes page-loader-snap {
  0% {
    transform: rotate(0deg);
  }
  45%,
  50% {
    transform: rotate(180deg);
  }
  95%,
  100% {
    transform: rotate(360deg);
  }
}

@media (prefers-reduced-motion: reduce) {
  .page-loader__logo {
    animation: none;
  }
}

.page-loader-enter-active,
.page-loader-leave-active {
  transition: opacity 0.3s, visibility 0.3s;
}

.page-loader-enter-from,
.page-loader-leave-to {
  opacity: 0;
  visibility: hidden;
}
</style>
