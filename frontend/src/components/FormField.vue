<script setup lang="ts">
// 表单字段（迁移自原站 .form-group 系列：底部划线输入 + 行内错误）。
// 用法：<FormField label="邮箱" :error="err"><input v-model="x" /></FormField>
// label 默认渲染为 sr-only（原站视觉上只有 placeholder），需要可见 label 时传 visible-label。
import { computed, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    label?: string
    error?: string
    hint?: string
    visibleLabel?: boolean
  }>(),
  { label: '', error: '', hint: '', visibleLabel: false },
)

const inputId = useId()
const labelClass = computed(() => (props.visibleLabel ? 'field-label' : 'field-label sr-only'))
</script>

<template>
  <div class="form-field" :class="{ 'form-field--error': !!error }">
    <label v-if="label" :class="labelClass" :for="inputId">{{ label }}</label>
    <slot :id="inputId" />
    <p v-if="hint && !error" class="field-hint">{{ hint }}</p>
    <p v-if="error" class="field-error">{{ error }}</p>
  </div>
</template>

<style scoped>
.form-field {
  margin-bottom: 24px;
}

.field-label {
  display: block;
  font-size: var(--text-xs);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--mid);
  margin-bottom: 8px;
}

.form-field :deep(input) {
  width: 100%;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--dim);
  padding: 0 0 12px;
  font-family: var(--font-mono);
  font-weight: 300;
  font-size: var(--text-md);
  color: var(--fg);
  letter-spacing: 0.06em;
  outline: none;
  transition: border-color 0.2s;
}

.form-field :deep(input)::placeholder {
  color: var(--dim);
}

.form-field :deep(input:focus) {
  border-color: var(--fg);
}

/* 键盘导航时给出明确焦点环（鼠标点击仅有下边框变色） */
.form-field :deep(input:focus-visible) {
  outline: 1px solid var(--fg);
  outline-offset: 4px;
}

.form-field--error :deep(input) {
  border-color: var(--error);
}

.field-error {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--error);
  margin-top: 8px;
}

.field-hint {
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  margin-top: 8px;
}
</style>