import { ParticleBuilder } from './particle-builder'
import { sampleLogoParticles } from './logo-particles'

const RINGS = [
  { r: 1.45, tilt: 0.42, n: [230, 140], speed: 0.1 },
  { r: 1.75, tilt: -0.3, n: [195, 118], speed: -0.07 },
  { r: 2.05, tilt: 0.62, n: [160, 98], speed: 0.05 },
] as const

/**
 * 雕塑本体：Logo 粒子板 + 三重星环 + 外围星尘，共处一个局部空间并整体自旋。
 * n 取 [完整, 窄屏] 两档，compact 时粒子数减半以保住帧预算。
 */
export function buildSculpture(compact: boolean): ParticleBuilder {
  const b = new ParticleBuilder()

  // Logo 粒子的 alpha 必须压低：加法混合下亮度靠叠加累积，单体过亮会让整个
  // N 字内部大面积越过 Bloom 阈值，变成喂爆模糊链的"大块中亮区域"。
  // 宁可粒子多而单体暗。aSize 以相机参考距离处的 CSS 像素直径标定。
  for (const p of sampleLogoParticles(compact ? 880 : 1700)) {
    b.add(
      [p.x, p.y, (Math.random() * 2 - 1) * 0.16 * p.t],
      0,
      2.4 + Math.random() * 1.7,
      0.18 + Math.random() * 0.2,
      Math.random() * 0.18,
      [Math.random() * Math.PI * 2, 0.6 + Math.random() * 1.4],
    )
  }

  for (const ring of RINGS) {
    for (let i = 0; i < (compact ? ring.n[1] : ring.n[0]); i++) {
      b.add(
        [0, 0, 0],
        1,
        1.9 + Math.random() * 1.1,
        0.12 + Math.random() * 0.2,
        0.65 + Math.random() * 0.3,
        [Math.random() * Math.PI * 2, 0.5 + Math.random() * 1.2],
        [
          ring.r + (Math.random() - 0.5) * 0.12,
          Math.random() * Math.PI * 2,
          ring.speed * (0.85 + Math.random() * 0.3),
        ],
        ring.tilt,
      )
    }
  }

  // 星尘：球壳分布，半径从星环外沿起
  const dustCount = compact ? 160 : 300
  for (let i = 0; i < dustCount; i++) {
    const theta = Math.random() * Math.PI * 2
    const phi = Math.acos(2 * Math.random() - 1)
    const r = 2.3 + Math.random() * 2.3
    b.add(
      [
        r * Math.sin(phi) * Math.cos(theta),
        r * Math.sin(phi) * Math.sin(theta),
        r * Math.cos(phi),
      ],
      2,
      1.5 + Math.random() * 1.2,
      0.06 + Math.random() * 0.1,
      0.3 + Math.random() * 0.4,
      [Math.random() * Math.PI * 2, 0.4 + Math.random() * 1.0],
    )
  }

  return b
}
