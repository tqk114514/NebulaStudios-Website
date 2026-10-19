import * as THREE from 'three'

export type ParticleKind = 0 | 1 | 2 // 0=Logo 1=星环 2=星尘

/**
 * 粒子顶点属性构建器：一次性填充全部属性后生成几何体，不支持增量更新。
 * 星环的轨道参数写进 aParams，运动在 GPU 每帧算，CPU 不参与。
 * aScatter/aDelay 只服务入场汇聚，巡航态下不参与成像。
 */
export class ParticleBuilder {
  private pos: number[] = []
  private scatter: number[] = []
  private delay: number[] = []
  private kind: number[] = []
  private params: number[] = []
  private tilt: number[] = []
  private size: number[] = []
  private seed: number[] = []
  private mix: number[] = []
  private alpha: number[] = []

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
    this.pos.push(p[0], p[1], p[2])
    this.scatter.push(scatter[0], scatter[1], scatter[2])
    this.delay.push(delay)
    this.kind.push(kind)
    this.params.push(params[0], params[1], params[2])
    this.tilt.push(tilt)
    this.size.push(size)
    this.seed.push(seed[0], seed[1])
    this.mix.push(mix)
    this.alpha.push(alpha)
  }

  geometry(): THREE.BufferGeometry {
    const scalar = (a: number[]) => new THREE.BufferAttribute(new Float32Array(a), 1)
    const vector = (a: number[]) => new THREE.BufferAttribute(new Float32Array(a), 3)
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', vector(this.pos))
    g.setAttribute('aScatter', vector(this.scatter))
    g.setAttribute('aDelay', scalar(this.delay))
    g.setAttribute('aKind', scalar(this.kind))
    g.setAttribute('aParams', vector(this.params))
    g.setAttribute('aTilt', scalar(this.tilt))
    g.setAttribute('aSize', scalar(this.size))
    g.setAttribute('aSeed', new THREE.BufferAttribute(new Float32Array(this.seed), 2))
    g.setAttribute('aMix', scalar(this.mix))
    g.setAttribute('aAlpha', scalar(this.alpha))
    return g
  }
}
