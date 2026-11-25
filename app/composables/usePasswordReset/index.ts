import { computed, ref } from 'vue'
import { useI18n } from '#imports'
import { checkPassword, passwordIssueKey } from '~/utils/password'

export type PasswordResetStep = 'email' | 'reset'

/**
 * 忘记密码的两步表单状态：邮箱发码 → 验证码 + 新密码。
 * 首次提交前不报错，之后错误随输入更新；后端接入前的提交只切换步骤。
 */
export function usePasswordReset() {
  const { t } = useI18n()

  const email = ref('')
  const code = ref('')
  const password = ref('')
  const step = ref<PasswordResetStep>('email')
  const attemptedEmail = ref(false)
  const attemptedReset = ref(false)

  const emailError = computed(() =>
    attemptedEmail.value && !email.value.trim() ? t('forgot.emailMissing') : '',
  )
  const codeError = computed(() =>
    attemptedReset.value && !code.value.trim() ? t('forgot.codeMissing') : '',
  )
  const passwordError = computed(() => {
    if (!attemptedReset.value) return ''
    if (!password.value.trim()) return t('forgot.passwordMissing')
    const issue = checkPassword(password.value)
    return issue ? t(passwordIssueKey(issue)) : ''
  })

  function submitEmail() {
    attemptedEmail.value = true
    if (emailError.value) return
    // 后端接入后在此发起 /api/auth/password/reset/request；
    // 该端点对未注册邮箱与冷却中同样返回 204，界面不得据此判断账户是否存在
    step.value = 'reset'
  }

  function submitReset() {
    attemptedReset.value = true
    if (codeError.value || passwordError.value) return
    // 后端接入后在此发起 /api/auth/password/reset/confirm
    // 请求体为 { email, code, new_password }，成功后清空会话并回登录页
  }

  function backToEmail() {
    step.value = 'email'
    code.value = ''
    password.value = ''
    attemptedReset.value = false
  }

  return {
    email,
    code,
    password,
    step,
    emailError,
    codeError,
    passwordError,
    submitEmail,
    submitReset,
    backToEmail,
  }
}
