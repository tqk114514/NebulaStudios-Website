import { describe, expect, it } from 'vitest'
import { INTRO_DURATION, INTRO_STAGGER, INTRO_TRAVEL } from '~/composables/useNebulaField/constants'
import {
  createScanClock,
  introProgress,
  landingFlash,
  particleProgress,
  spinScale,
} from '~/composables/useNebulaField/intro'

describe('introProgress', () => {
  it('按时长折算成 0..1', () => {
    expect(introProgress(0)).toBe(0)
    expect(introProgress(INTRO_DURATION / 2)).toBe(0.5)
    expect(introProgress(INTRO_DURATION)).toBe(1)
  })

  it('越界的时间不产生越界进度', () => {
    expect(introProgress(-0.4)).toBe(0)
    expect(introProgress(INTRO_DURATION * 3)).toBe(1)
  })
})

describe('spinScale', () => {
  it('首帧压到 0.35，入场结束恢复满速', () => {
    expect(spinScale(0)).toBe(0.35)
    expect(spinScale(1)).toBe(1)
  })

  it('起步慢于线性：四分之一处仍接近下限', () => {
    // 线性到 0.25 应为 0.35 + 0.65 × 0.25 = 0.5125
    expect(spinScale(0.25)).toBeLessThan(0.5125)
    expect(spinScale(0.25)).toBeGreaterThan(0.35)
  })

  it('全程单调不减，自旋不会倒转', () => {
    const samples = Array.from({ length: 21 }, (_, i) => spinScale(i / 20))
    const ascending = samples.every((value, i) => i === 0 || value >= (samples[i - 1] ?? 0))
    expect(ascending).toBe(true)
  })
})

describe('landingFlash', () => {
  it('窗口宽 0.4，峰值在 q=1', () => {
    expect(landingFlash(0.8)).toBeCloseTo(0)
    expect(landingFlash(0.9)).toBeCloseTo(0.5)
    expect(landingFlash(1)).toBe(1)
    expect(landingFlash(1.2)).toBeCloseTo(0)
  })

  it('巡航态（intro=1）下整个错峰区间的闪光都已归零', () => {
    for (let i = 0; i <= 200; i++) {
      const delay = (i / 200) * INTRO_STAGGER
      expect(landingFlash(particleProgress(1, delay))).toBe(0)
    }
  })

  it('最后一批粒子在 intro 触顶前完成落地闪光', () => {
    expect(landingFlash(particleProgress(INTRO_STAGGER + INTRO_TRAVEL, INTRO_STAGGER))).toBeCloseTo(1)
    expect(INTRO_STAGGER + INTRO_TRAVEL * 1.2).toBeLessThan(1)
  })
})

describe('createScanClock', () => {
  it('组装完成前停在起点，光扫不会提前开场', () => {
    const clock = createScanClock()
    expect(clock(0.016, 0)).toBe(0)
    expect(clock(0.016, 0.999)).toBe(0)
  })

  it('intro 到 1 后按帧间隔累加', () => {
    const clock = createScanClock()
    clock(0.016, 0)
    expect(clock(0.02, 1)).toBeCloseTo(0.02)
    expect(clock(0.03, 1)).toBeCloseTo(0.05)
  })

  it('切后台回来的大间隔被夹住，恢复时不跳相位', () => {
    const clock = createScanClock()
    expect(clock(5, 1)).toBe(0.05)
  })
})
