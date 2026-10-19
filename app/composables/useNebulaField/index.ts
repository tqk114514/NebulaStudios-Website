import { onMounted, onScopeDispose, type Ref } from 'vue'
import { COMPACT_MAX_WIDTH, SPIN } from './constants'
import { createParallax } from './parallax'
import { introProgress } from './intro'
import { createNebulaScene, type NebulaScene } from './scene'

export interface NebulaFieldOptions {
  /** 承载画布与视差判界的区域，通常是整个 hero */
  heroRef: Ref<HTMLElement | null>
  canvasRef: Ref<HTMLCanvasElement | null>
  /** 雕塑右锚点；其尺寸决定雕塑比例，宽度为 0 时退化为居中、缩小、压暗 */
  anchorRef?: Ref<HTMLElement | null>
}

/**
 * 挂载 WebGL 星云。仅客户端执行；WebGL2 不可用时静默跳过，
 * prefers-reduced-motion 下渲染一帧静态图后不进循环。
 */
export function useNebulaField(options: NebulaFieldOptions): void {
  const parallax = createParallax(() => options.heroRef.value?.getBoundingClientRect() ?? null)
  let scene: NebulaScene | null = null
  let animId: number | null = null
  let resizeObs: ResizeObserver | null = null
  let visibleObs: IntersectionObserver | null = null
  let last = 0
  let rotY = 0
  let introElapsed = 0
  let inView = true

  function frame(now: number) {
    animId = requestAnimationFrame(frame)
    // hero 滚出视口或标签页隐藏时跳过渲染，rAF 保持心跳，恢复即续
    if (!inView || document.hidden) {
      last = now
      return
    }
    const dt = last === 0 ? 0.016 : Math.min(0.05, (now - last) / 1000)
    last = now
    rotY += dt * SPIN
    // 入场时钟只在真正出帧的帧上推进：中途滚出视口则暂停，回来接着播
    const intro = introProgress(introElapsed)
    if (intro < 1) introElapsed += dt
    const { x, y } = parallax.advance()
    scene?.render({ time: now / 1000, spin: rotY, parX: x, parY: y, intro })
  }

  onMounted(() => {
    const hero = options.heroRef.value
    const canvas = options.canvasRef.value
    if (!hero || !canvas) return

    const compact = window.matchMedia(`(max-width: ${COMPACT_MAX_WIDTH}px)`).matches
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    scene = createNebulaScene(canvas, hero, options.anchorRef?.value ?? null, compact, reduced)
    if (!scene) return

    resizeObs = new ResizeObserver(() => scene?.resize())
    resizeObs.observe(hero)

    visibleObs = new IntersectionObserver((entries) => {
      for (const entry of entries) inView = entry.isIntersecting
    })
    visibleObs.observe(hero)

    if (reduced) return // scene 内部已渲染静态帧
    window.addEventListener('pointermove', parallax.onPointerMove, { passive: true })
    animId = requestAnimationFrame(frame)
  })

  onScopeDispose(() => {
    if (animId !== null) cancelAnimationFrame(animId)
    animId = null
    window.removeEventListener('pointermove', parallax.onPointerMove)
    resizeObs?.disconnect()
    visibleObs?.disconnect()
    scene?.dispose()
    scene = null
  })
}
