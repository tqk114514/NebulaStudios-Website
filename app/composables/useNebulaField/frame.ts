import { SPIN } from './constants'
import { introProgress } from './intro'
import type { Parallax } from './parallax'
import type { NebulaScene } from './scene'

/** 出帧时钟：上次出帧时刻、累计自旋与入场已用时长。 */
export interface FrameClock {
  last: number
  rotY: number
  introElapsed: number
}

/** 归零的出帧时钟；上下文丢失后不清零，恢复时接着播。 */
export function createFrameClock(): FrameClock {
  return { last: 0, rotY: 0, introElapsed: 0 }
}

/**
 * 造出每帧回调。hero 滚出视口或标签页隐藏时跳过渲染（rAF 心跳由调用方保持）；
 * 入场时钟只在真正出帧的帧上推进，中途滚出视口则暂停、回来接着播。
 * `sceneOf` 每帧读取当前场景——上下文丢失期间为 null，恢复后自动续上。
 */
export function createFrame(
  parallax: Parallax,
  clock: FrameClock,
  isInView: () => boolean,
  sceneOf: () => NebulaScene | null,
): (now: number) => void {
  return (now) => {
    if (!isInView() || document.hidden) {
      clock.last = now
      return
    }
    const dt = clock.last === 0 ? 0.016 : Math.min(0.05, (now - clock.last) / 1000)
    clock.last = now
    clock.rotY += dt * SPIN
    const intro = introProgress(clock.introElapsed)
    if (intro < 1) clock.introElapsed += dt
    const { x, y } = parallax.advance()
    sceneOf()?.render({ time: now / 1000, spin: clock.rotY, parX: x, parY: y, intro })
  }
}
