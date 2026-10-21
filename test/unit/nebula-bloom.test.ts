import { describe, expect, it } from 'vitest'
import { BLOOM_KERNEL_SIZES, gaussianKernel, mipSize } from '~/composables/useNebulaField/gl/bloom-math'

describe('gaussianKernel', () => {
  it('半径 6 给出三对合并抽样，中心权重为 0.39894/σ', () => {
    const kernel = gaussianKernel(6)

    expect(kernel.centerWeight).toBeCloseTo(0.19947, 5)
    expect(kernel.offsets).toHaveLength(3)
    expect(kernel.weights[0]).toBeCloseTo(0.29702, 4)
    expect(kernel.offsets[0]).toBeCloseTo(1.4073, 3)
    expect(kernel.offsets[2]).toBe(5)
  })

  it('能量近似守恒：中心 + 2×权重和接近 1', () => {
    for (const radius of BLOOM_KERNEL_SIZES) {
      const { centerWeight, weights } = gaussianKernel(radius)
      const energy = centerWeight + 2 * weights.reduce((a, b) => a + b, 0)

      expect(energy).toBeGreaterThan(0.99)
      expect(energy).toBeLessThan(1.01)
    }
  })

  it('偏移随序号递增、权重递减，卷积核不会左右乱序', () => {
    const { offsets, weights } = gaussianKernel(22)

    expect(offsets).toEqual([...offsets].sort((a, b) => a - b))
    expect(weights).toEqual([...weights].sort((a, b) => b - a))
  })
})

describe('mipSize', () => {
  it('1920×1080 逐级对半，奇数边长向上取整', () => {
    expect(mipSize(1920, 1080, 0)).toEqual({ width: 1920, height: 1080 })
    expect(mipSize(1920, 1080, 1)).toEqual({ width: 960, height: 540 })
    expect(mipSize(1920, 1080, 3)).toEqual({ width: 240, height: 135 })
    expect(mipSize(1920, 1080, 4)).toEqual({ width: 120, height: 68 })
    expect(mipSize(1920, 1080, 5)).toEqual({ width: 60, height: 34 })
  })

  it('最小尺寸不会塌到 0', () => {
    expect(mipSize(2, 2, 5)).toEqual({ width: 1, height: 1 })
    expect(mipSize(1001, 1, 3)).toEqual({ width: 126, height: 1 })
  })
})
