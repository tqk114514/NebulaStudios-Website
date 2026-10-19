import { INTRO_STAGGER } from './constants'
import { ParticleBuilder } from './particle-builder'
import { sampleLogoParticles } from './logo-particles'

const RINGS = [
  { r: 1.45, tilt: 0.42, n: [230, 140], speed: 0.1 },
  { r: 1.75, tilt: -0.3, n: [195, 118], speed: -0.07 },
  { r: 2.05, tilt: 0.62, n: [160, 98], speed: 0.05 },
] as const

// 起点球壳必须落在画面内：半径若大于视口半对角，入场期间画布上什么都没有，
// 观感就是"粒子突然从边缘涌进来"而不是"星尘析出成形"。
// 1.8–4.2 单位在常见视口下约 400–940px，多数粒子起始可见，少数仍从框外飞入。
const SCATTER_MIN = 1.8
const SCATTER_SPAN = 2.4

/** 入场起点：球壳上随机一点，落在雕塑缩放后的视口外，汇聚方向因此各不相同 */
function scatterPoint(): [number, number, number] {
  const theta = Math.random() * Math.PI * 2
  const phi = Math.acos(2 * Math.random() - 1)
  const r = SCATTER_MIN + Math.random() * SCATTER_SPAN
  return [
    r * Math.sin(phi) * Math.cos(theta),
    r * Math.sin(phi) * Math.sin(theta),
    r * Math.cos(phi),
  ]
}

const LOGO_DELAY_FROM = 0.28 // 字标最后落地，且自左向右扫着汇聚

/**
 * 雕塑本体：Logo 粒子板 + 三重星环 + 外围星尘，共处一个局部空间并整体自旋。
 * n 取 [完整, 窄屏] 两档，compact 时粒子数减半以保住帧预算。
 * 入场顺序星尘 → 星环（由内而外）→ 字标，读作"先有星尘再有字"。
 */
export function buildSculpture(compact: boolean): ParticleBuilder {
  const b = new ParticleBuilder()
  const logo = sampleLogoParticles(compact ? 880 : 1700)
  // 按采样点的真实 x 跨度归一化，字标不满宽时错峰窗口才不会空出一截
  const logoX = logo.map((p) => p.x)
  const logoMin = Math.min(...logoX)
  const logoSpan = Math.max(...logoX) - logoMin || 1

  // Logo 粒子的 alpha 必须压低：加法混合下亮度靠叠加累积，单体过亮会让整个
  // N 字内部大面积越过 Bloom 阈值，变成喂爆模糊链的"大块中亮区域"。
  // 宁可粒子多而单体暗。aSize 以相机参考距离处的 CSS 像素直径标定。
  for (const p of logo) {
    b.add(
      [p.x, p.y, (Math.random() * 2 - 1) * 0.16 * p.t],
      0,
      2.4 + Math.random() * 1.7,
      0.18 + Math.random() * 0.2,
      Math.random() * 0.18,
      [Math.random() * Math.PI * 2, 0.6 + Math.random() * 1.4],
      scatterPoint(),
      LOGO_DELAY_FROM + (INTRO_STAGGER - LOGO_DELAY_FROM) * ((p.x - logoMin) / logoSpan),
    )
  }

  for (const [index, ring] of RINGS.entries()) {
    for (let i = 0; i < (compact ? ring.n[1] : ring.n[0]); i++) {
      b.add(
        [0, 0, 0],
        1,
        1.9 + Math.random() * 1.1,
        0.12 + Math.random() * 0.2,
        0.65 + Math.random() * 0.3,
        [Math.random() * Math.PI * 2, 0.5 + Math.random() * 1.2],
        scatterPoint(),
        0.06 + index * 0.05 + Math.random() * 0.08,
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
      scatterPoint(),
      Math.random() * 0.05,
    )
  }

  return b
}
