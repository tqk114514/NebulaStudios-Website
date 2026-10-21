import { describe, expect, it } from 'vitest'
import { hexToLinear, srgbToLinear } from '~/composables/useNebulaField/gl/color'
import { modelView, perspective } from '~/composables/useNebulaField/gl/matrix'

/** 索引访问在 noUncheckedIndexedAccess 下是可选值，比较前显式取数 */
function at(matrix: ArrayLike<number>, index: number): number {
  const value = matrix[index]
  if (value === undefined) throw new Error(`矩阵缺少第 ${index} 项`)
  return value
}

describe('perspective', () => {
  it('45° 竖直视场、宽高比 1.6 时给出 f=2.4142 的对称透视矩阵', () => {
    const m = perspective(45, 1.6, 0.1, 40)

    expect(m[0]).toBeCloseTo(1.50888, 4)
    expect(m[5]).toBeCloseTo(2.41421, 4)
    expect(m[10]).toBeCloseTo(-1.00501, 4)
    expect(m[11]).toBe(-1)
    expect(m[14]).toBeCloseTo(-0.2005, 4)
  })

  it('z=0 平面上的世界高度除以视锥半高即 NDC y，与 measureViewport 同一算法', () => {
    // 半高 = tan(22.5°) × 7 = 2.8995，layout.ts 用的是同一个值
    const m = perspective(45, 1.6, 0.1, 7)
    const mv = modelView({ x: 0, y: 3, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1 }, 7)
    const clipY = at(m, 5) * at(mv, 13)
    const clipW = -at(mv, 14)

    expect(clipY / clipW).toBeCloseTo(3 / 2.8995, 3)
  })
})

describe('modelView', () => {
  it('三轴缩放不进平移项：平移落在第四列且不被放大', () => {
    const m = modelView({ x: 1, y: 2, z: -1.5, scaleX: 1.5, scaleY: 0.5, scaleZ: 4.5 }, 7)

    expect([...m]).toEqual([
      1.5, 0, 0, 0, 0, 0.5, 0, 0, 0, 0, 4.5, 0, 1, 2, -8.5, 1,
    ])
  })
})

describe('色彩转换', () => {
  it('线性区间端点与拐点', () => {
    expect(srgbToLinear(0)).toBe(0)
    expect(srgbToLinear(1)).toBeCloseTo(1, 6)
    expect(srgbToLinear(0.5)).toBeCloseTo(0.21404, 5)
    expect(srgbToLinear(0.04045)).toBeCloseTo(0.04045 / 12.92, 8)
  })

  it('三位十六进制展开为六位后按通道转线性', () => {
    expect(hexToLinear('#fff')).toEqual(hexToLinear('#ffffff'))
    const [r, g, b] = hexToLinear('#f0ede8')

    expect(r).toBeCloseTo(0.87137, 4)
    expect(g).toBeCloseTo(0.84688, 4)
    expect(b).toBeCloseTo(0.80697, 4)
  })

  it('空白串按 fallback 取值，不抛错', () => {
    expect(Array.from(hexToLinear('  #000  '))).toEqual([0, 0, 0])
  })
})
