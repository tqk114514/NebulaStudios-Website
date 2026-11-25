/**
 * 后端 `details.fields[].code` 到字段文案键的映射。
 * 只登记已经在本地表单出现的字段；未知 code 由调用方落到通用失败文案。
 */
const FIELD_MESSAGE_KEYS: Record<string, string> = {
  invalid_email: 'register.emailInvalid',
  email_domain_not_allowed: 'register.emailDomainNotAllowed',
  username_too_short: 'register.usernameTooShort',
  username_too_long: 'register.usernameTooLong',
  username_invalid_characters: 'register.usernameInvalidCharacters',
  password_too_short: 'password.tooShort',
  password_too_long: 'password.tooLong',
  password_missing_digit: 'password.missingDigit',
  password_missing_lowercase: 'password.missingLowercase',
  password_missing_uppercase: 'password.missingUppercase',
  password_missing_special: 'password.missingSpecial',
}

/** 返回静态 i18n 键；字段名由调用方决定错误落在哪个输入上。 */
export function fieldMessageKey(code: string): string | undefined {
  return FIELD_MESSAGE_KEYS[code]
}
