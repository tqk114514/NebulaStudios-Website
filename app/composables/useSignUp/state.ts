import { computed, ref, watch } from 'vue'
import { useI18n } from '#imports'
import type { ApiErrorBody } from '@nebula/api-client'
import { useApi } from '~/composables/useApi'
import { apiMessageKey } from '~/utils/api-message'
import { signUpFailure, signUpIssueKey } from './errors'
import { requestRegister, requestResend, requestVerify } from './requests'
import { checkSignUp, type SignUpIssue } from './validation'

export type SignUpStep = 'register' | 'verify'
/** 注册两步：建号发码 → 邮箱验证码确认；占用与白名单只由服务端判定。 */
export function useSignUpState() {
  const { t } = useI18n()
  const api = useApi()
  const email = ref('')
  const username = ref('')
  const password = ref('')
  const code = ref('')
  const step = ref<SignUpStep>('register')
  const attempted = ref(false)
  const attemptedCode = ref(false)
  const pending = ref(false)
  const verified = ref(false)
  const notice = ref('')
  const formError = ref('')
  const serverField = ref<{ field: string; text: string } | null>(null)
  const issue = computed<SignUpIssue>(() =>
    attempted.value
      ? checkSignUp({ email: email.value, username: username.value, password: password.value })
      : 'none',
  )
  const issueText = computed(() => {
    const key = signUpIssueKey(issue.value)
    return key ? t(key) : ''
  })
  const formErrorText = computed(() =>
    formError.value || (issue.value === 'fieldsRequired' ? issueText.value : ''),
  )
  const fieldText = (field: string) =>
    serverField.value?.field === field ? serverField.value.text : ''
  const emailError = computed(() => fieldText('email'))
  const usernameError = computed(() =>
    fieldText('username')
    || (issue.value.startsWith('username') ? issueText.value : ''),
  )
  const passwordError = computed(() =>
    fieldText('password') || (issue.value.startsWith('password') ? issueText.value : ''),
  )
  const codeError = computed(() =>
    fieldText('code') || (attemptedCode.value && !code.value.trim() ? t('register.codeMissing') : ''),
  )
  watch([email, username, password, code], () => {
    serverField.value = null
    formError.value = ''
  })
  function applyRegisterFailure(body: ApiErrorBody | undefined) {
    const failure = signUpFailure(body)
    if ('field' in failure) serverField.value = { field: failure.field, text: t(failure.key) }
    else formError.value = t(failure.messageKey)
  }
  async function submit(turnstileToken?: string) {
    attempted.value = true
    serverField.value = null
    formError.value = ''
    if (issue.value !== 'none') return
    pending.value = true
    try {
      const result = await requestRegister(api, {
        email: email.value.trim(),
        username: username.value.trim(),
        password: password.value,
      }, turnstileToken)
      if (result.kind === 'ok') { step.value = 'verify'; notice.value = t('register.codeSent') }
      else applyRegisterFailure(result.body)
    } finally {
      pending.value = false
    }
  }
  async function verifyCode() {
    attemptedCode.value = true
    serverField.value = null
    formError.value = ''
    if (!code.value.trim()) return
    pending.value = true
    try {
      const result = await requestVerify(api, email.value.trim(), code.value.trim())
      if (result.kind === 'ok') { verified.value = true; notice.value = '' }
      else if (result.body?.code === 'invalid_code') {
        serverField.value = { field: 'code', text: t('register.invalidCode') }
      } else formError.value = t(apiMessageKey(result.body))
    } finally {
      pending.value = false
    }
  }
  async function resend(turnstileToken?: string) {
    serverField.value = null
    formError.value = ''
    pending.value = true
    try {
      const result = await requestResend(api, email.value.trim(), turnstileToken)
      if (result.kind === 'ok') notice.value = t('register.codeSent')
      else formError.value = t(apiMessageKey(result.body))
    } finally {
      pending.value = false
    }
  }
  function backToRegister() {
    step.value = 'register'
    code.value = ''
    attemptedCode.value = false
    notice.value = ''
    serverField.value = null
    formError.value = ''
  }
  return {
    email, username, password, code, step, pending, verified, notice, formErrorText,
    emailError, usernameError, passwordError, codeError, submit, verifyCode, resend, backToRegister,
  }
}
