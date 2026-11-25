/**
 * 登录表单的纯校验规则：标识先于密码判定，一次只报一个字段。
 * 标识为邮箱或用户名；服务端只对含 `@` 的标识做邮箱归一化（ADR-0015）。
 */

export type CredentialIssue = 'none' | 'identifierMissing' | 'passwordMissing'

export function checkCredentials(identifier: string, password: string): CredentialIssue {
  if (identifier.trim().length === 0) return 'identifierMissing'
  if (password.trim().length === 0) return 'passwordMissing'
  return 'none'
}

/** 动态码固定 6 位数字；粘贴带来的空白字符先剥掉 */
export function isCompleteCode(value: string): boolean {
  return /^\d{6}$/.test(value.trim())
}
