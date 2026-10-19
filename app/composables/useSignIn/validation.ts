/**
 * 登录表单的纯校验规则：邮箱先于密码判定，一次只报一个字段。
 */

export type CredentialIssue = 'none' | 'emailMissing' | 'passwordMissing'

export function checkCredentials(email: string, password: string): CredentialIssue {
  if (email.trim().length === 0) return 'emailMissing'
  if (password.trim().length === 0) return 'passwordMissing'
  return 'none'
}

/** 动态码固定 6 位数字；粘贴带来的空白字符先剥掉 */
export function isCompleteCode(value: string): boolean {
  return /^\d{6}$/.test(value.trim())
}
