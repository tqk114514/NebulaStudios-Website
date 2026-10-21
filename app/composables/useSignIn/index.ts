import { computed, ref } from 'vue'
import { useI18n } from '#imports'
import { checkCredentials, isCompleteCode } from './validation'

export type SignInStep = 'credentials' | 'totp'

/**
 * 登录两步流程：口令步骤通过后进入动态码步骤。
 * 首次提交前不报错，之后错误随输入更新。
 */
export function useSignIn() {
  const { t } = useI18n()

  const email = ref('')
  const password = ref('')
  const code = ref('')
  const step = ref<SignInStep>('credentials')
  const attemptedCredentials = ref(false)
  const attemptedCode = ref(false)

  const credentialIssue = computed(() =>
    attemptedCredentials.value ? checkCredentials(email.value, password.value) : 'none',
  )

  const emailError = computed(() =>
    credentialIssue.value === 'emailMissing' ? t('login.emailMissing') : '',
  )
  const passwordError = computed(() =>
    credentialIssue.value === 'passwordMissing' ? t('login.passwordMissing') : '',
  )
  const codeError = computed(() =>
    attemptedCode.value && !isCompleteCode(code.value) ? t('totp.invalidCode') : '',
  )

  function submitCredentials() {
    attemptedCredentials.value = true
    if (checkCredentials(email.value, password.value) !== 'none') return
    // 通过后清空标记：返回口令步骤时密码已被清掉，不该带出一条错误
    attemptedCredentials.value = false
    step.value = 'totp'
  }

  function submitCode() {
    attemptedCode.value = true
    if (!isCompleteCode(code.value)) return
    // 后端接入后在此发起 /api/auth/login/totp
  }

  function backToCredentials() {
    attemptedCode.value = false
    step.value = 'credentials'
    code.value = ''
    password.value = ''
  }

  return {
    email,
    password,
    code,
    step,
    emailError,
    passwordError,
    codeError,
    submitCredentials,
    submitCode,
    backToCredentials,
  }
}
