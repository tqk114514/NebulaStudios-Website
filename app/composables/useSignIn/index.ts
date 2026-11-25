import { computed, ref } from 'vue'
import { useI18n } from '#imports'
import { checkCredentials, isCompleteCode } from './validation'

export type SignInStep = 'credentials' | 'totp'

/**
 * 登录两步流程的表单状态：口令步骤与动态码步骤，错误只在提交后出现。
 * 二段由服务端 401 `totp_required` 的挑战令牌驱动（ADR-0004、ADR-0016）。
 */
export function useSignIn() {
  const { t } = useI18n()

  const identifier = ref('')
  const password = ref('')
  const code = ref('')
  const step = ref<SignInStep>('credentials')
  const challengeToken = ref('')
  const attemptedCredentials = ref(false)
  const attemptedCode = ref(false)

  const credentialIssue = computed(() =>
    attemptedCredentials.value ? checkCredentials(identifier.value, password.value) : 'none',
  )

  const identifierError = computed(() =>
    credentialIssue.value === 'identifierMissing' ? t('login.identifierMissing') : '',
  )
  const passwordError = computed(() =>
    credentialIssue.value === 'passwordMissing' ? t('login.passwordMissing') : '',
  )
  const codeError = computed(() =>
    attemptedCode.value && !isCompleteCode(code.value) ? t('totp.invalidCode') : '',
  )

  /** 由登录响应或社交回调进入二段；挑战令牌只在此后提交。 */
  function startTotpChallenge(token: string) {
    challengeToken.value = token
    code.value = ''
    attemptedCode.value = false
    step.value = 'totp'
  }

  function submitCredentials() {
    attemptedCredentials.value = true
    if (credentialIssue.value !== 'none') return
    attemptedCredentials.value = false
    // 网络接入前仅演示二段界面：真实流程由登录响应决定是否下发挑战令牌
    step.value = 'totp'
  }

  function submitCode() {
    attemptedCode.value = true
    if (!isCompleteCode(code.value)) return
    // 接入后提交 { challenge_token: challengeToken.value, code: code.value }
  }

  function backToCredentials() {
    attemptedCode.value = false
    step.value = 'credentials'
    challengeToken.value = ''
    code.value = ''
    password.value = ''
  }

  return {
    identifier,
    password,
    code,
    step,
    challengeToken,
    identifierError,
    passwordError,
    codeError,
    submitCredentials,
    submitCode,
    backToCredentials,
    startTotpChallenge,
  }
}
