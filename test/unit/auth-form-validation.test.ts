import { describe, expect, it } from 'vitest'
import { checkCredentials, isCompleteCode } from '~/composables/useSignIn/validation'
import { checkSignUp } from '~/composables/useSignUp/validation'

describe('checkCredentials', () => {
  it('邮箱为空时报邮箱缺项，不连带报密码', () => {
    expect(checkCredentials('', 'hunter2')).toBe('emailMissing')
    expect(checkCredentials('  ', 'hunter2')).toBe('emailMissing')
  })

  it('邮箱有内容而密码为空时报密码缺项', () => {
    expect(checkCredentials('a@example.com', '   ')).toBe('passwordMissing')
  })

  it('两者都有内容时不在本地判密码对错', () => {
    expect(checkCredentials('a@example.com', 'x')).toBe('none')
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

describe('checkSignUp', () => {
  const valid = { email: 'a@example.com', username: 'san', password: 'hunter2', confirm: 'hunter2' }

  it('四项齐全且两次密码一致时通过', () => {
    expect(checkSignUp(valid)).toBe('none')
  })

  it('任一项为空先报缺项', () => {
    expect(checkSignUp({ ...valid, confirm: '' })).toBe('fieldsRequired')
    expect(checkSignUp({ ...valid, username: '  ' })).toBe('fieldsRequired')
  })

  it('用户名短于三个字符时报长度不足', () => {
    expect(checkSignUp({ ...valid, username: 'ab' })).toBe('usernameTooShort')
  })

  it('两次密码不一致时报不一致', () => {
    expect(checkSignUp({ ...valid, confirm: 'hunter3' })).toBe('passwordMismatch')
  })

  it('同时有多个问题时只报第一个：缺项优先于密码不一致', () => {
    expect(checkSignUp({ ...valid, email: '', confirm: 'hunter3' })).toBe('fieldsRequired')
  })
})
