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

/** 二段接受 6 位 TOTP 动态码或 10 位恢复码；粘贴带来的空白字符先剥掉 */
export function isCompleteTotpInput(value: string): boolean {
  const code = value.trim()
  return /^\d{6}$/.test(code) || /^[A-Za-z0-9]{10}$/.test(code)
}
