<script setup lang="ts">
// Toast 容器：配合 src/composables/useToast.ts 使用，全局只渲染一个实例。
// 配色对齐原站暗色体系（--bg / --line / --fg / --error）。
import { useToastState } from '@/composables/useToast'
const { list } = useToastState()
</script>

<template>
  <Teleport to="body">
    <div class="toast-container">
      <TransitionGroup name="toast">
        <div v-for="t in list" :key="t.id" class="toast" :class="`toast-${t.type}`">
          {{ t.message }}
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-container {
  position: fixed;
  top: 72px;
  left: 50%;
  transform: translateX(-50%);
  /* 全站层级约定：顶栏 100、页面遮罩 110、普通弹窗 200、提示/确认二级弹窗 210、Toast 220 */
  z-index: 220;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  pointer-events: none;
}
.toast {
  padding: 12px 20px;
  background: var(--bg);
  border: 1px solid var(--line);
  color: var(--fg);
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  letter-spacing: 0.08em;
}
.toast-error {
  border-color: var(--error);
  color: var(--error-bright);
}
.toast-success {
  border-color: var(--success);
  color: var(--success);
}
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>