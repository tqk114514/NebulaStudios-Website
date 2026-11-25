import { describe, expect, it } from 'vitest'
import { checkCredentials, isCompleteCode } from '~/composables/useSignIn/validation'
import { checkSignUp } from '~/composables/useSignUp/validation'
import { checkPassword } from '~/utils/password'

describe('checkCredentials', () => {
  it('标识为空时报标识缺项，不连带报密码', () => {
    expect(checkCredentials('', 'hunter2')).toBe('identifierMissing')
    expect(checkCredentials('  ', 'hunter2')).toBe('identifierMissing')
  })

  it('标识有内容而密码为空时报密码缺项', () => {
    expect(checkCredentials('a@example.com', '   ')).toBe('passwordMissing')
    expect(checkCredentials('nova', '   ')).toBe('passwordMissing')
  })

  it('两者都有内容时不在本地判凭据对错', () => {
    expect(checkCredentials('a@example.com', 'x')).toBe('none')
    expect(checkCredentials('nova', 'x')).toBe('none')
  })
})

describe('isCompleteCode', () => {
  it('六位数字才算完整', () => {
    expect(isCompleteCode('492107')).toBe(true)
  })

  it('位数不足或含非数字都不通过', () => {
    expect(isCompleteCode('49210')).toBe(false)
    expect(isCompleteCode('49a107')).toBe(false)
  })

  it('粘贴带入的首尾空白不影响判定', () => {
    expect(isCompleteCode('  492107 ')).toBe(true)
  })
})

describe('checkPassword', () => {
  it('合规口令通过', () => {
    expect(checkPassword('Nebula!Secure2026')).toBeNull()
  })

  it('长度边界与后端一致：16 位通过，15 位与 65 位不通过', () => {
    expect(checkPassword('Abcdefgh12345!xy')).toBeNull()
    expect(checkPassword('Abcdefgh12345!x')).toBe('passwordTooShort')
    expect(checkPassword(`${'Abcdefgh12345!xy'.repeat(4)}A`)).toBe('passwordTooLong')
  })

  it('缺少任一字符类别都报对应的首个问题', () => {
    expect(checkPassword('Nebula!SecureTwoSix')).toBe('passwordMissingDigit')
    expect(checkPassword('NEBULA!SECURE2026')).toBe('passwordMissingLowercase')
    expect(checkPassword('nebula!secure2026')).toBe('passwordMissingUppercase')
    expect(checkPassword('NebulaSecure2026X')).toBe('passwordMissingSpecial')
  })
})

describe('checkSignUp', () => {
  const valid = { email: 'a@example.com', username: 'san', password: 'Nebula!Secure2026' }

  it('三项齐全且口令合规时通过', () => {
    expect(checkSignUp(valid)).toBe('none')
  })

  it('任一项为空先报缺项', () => {
    expect(checkSignUp({ ...valid, password: '' })).toBe('fieldsRequired')
    expect(checkSignUp({ ...valid, username: '  ' })).toBe('fieldsRequired')
  })

  it('用户名短于三个字符或长于十五个字符时报长度问题', () => {
    expect(checkSignUp({ ...valid, username: 'ab' })).toBe('usernameTooShort')
    expect(checkSignUp({ ...valid, username: 'a'.repeat(16) })).toBe('usernameTooLong')
  })

  it('用户名含 @ 或 . 时报非法字符', () => {
    expect(checkSignUp({ ...valid, username: 'bad@name' })).toBe('usernameInvalidCharacters')
    expect(checkSignUp({ ...valid, username: 'bad.name' })).toBe('usernameInvalidCharacters')
  })

  it('口令不合规时报对应的口令问题', () => {
    expect(checkSignUp({ ...valid, password: 'hunter2' })).toBe('passwordTooShort')
    expect(checkSignUp({ ...valid, password: 'NEBULA!SECURE2026' })).toBe(
      'passwordMissingLowercase',
    )
  })

  it('同时有多个问题时只报第一个', () => {
    expect(checkSignUp({ ...valid, email: '', username: 'ab' })).toBe('fieldsRequired')
    expect(checkSignUp({ ...valid, username: 'ab', password: 'hunter2' })).toBe('usernameTooShort')
  })
})
