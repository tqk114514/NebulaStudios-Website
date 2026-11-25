import { describe, expect, it } from 'vitest'
import { parseSocialCallback, socialErrorKey } from '~/composables/useSignIn/social'

describe('parseSocialCallback', () => {
  it('非社交回调返回 null', () => {
    expect(parseSocialCallback({})).toBeNull()
    expect(parseSocialCallback({ social: 'unknown' })).toBeNull()
  })

  it('social=ok 表示会话已由回调下发', () => {
    expect(parseSocialCallback({ social: 'ok' })).toEqual({ kind: 'ok' })
  })

  it('totp_required 带挑战令牌时进入二段', () => {
    expect(parseSocialCallback({ social: 'totp_required', challenge_token: 'abc' })).toEqual({
      kind: 'totp',
      challengeToken: 'abc',
    })
  })

  it('totp_required 缺挑战令牌时按无效状态处理', () => {
    expect(parseSocialCallback({ social: 'totp_required' })).toEqual({
      kind: 'error',
      slug: 'invalid_state',
    })
    expect(parseSocialCallback({ social: 'totp_required', challenge_token: '   ' })).toEqual({
      kind: 'error',
      slug: 'invalid_state',
    })
  })

  it('social_error 优先于 social，且支持数组形式的查询值', () => {
    expect(parseSocialCallback({ social: 'ok', social_error: 'not_linked' })).toEqual({
      kind: 'error',
      slug: 'not_linked',
    })
    expect(parseSocialCallback({ social_error: ['expired', 'other'] })).toEqual({
      kind: 'error',
      slug: 'expired',
    })
  })
})

describe('socialErrorKey', () => {
  it('已知 slug 映射到对应文案键', () => {
    expect(socialErrorKey('not_linked')).toBe('login.socialErrorNotLinked')
    expect(socialErrorKey('provider_disabled')).toBe('login.socialErrorProviderDisabled')
  })

  it('未知 slug 落到通用失败文案', () => {
    expect(socialErrorKey('something_else')).toBe('login.socialErrorGeneric')
  })
})
