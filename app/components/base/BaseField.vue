<script setup lang="ts">
// 表单字段：标签、控件、提示与错误行的唯一出处。
// 控件由本组件渲染而非插槽下发，id 关联因此可以在编译期确定。
// 口令类型自带显示切换（NIST SP 800-63B 的 SHOULD 项）。
import { computed, ref, useId } from 'vue'
import { useI18n } from '#imports'

const model = defineModel<string>({ required: true })

const props = withDefaults(
  defineProps<{
    label: string
    name: string
    type?: 'text' | 'email' | 'tel' | 'password' | 'number'
    autocomplete?: string
    error?: string
    hint?: string
    required?: boolean
    disabled?: boolean
    maxlength?: number
    inputmode?: 'text' | 'numeric' | 'email' | 'tel'
  }>(),
  {
    type: 'text',
    autocomplete: 'off',
    error: '',
    hint: '',
    required: false,
    disabled: false,
    maxlength: undefined,
    inputmode: undefined,
  },
)

const uid = useId()
const { t } = useI18n()
const controlId = computed(() => `${uid}-control`)
// 错误优先于提示：这行同时决定控件的 aria-describedby 与下方消息行的 id/class/role
const messageKind = computed(() => (props.error ? 'error' : props.hint ? 'hint' : ''))
const message = computed(() => props.error || props.hint)
const describedBy = computed(() => (messageKind.value ? `${uid}-${messageKind.value}` : undefined))

const revealed = ref(false)
const isSecret = computed(() => props.type === 'password')
const controlType = computed(() =>
  isSecret.value && revealed.value ? 'text' : props.type,
)

const numeric = computed(() => props.type === 'number' || props.inputmode === 'numeric')

function toggleReveal() {
  revealed.value = !revealed.value
}
</script>

<template>
  <div class="base-field" :class="{ 'base-field--error': !!props.error }">
    <label class="base-field__label" :for="controlId">{{ props.label }}</label>

    <div class="base-field__control">
      <input
        :id="controlId"
        v-model="model"
        class="base-field__input"
        :class="{ 'base-field__input--code': numeric }"
        :name="props.name"
        :type="controlType"
        :autocomplete="props.autocomplete"
        :inputmode="props.inputmode"
        :maxlength="props.maxlength"
        :required="props.required"
        :disabled="props.disabled"
        :aria-invalid="props.error ? 'true' : undefined"
        :aria-describedby="describedBy"
      >

      <button
        v-if="isSecret"
        class="base-field__reveal"
        type="button"
        :aria-pressed="revealed"
        @click="toggleReveal"
      >
        {{ revealed ? t('form.hidePassword') : t('form.showPassword') }}
      </button>
    </div>

    <p
      v-if="message"
      :id="`${uid}-${messageKind}`"
      :class="`base-field__${messageKind}`"
      :role="messageKind === 'error' ? 'alert' : undefined"
    >{{ message }}</p>
  </div>
</template>

<style scoped>
.base-field {
  display: grid;
  gap: var(--space-2);
}

.base-field__label {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-caps);
  text-transform: uppercase;
  color: var(--fg-muted);
}

.base-field__control {
  position: relative;
  display: flex;
  align-items: center;
}

.base-field__input {
  width: 100%;
  min-height: var(--target-comfort);
  padding: var(--space-2) 0;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--line-strong);
  font-family: var(--font-sans);
  font-weight: 400;
  font-size: var(--text-md);
  color: var(--fg);
  transition: border-color var(--dur-ui) var(--ease-standard);
}
.base-field__input::placeholder {
  color: var(--fg-faint);
}
.base-field__input:hover {
  border-color: var(--fg-faint);
}
.base-field__input:focus {
  border-color: var(--fg);
}
.base-field__input:focus-visible {
  border-bottom-width: 2px;
  padding-bottom: calc(var(--space-2) - 1px);
}

/* 动态码：单框 + 字距分格，六个分格会破坏自动填充与退格 */
.base-field__input--code {
  font-family: var(--font-mono);
  font-size: var(--text-lg);
  letter-spacing: 0.5em;
}

.base-field--error .base-field__input {
  border-color: var(--error);
}

/* 切换按钮浮在控件右端，明文状态下的输入内容需要从它下面让开 */
.base-field__control:has(.base-field__reveal) .base-field__input {
  padding-right: var(--space-24);
}

.base-field__reveal {
  position: absolute;
  right: 0;
  min-width: var(--target-min);
  min-height: var(--target-min);
  padding: 0 var(--space-1);
  border: none;
  background: transparent;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
  cursor: pointer;
}
.base-field__reveal:hover {
  color: var(--fg);
}

.base-field__hint {
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
}

.base-field__error {
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--error);
}

/* Chrome 对已填充字段强加底色，会盖掉 --bg */
.base-field__input:-webkit-autofill,
.base-field__input:-webkit-autofill:hover,
.base-field__input:-webkit-autofill:focus {
  -webkit-text-fill-color: var(--fg);
  -webkit-box-shadow: 0 0 0 100vh var(--bg) inset;
  caret-color: var(--fg);
  transition: background-color 99999s;
}
</style>
