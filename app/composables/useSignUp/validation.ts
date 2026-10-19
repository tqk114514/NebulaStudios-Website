/**
 * 注册表单的纯校验规则。每个取值对应一条同名的 `register.<issue>` 文案。
 */

const USERNAME_MIN_LENGTH = 3

export type SignUpIssue = 'none' | 'fieldsRequired' | 'usernameTooShort' | 'passwordMismatch'

export interface SignUpDraft {
  email: string
  username: string
  password: string
  confirm: string
}

/** 一次只报第一个问题：同时报三条错误只会让人先读错的那条 */
export function checkSignUp(draft: SignUpDraft): SignUpIssue {
  const { email, username, password, confirm } = draft
  if (!email.trim() || !username.trim() || !password.trim() || !confirm.trim()) return 'fieldsRequired'
  if (draft.username.trim().length < USERNAME_MIN_LENGTH) return 'usernameTooShort'
  if (draft.password !== draft.confirm) return 'passwordMismatch'
  return 'none'
}
