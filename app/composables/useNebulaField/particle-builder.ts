import type { PointArrays, PointField } from './gl/geometry'

export type ParticleKind = 0 | 1 | 2 // 0=Logo 1=星环 2=星尘

const FIELDS: readonly PointField[] = [
  'position',
  'aScatter',
  'aDelay',
  'aKind',
  'aParams',
  'aTilt',
  'aSize',
  'aSeed',
  'aMix',
  'aAlpha',
]

/**
 * 粒子顶点属性构建器：一次性填充全部属性，不支持增量更新。
 * 星环的轨道参数写进 aParams，运动在 GPU 每帧算，CPU 不参与。
 * aScatter/aDelay 只服务入场汇聚，巡航态下不参与成像。
 * 属性按字段分组存放，交错与上传由 `gl/geometry.ts` 完成。
 */
export class ParticleBuilder {
  private readonly grouped = Object.fromEntries(
    FIELDS.map((field) => [field, [] as number[]]),
  ) as PointArrays

  private vertexCount = 0

  get attributes(): PointArrays {
    return this.grouped
  }

  get count(): number {
    return this.vertexCount
  }

  add(
    p: readonly [number, number, number],
    kind: ParticleKind,
    size: number,
    alpha: number,
    mix: number, // 颜色混合：0=暖白 → 1=冷白
    seed: readonly [number, number], // 闪烁相位 / 速度
    scatter: readonly [number, number, number], // 入场起点
    delay: number, // 入场错峰，∈ [0, INTRO_STAGGER]
    params: readonly [number, number, number] = [0, 0, 0],
    tilt = 0,
  ) {
    const next: [PointField, readonly number[]][] = [
      ['position', p],
      ['aScatter', scatter],
      ['aDelay', [delay]],
      ['aKind', [kind]],
      ['aParams', params],
      ['aTilt', [tilt]],
      ['aSize', [size]],
      ['aSeed', seed],
      ['aMix', [mix]],
      ['aAlpha', [alpha]],
    ]
    for (const [field, values] of next) this.grouped[field].push(...values)
    this.vertexCount += 1
  }
}
