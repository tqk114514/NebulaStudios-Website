import { ParticleBuilder } from './particle-builder'

/**
 * 背景星场：粒子分布在单位立方体内，由对象 scale 拉伸铺满整个视锥（含前后景深）。
 * 只做轻微视差倾斜、不随雕塑自旋——保证任何视口下整个 hero 都有星尘，而非只有右侧。
 * 入场起点取自身位置的 1.6 倍，即视锥之外，因此是从画面外滑入而非原地显形。
 */
export function buildStarField(compact: boolean): ParticleBuilder {
  const b = new ParticleBuilder()
  const count = compact ? 180 : 400

  for (let i = 0; i < count; i++) {
    const p: readonly [number, number, number] = [
      Math.random() * 2 - 1,
      Math.random() * 2 - 1,
      Math.random() * 2 - 1,
    ]
    b.add(
      p,
      2,
      1.2 + Math.random() * 1.2,
      0.04 + Math.random() * 0.08,
      0.25 + Math.random() * 0.45,
      [Math.random() * Math.PI * 2, 0.3 + Math.random() * 0.7],
      [p[0] * 1.6, p[1] * 1.6, p[2] * 1.6],
      Math.random() * 0.35,
    )
  }

  return b
}
