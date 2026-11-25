/**
 * 注册表单的纯校验规则。每个取值对应一条文案：用户名问题用
 * `register.<issue>`，口令问题用 `password.<issue>`（与后端字段码同一来源）。
 */

import { checkPassword, type PasswordIssue } from '~/utils/password'

const USERNAME_MIN_LENGTH = 3
const USERNAME_MAX_LENGTH = 15
const USERNAME_PATTERN = /^[A-Za-z0-9_-]+$/

export type SignUpIssue =
  | 'none'
  | 'fieldsRequired'
  | 'usernameTooShort'
  | 'usernameTooLong'
  | 'usernameInvalidCharacters'
  | PasswordIssue

export interface SignUpDraft {
  email: string
  username: string
  password: string
}

/** 一次只报第一个问题：同时报三条错误只会让人先读错的那条 */
export function checkSignUp(draft: SignUpDraft): SignUpIssue {
  if (!draft.email.trim() || !draft.username.trim() || !draft.password.trim()) {
    return 'fieldsRequired'
  }
  const username = draft.username.trim()
  const usernameLength = [...username].length
  if (usernameLength < USERNAME_MIN_LENGTH) return 'usernameTooShort'
  if (usernameLength > USERNAME_MAX_LENGTH) return 'usernameTooLong'
  if (!USERNAME_PATTERN.test(username)) return 'usernameInvalidCharacters'
  return checkPassword(draft.password) ?? 'none'
}
