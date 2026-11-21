<script setup lang="ts">
// 表单字段：标签、控件、提示与错误行的唯一出处。
// 控件由本组件渲染而非插槽下发，id 关联因此可以在编译期确定。
// 口令类型自带显示切换（NIST SP 800-63B 的 SHOULD 项）。
import { computed, ref, useId } from 'vue'

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
</script>

<template>
  <div class="base-field" :class="{ 'base-field--error': !!props.error }">
    <div class="base-field__control" :class="{ 'base-field__control--secret': isSecret }">
      <input
        :id="controlId"
        v-model="model"
        class="base-field__input"
        :class="{ 'base-field__input--code': numeric }"
        :name="props.name"
        :type="controlType"
        placeholder=" "
        :autocomplete="props.autocomplete"
        :inputmode="props.inputmode"
        :maxlength="props.maxlength"
        :required="props.required"
        :disabled="props.disabled"
        :aria-invalid="props.error ? 'true' : undefined"
        :aria-describedby="describedBy"
      >
      <label class="base-field__label" :for="controlId">{{ props.label }}</label>

      <BasePasswordReveal v-if="isSecret" v-model="revealed" />
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

.base-field__control {
  position: relative;
  display: flex;
  align-items: center;
}

.base-field__input {
  width: 100%;
  min-height: var(--target-comfort);
  padding: var(--space-5) 0 var(--space-1);
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
  color: transparent;
}
.base-field__input:hover {
  border-color: var(--fg-faint);
}
.base-field__input:focus {
  border-color: var(--fg);
}
.base-field__input:focus-visible {
  border-bottom-width: 2px;
  padding-bottom: calc(var(--space-1) - 1px);
}

/* 标签初始在框内；聚焦或有值时浮到上沿，保持可见（Modern Web Guidance: forms） */
.base-field__label {
  position: absolute;
  inset-inline-start: 0;
  top: 50%;
  transform: translateY(-50%);
  font-family: var(--font-sans);
  font-size: var(--text-md);
  letter-spacing: var(--track-body);
  color: var(--fg-faint);
  pointer-events: none;
  transition:
    top var(--dur-ui) var(--ease-standard),
    transform var(--dur-ui) var(--ease-standard),
    font-size var(--dur-ui) var(--ease-standard),
    letter-spacing var(--dur-ui) var(--ease-standard),
    color var(--dur-ui) var(--ease-standard);
}
.base-field__input:focus + .base-field__label,
.base-field__input:not(:placeholder-shown) + .base-field__label {
  top: 0;
  transform: none;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-caps);
  text-transform: uppercase;
  color: var(--fg-muted);
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

/* 图标按钮宽度与语言无关；输入内容统一让出 48px */
.base-field__control--secret .base-field__input {
  padding-right: var(--space-12);
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
