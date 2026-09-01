<script setup lang="ts">
// 页面加载遮罩（迁移自原站 .page-loader：fixed 顶栏以下全覆盖 + 居中 spinner）。
// 显示/隐藏逻辑见 composables/usePageLoader.ts（路由懒加载 + 页面数据 + 字体就绪）。
import { pageLoaderVisible } from '@/composables/usePageLoader'

const visible = pageLoaderVisible()
</script>

<template>
  <Transition name="page-loader">
    <div v-if="visible" class="page-loader">
      <div class="page-loader__spinner"></div>
    </div>
  </Transition>
</template>

<style scoped>
.page-loader {
  position: fixed;
  inset: 60px 0 0;
  background: var(--bg);
  display: flex;
  justify-content: center;
  align-items: center;
  /* 全站层级约定：顶栏 100、页面遮罩 110、普通弹窗 200、提示/确认二级弹窗 210 */
  z-index: 110;
}

.page-loader__spinner {
  width: 32px;
  height: 32px;
  border: 2px solid var(--line);
  border-top-color: var(--fg);
  border-radius: 50%;
  animation: page-loader-spin 0.8s linear infinite;
}

@keyframes page-loader-spin {
  to {
    transform: rotate(360deg);
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