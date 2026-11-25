import { computed, ref } from 'vue'
import { useI18n } from '#imports'
import { passwordIssueKey, type PasswordIssue } from '~/utils/password'
import { checkSignUp, type SignUpIssue } from './validation'

/** 口令问题与用户名问题共用一套 issue 联合类型，靠前缀分流文案来源。 */
function isPasswordIssue(issue: SignUpIssue): issue is PasswordIssue {
  return issue.startsWith('password')
}

/**
 * 注册表单的字段状态与本地校验。首次提交前不报错，之后错误随输入更新。
 * 用户名占用、邮箱域名白名单只有服务端能判定，本地不猜。
 */
export function useSignUp() {
  const { t } = useI18n()

  const email = ref('')
  const username = ref('')
  const password = ref('')
  const attempted = ref(false)

  const issue = computed<SignUpIssue>(() =>
    attempted.value
      ? checkSignUp({
        email: email.value,
        username: username.value,
        password: password.value,
      })
      : 'none',
  )

  // 键写字面量：§3 第 4 条的缺项检查只扫得到静态键
  const issueText = computed(() => {
    if (isPasswordIssue(issue.value)) return t(passwordIssueKey(issue.value))
    switch (issue.value) {
      case 'fieldsRequired':
        return t('register.fieldsRequired')
      case 'usernameTooShort':
        return t('register.usernameTooShort')
      case 'usernameTooLong':
        return t('register.usernameTooLong')
      case 'usernameInvalidCharacters':
        return t('register.usernameInvalidCharacters')
      default:
        return ''
    }
  })

  // 缺项无法归到某一个字段，由表单级错误行承担
  const formError = computed(() => (issue.value === 'fieldsRequired' ? issueText.value : ''))
  const usernameError = computed(() =>
    issue.value === 'usernameTooShort'
    || issue.value === 'usernameTooLong'
    || issue.value === 'usernameInvalidCharacters'
      ? issueText.value
      : '',
  )
  const passwordError = computed(() =>
    isPasswordIssue(issue.value) ? issueText.value : '',
  )

  function submit() {
    attempted.value = true
    if (issue.value !== 'none') return
    // 后端接入后在此发起 /api/auth/register
  }

  return {
    email,
    username,
    password,
    formError,
    usernameError,
    passwordError,
    submit,
  }
}
