import { INTRO_DURATION, INTRO_SPIN_FLOOR, INTRO_TRAVEL } from './constants'

/** 入场进度：已推进的秒数折算成 0..1，1 之后即巡航态 */
export function introProgress(elapsed: number): number {
  return Math.min(1, Math.max(0, elapsed / INTRO_DURATION))
}

/**
 * 自旋速度倍率。曲线与 --ease-brand 同形（两端缓、中段快），
 * 首帧压到 INTRO_SPIN_FLOOR，intro=1 时回到 1。
 */
export function spinScale(progress: number): number {
  const t = Math.min(1, Math.max(0, progress))
  const eased = t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2
  return INTRO_SPIN_FLOOR + (1 - INTRO_SPIN_FLOOR) * eased
}

/**
 * 单个粒子的本地进度。顶点着色器实现的是同一式子（`q = (uIntro - aDelay) / TRAVEL`），
 * 两处需一起改。
 */
export function particleProgress(intro: number, delay: number): number {
  return (intro - delay) / INTRO_TRAVEL
}

/** 落地闪光强度：q=1 最亮，q 落出 [0.8, 1.2] 之外为 0 */
export function landingFlash(q: number): number {
  return Math.max(0, 1 - Math.abs(q - 1) * 5)
}

/**
 * 扫描相位时钟：入场结束前恒返回 0，让光扫停在起点等组装完成，之后按帧间隔累加。
 * dt 夹在与渲染循环相同的上限里，切后台回来不跳相位。
 */
export function createScanClock(): (dt: number, intro: number) => number {
  let elapsed = 0
  return (dt, intro) => {
    if (intro < 1) return 0
    elapsed += Math.min(0.05, Math.max(0, dt))
    return elapsed
  }
}
