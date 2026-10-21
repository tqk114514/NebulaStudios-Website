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

// 差分对照：本节以 three 为基准验证自持实现，移除 three 依赖时一并删除
describe('与 three 的结果一致', () => {
  it('投影矩阵逐元素相同（本项目取 Float32，允许 f32 量化差）', async () => {
    const { PerspectiveCamera } = await import('three')
    const camera = new PerspectiveCamera(45, 1.6, 0.1, 40)
    camera.updateProjectionMatrix()

    perspective(45, 1.6, 0.1, 40).forEach((value, i) => {
      expect(value).toBeCloseTo(at(camera.projectionMatrix.elements, i), 6)
    })
  })

  it('modelViewMatrix 与 Object3D + 相机的合成结果相同', async () => {
    const { Matrix4, Object3D, PerspectiveCamera } = await import('three')
    const camera = new PerspectiveCamera(45, 1.6, 0.1, 40)
    camera.position.set(0, 0, 7)
    camera.updateMatrixWorld()
    const object = new Object3D()
    object.position.set(1, 2, -1.5)
    object.scale.set(1.5, 0.5, 4.5)
    object.updateMatrixWorld()

    const expected = new Matrix4().multiplyMatrices(
      camera.matrixWorldInverse,
      object.matrixWorld,
    )

    expect([...expected.elements]).toEqual(
      Array.from(modelView({ x: 1, y: 2, z: -1.5, scaleX: 1.5, scaleY: 0.5, scaleZ: 4.5 }, 7)),
    )
  })

  it('Color 的线性分量与十六进制转换相同', async () => {
    const { Color } = await import('three')
    const color = new Color('#f0ede8')
    const linear = hexToLinear('#f0ede8')

    // three 存双精度，本项目存 Float32，允许量化差
    ;[color.r, color.g, color.b].forEach((value, channel) => {
      expect(linear[channel]).toBeCloseTo(value, 6)
    })
  })
})
