import { computed, ref, watch } from 'vue'
import { useI18n } from '#imports'
import type { ApiErrorBody } from '@nebula/api-client'
import { useApi } from '~/composables/useApi'
import { apiMessageKey, firstFieldError } from '~/utils/api-message'
import { requestLogin, requestLoginTotp, type ApiUser } from './requests'
import { checkCredentials, isCompleteTotpInput } from './validation'

export type SignInStep = 'credentials' | 'totp'

/** 登录两步流程；二段由服务端 401 `totp_required` 的挑战令牌驱动。 */
export function useSignInState() {
  const { t } = useI18n()
  const api = useApi()
  const identifier = ref('')
  const password = ref('')
  const code = ref('')
  const step = ref<SignInStep>('credentials')
  const challengeToken = ref('')
  const attemptedCredentials = ref(false)
  const attemptedCode = ref(false)
  const pending = ref(false)
  const signedInUser = ref<ApiUser | null>(null)
  const formError = ref('')
  const serverField = ref<{ field: string; text: string } | null>(null)

  const credentialIssue = computed(() =>
    attemptedCredentials.value ? checkCredentials(identifier.value, password.value) : 'none',
  )
  const identifierError = computed(() => {
    if (serverField.value?.field === 'identifier') return serverField.value.text
    return credentialIssue.value === 'identifierMissing' ? t('login.identifierMissing') : ''
  })
  const passwordError = computed(() => {
    if (serverField.value?.field === 'password') return serverField.value.text
    return credentialIssue.value === 'passwordMissing' ? t('login.passwordMissing') : ''
  })
  const codeError = computed(() => {
    if (serverField.value?.field === 'code') return serverField.value.text
    return attemptedCode.value && !isCompleteTotpInput(code.value) ? t('totp.invalidCode') : ''
  })

  // 输入即清除上一次的服务端错误
  watch([identifier, password, code], () => {
    serverField.value = null
    formError.value = ''
  })

  function applyFailure(body: ApiErrorBody | undefined) {
    const issue = firstFieldError(body)
    if (issue?.field === 'identifier') {
      serverField.value = { field: 'identifier', text: t('login.identifierMissing') }
    } else if (issue?.field === 'password') {
      serverField.value = { field: 'password', text: t('login.passwordMissing') }
    } else {
      formError.value = t(apiMessageKey(body))
    }
  }

  function startTotpChallenge(token: string) {
    challengeToken.value = token
    code.value = ''
    attemptedCode.value = false
    serverField.value = null
    formError.value = ''
    step.value = 'totp'
  }

  async function submitCredentials() {
    attemptedCredentials.value = true
    serverField.value = null
    formError.value = ''
    if (credentialIssue.value !== 'none') return
    pending.value = true
    try {
      const outcome = await requestLogin(api, identifier.value.trim(), password.value)
      if (outcome.kind === 'totp') startTotpChallenge(outcome.challengeToken)
      else if (outcome.kind === 'ok') signedInUser.value = outcome.user
      else applyFailure(outcome.body)
    } finally {
      pending.value = false
    }
  }

  async function submitCode() {
    attemptedCode.value = true
    serverField.value = null
    formError.value = ''
    if (!isCompleteTotpInput(code.value) || !challengeToken.value) return
    pending.value = true
    try {
      // 恢复码字母表为大写；统一转大写不影响 6 位数字动态码
      const outcome = await requestLoginTotp(api, challengeToken.value, code.value.trim().toUpperCase())
      if (outcome.kind === 'ok') signedInUser.value = outcome.user
      else if (outcome.body?.code === 'totp_invalid') {
        serverField.value = { field: 'code', text: t('totp.invalidCode') }
      } else applyFailure(outcome.body)
    } finally {
      pending.value = false
    }
  }

  function backToCredentials() {
    attemptedCode.value = false
    step.value = 'credentials'
    challengeToken.value = ''
    code.value = ''
    password.value = ''
    serverField.value = null
    formError.value = ''
  }

  return {
    identifier, password, code, step, challengeToken, pending, signedInUser,
    formError, identifierError, passwordError, codeError, submitCredentials,
    submitCode, backToCredentials, startTotpChallenge,
  }
}
