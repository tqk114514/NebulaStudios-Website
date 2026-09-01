<script setup lang="ts">
// 确认对话框：基于 AppModal，两步交互。
// 样式自持（scoped）：正文样式迁移自原站 .modal-message（值不变）；
// 按钮用 AppButton（取消=secondary / 确认=primary 无箭头居中，等价旧 .modal-footer 覆盖）。
// danger 变体：靠 AppModal 标题红表达危险（旧站无红色主按钮），确认按钮保持 primary。
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'

withDefaults(
  defineProps<{
    open: boolean
    title?: string
    content?: string
    confirmText?: string
    cancelText?: string
    danger?: boolean
  }>(),
  {
    title: '',
    content: '',
    confirmText: '',
    cancelText: '',
    danger: false,
  },
)

const emit = defineEmits<{
  'update:open': [value: boolean]
  confirm: []
  cancel: []
}>()

function onConfirm() {
  emit('update:open', false)
  emit('confirm')
}
function onCancel() {
  emit('update:open', false)
  emit('cancel')
}
</script>

<template>
  <AppModal
    :open="open"
    :title="title"
    closable
    :content-class="danger ? 'danger' : undefined"
    @update:open="(v) => emit('update:open', v)"
  >
    <p v-if="content" class="confirm-dialog__message">{{ content }}</p>
    <slot />
    <template #footer>
      <slot name="actions">
        <AppButton variant="secondary" @click="onCancel">{{ cancelText }}</AppButton>
        <AppButton variant="primary" :arrow="false" @click="onConfirm">{{ confirmText }}</AppButton>
      </slot>
    </template>
  </AppModal>
</template>

<style scoped>
/* 迁移自原站 .modal-message（值不变） */
.confirm-dialog__message {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
</style>
