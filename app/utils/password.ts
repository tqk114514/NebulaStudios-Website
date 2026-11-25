/**
 * 口令强度规则，与后端 `password_problem` 同一判定（ADR-0002、ADR-0016）：
 * 16–64 个字符，且含 ASCII 大写、小写、数字与标点。前端只提前提示，
 * 最终判定仍以服务端为准。
 */

export type PasswordIssue =
  | 'passwordTooShort'
  | 'passwordTooLong'
  | 'passwordMissingDigit'
  | 'passwordMissingLowercase'
  | 'passwordMissingUppercase'
  | 'passwordMissingSpecial'

const PASSWORD_MIN_LENGTH = 16
const PASSWORD_MAX_LENGTH = 64
const ASCII_PUNCTUATION = /[\u0021-\u002f\u003a-\u0040\u005b-\u0060\u007b-\u007e]/

/** 返回第一个不满足项；合规返回 `null`。 */
export function checkPassword(password: string): PasswordIssue | null {
  const length = [...password].length
  if (length < PASSWORD_MIN_LENGTH) return 'passwordTooShort'
  if (length > PASSWORD_MAX_LENGTH) return 'passwordTooLong'
  if (!/\d/.test(password)) return 'passwordMissingDigit'
  if (!/[a-z]/.test(password)) return 'passwordMissingLowercase'
  if (!/[A-Z]/.test(password)) return 'passwordMissingUppercase'
  if (!ASCII_PUNCTUATION.test(password)) return 'passwordMissingSpecial'
  return null
}

const PASSWORD_ISSUE_KEYS: Record<PasswordIssue, string> = {
  passwordTooShort: 'password.tooShort',
  passwordTooLong: 'password.tooLong',
  passwordMissingDigit: 'password.missingDigit',
  passwordMissingLowercase: 'password.missingLowercase',
  passwordMissingUppercase: 'password.missingUppercase',
  passwordMissingSpecial: 'password.missingSpecial',
}

/** 口令问题对应的静态 i18n 键；`check:i18n` 按点号字面量收集引用。 */
export function passwordIssueKey(issue: PasswordIssue): string {
  return PASSWORD_ISSUE_KEYS[issue]
}
