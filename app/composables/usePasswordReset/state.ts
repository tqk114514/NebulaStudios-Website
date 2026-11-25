import { computed, ref, watch } from 'vue'
import { useI18n } from '#imports'
import type { ApiErrorBody } from '@nebula/api-client'
import { useApi } from '~/composables/useApi'
import { apiMessageKey, firstFieldError } from '~/utils/api-message'
import { fieldMessageKey } from '~/utils/field-message'
import { checkPassword, passwordIssueKey } from '~/utils/password'
import { confirmReset, requestResetCode } from './requests'

export type PasswordResetStep = 'email' | 'reset'

/** 忘记密码两步：发码 → 验证码 + 新口令；发码结果不区分账户是否存在。 */
export function usePasswordResetState() {
  const { t } = useI18n()
  const api = useApi()
  const email = ref('')
  const code = ref('')
  const password = ref('')
  const step = ref<PasswordResetStep>('email')
  const attemptedEmail = ref(false)
  const attemptedReset = ref(false)
  const pending = ref(false)
  const completed = ref(false)
  const formError = ref('')
  const serverField = ref<{ field: string; text: string } | null>(null)

  const fieldText = (field: string) =>
    serverField.value?.field === field ? serverField.value.text : ''
  const emailError = computed(() =>
    fieldText('email') || (attemptedEmail.value && !email.value.trim() ? t('forgot.emailMissing') : ''),
  )
  const codeError = computed(() =>
    fieldText('code') || (attemptedReset.value && !code.value.trim() ? t('forgot.codeMissing') : ''),
  )
  const passwordError = computed(() => {
    if (fieldText('password') || fieldText('new_password')) {
      return fieldText('password') || fieldText('new_password')
    }
    if (!attemptedReset.value) return ''
    if (!password.value.trim()) return t('forgot.passwordMissing')
    const issue = checkPassword(password.value)
    return issue ? t(passwordIssueKey(issue)) : ''
  })

  watch([email, code, password], () => {
    serverField.value = null
    formError.value = ''
  })

  function applyFailure(body: ApiErrorBody | undefined) {
    if (body?.code === 'invalid_code') {
      serverField.value = { field: 'code', text: t('forgot.invalidCode') }
      return
    }
    const issue = firstFieldError(body)
    // 确认步骤没有邮箱输入框，邮箱类错误只能落到表单级并提示返回上一步
    if (issue?.field === 'email') {
      formError.value = t('forgot.emailInvalid')
      return
    }
    const key = issue ? fieldMessageKey(issue.code) : undefined
    if (issue && key) {
      serverField.value = { field: issue.field, text: t(key) }
      return
    }
    formError.value = t(apiMessageKey(body))
  }

  async function submitEmail(turnstileToken?: string) {
    attemptedEmail.value = true
    serverField.value = null
    formError.value = ''
    if (emailError.value) return
    pending.value = true
    try {
      const result = await requestResetCode(api, email.value.trim(), turnstileToken)
      if (result.kind === 'ok') step.value = 'reset'
      else applyFailure(result.body)
    } finally {
      pending.value = false
    }
  }

  async function submitReset() {
    attemptedReset.value = true
    serverField.value = null
    formError.value = ''
    if (codeError.value || passwordError.value) return
    pending.value = true
    try {
      const result = await confirmReset(api, email.value.trim(), code.value.trim(), password.value)
      if (result.kind === 'ok') completed.value = true
      else applyFailure(result.body)
    } finally {
      pending.value = false
    }
  }

  function backToEmail() {
    step.value = 'email'
    code.value = ''
    password.value = ''
    attemptedReset.value = false
    serverField.value = null
    formError.value = ''
  }

  return {
    email, code, password, step, pending, completed, formError,
    emailError, codeError, passwordError, submitEmail, submitReset, backToEmail,
  }
}
