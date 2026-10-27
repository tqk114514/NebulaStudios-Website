import { LOGO_PATHS } from '~/utils/logo-mark'

export interface LogoPoint {
  x: number
  y: number
  /** 3×3 邻域实心率（步长 4px）：1=深处内部，0=贴边 */
  t: number
}

const SIDE = 256
const CENTER = 128
const SCALE = 100

/**
 * 把 Logo path 画到离屏画布后按像素采样，得到轮廓内坐标。
 * t 用于生成倒角厚度——中心厚、边缘薄，旋转到侧面时读作一块有倒角的立体板，
 * 而不是均匀薄片。
 */
export function sampleLogoParticles(count: number): LogoPoint[] {
  const off = document.createElement('canvas')
  off.width = SIDE
  off.height = SIDE
  const ctx = off.getContext('2d', { willReadFrequently: true })
  if (!ctx) return []

  ctx.fillStyle = '#fff'
  for (const d of LOGO_PATHS) ctx.fill(new Path2D(d))
  const data = ctx.getImageData(0, 0, SIDE, SIDE).data

  const filled = (px: number, py: number) =>
    px >= 0 && py >= 0 && px < SIDE && py < SIDE && (data[(py * SIDE + px) * 4 + 3] ?? 0) > 128

  const inside: LogoPoint[] = []
  for (let py = 0; py < SIDE; py += 2) {
    for (let px = 0; px < SIDE; px += 2) {
      if (!filled(px, py)) continue
      let n = 0
      for (let dy = -4; dy <= 4; dy += 4) {
        for (let dx = -4; dx <= 4; dx += 4) {
          if (filled(px + dx, py + dy)) n++
        }
      }
      inside.push({ x: px, y: py, t: n / 9 })
    }
  }
  if (inside.length === 0) return []

  const points: LogoPoint[] = []
  for (let i = 0; i < count; i++) {
    const p = inside[Math.floor(Math.random() * inside.length)]
    if (!p) continue
    // 以 128 为中心归一化到 [-1, 1]，Y 翻转（屏幕坐标向下）
    points.push({ x: (p.x - CENTER) / SCALE, y: -(p.y - CENTER) / SCALE, t: p.t })
  }
  return points
}
