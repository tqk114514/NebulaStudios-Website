import type { ApiErrorBody } from '@nebula/api-client'
import { apiMessageKey, firstFieldError } from '~/utils/api-message'
import { fieldMessageKey } from '~/utils/field-message'
import { passwordIssueKey, type PasswordIssue } from '~/utils/password'
import type { SignUpIssue } from './validation'

function isPasswordIssue(issue: SignUpIssue): issue is PasswordIssue {
  return issue.startsWith('password')
}

/** 本地校验 issue 到文案键；`none` 返回空串，调用方据此不渲染错误。 */
export function signUpIssueKey(issue: SignUpIssue): string {
  if (isPasswordIssue(issue)) return passwordIssueKey(issue)
  switch (issue) {
    case 'fieldsRequired': return 'register.fieldsRequired'
    case 'usernameTooShort': return 'register.usernameTooShort'
    case 'usernameTooLong': return 'register.usernameTooLong'
    case 'usernameInvalidCharacters': return 'register.usernameInvalidCharacters'
    default: return ''
  }
}

export type SignUpFailure = { field: string; key: string } | { messageKey: string }

/** 注册失败落到具体字段，或退到表单级通用文案。 */
export function signUpFailure(body: ApiErrorBody | undefined): SignUpFailure {
  if (body?.code === 'email_taken') return { field: 'email', key: 'api.emailTaken' }
  if (body?.code === 'username_taken') return { field: 'username', key: 'api.usernameTaken' }
  const issue = firstFieldError(body)
  const key = issue ? fieldMessageKey(issue.code) : undefined
  if (issue && key) return { field: issue.field, key }
  return { messageKey: apiMessageKey(body) }
}
