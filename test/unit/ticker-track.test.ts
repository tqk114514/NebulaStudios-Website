import { describe, expect, it } from 'vitest'
import {
  requiredItemCount,
  tickerDuration,
  TICKER_COVERAGE,
  TICKER_MIN_ITEMS,
  TICKER_SPEED,
} from '~/composables/useTicker/track'

describe('requiredItemCount', () => {
  it('按两轨覆盖所需的条目数向上取整', () => {
    // 1440 视口需要 2880px 覆盖，单条 180px → 16 条
    expect(requiredItemCount(1440, 180)).toBe(16)
  })

  it('窄视口也保有下限条数，轨道不会只剩半截字', () => {
    expect(requiredItemCount(320, 180)).toBe(TICKER_MIN_ITEMS)
  })

  it('字体度量未就绪时退回下限，交由下一次测量修正', () => {
    expect(requiredItemCount(1440, 0)).toBe(TICKER_MIN_ITEMS)
  })

  it('覆盖不变量：轨道总宽恒 ≥ 视口 × 系数', () => {
    for (const viewport of [320, 768, 1440, 2560, 3840]) {
      for (const item of [90, 175.4, 260]) {
        const count = requiredItemCount(viewport, item)
        expect(count * item).toBeGreaterThanOrEqual(viewport * TICKER_COVERAGE - 1e-9)
      }
    }
  })
})

describe('tickerDuration', () => {
  it('时长 = 轨道宽 ÷ 屏速，位移一个轨道宽恰好走完一个周期', () => {
    expect(tickerDuration(1800)).toBe(`${(1800 / TICKER_SPEED).toFixed(2)}s`)
    expect(tickerDuration(1800)).toBe('50.00s')
  })

  it('宽度未测出时不给时长，交给 CSS 回退值', () => {
    expect(tickerDuration(0)).toBe('')
  })

  it('屏速与轨道宽度无关：加条目只加时长，不加快移动', () => {
    const item = 180
    const short = Number.parseFloat(tickerDuration(item * 10))
    const long = Number.parseFloat(tickerDuration(item * 40))
    expect(item * 10 / short).toBeCloseTo(item * 40 / long)
  })
})
