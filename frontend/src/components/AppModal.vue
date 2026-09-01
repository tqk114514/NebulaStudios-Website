<script setup lang="ts">
// 基础弹窗（Teleport 到 body，避免被父级 overflow/层级裁剪）。
// 样式迁移自原站 .modal-* 系列（值不变），过渡类在 styles/main.css 全局声明。
// 用法：<AppModal v-model:open="show" title="提示">...</AppModal>
// footer 通过命名插槽传入（置于 .modal-footer 内）；contentClass 用于变体（如危险弹窗红标题）。
import { watch, onBeforeUnmount } from 'vue'

const props = defineProps<{
  open: boolean
  title?: string
  closable?: boolean
  width?: string
  contentClass?: string
  /** 覆盖层层级。默认 200；提示/确认等二级弹窗传 210（叠于业务弹窗之上） */
  zIndex?: number
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  close: []
}>()

// Esc 关闭
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open && props.closable !== false) close()
}
watch(
  () => props.open,
  (v) => (v ? document.addEventListener('keydown', onKeydown) : document.removeEventListener('keydown', onKeydown)),
)
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))

function close() {
  emit('update:open', false)
  emit('close')
}
</script>

<template>
  <Teleport to="body">
    <Transition name="modal-fade">
      <div
        v-if="open"
        class="modal-overlay"
        :style="zIndex ? { zIndex } : undefined"
        @click.self="closable !== false && close()"
      >
        <div class="modal-container" :style="width ? { maxWidth: width } : undefined" role="dialog" aria-modal="true">
          <div class="modal-content" :class="contentClass">
            <h2 v-if="title" class="modal-title">{{ title }}</h2>
            <div class="modal-body">
              <slot />
            </div>
            <div v-if="$slots.footer" class="modal-footer">
              <slot name="footer" />
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.8);
  backdrop-filter: blur(8px);
  display: flex;
  justify-content: center;
  align-items: center;
  /* 全站层级约定：顶栏 100、页面遮罩 110、普通弹窗 200、提示/确认二级弹窗 210 */
  z-index: 200;
}

.modal-container {
  width: 100%;
  max-width: 400px;
  margin: 24px;
}

.modal-content {
  background: var(--bg);
  border: 1px solid var(--line);
  padding: 32px;
}

.modal-title {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-lg);
  letter-spacing: -0.02em;
  color: var(--fg);
  margin-bottom: 16px;
}

.modal-body {
  margin-bottom: 24px;
}

.modal-footer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 危险弹窗变体（删除账户等）：标题红色，对齐原站 .delete-account-modal-content */
.modal-content.danger .modal-title {
  color: var(--error);
}
</style>