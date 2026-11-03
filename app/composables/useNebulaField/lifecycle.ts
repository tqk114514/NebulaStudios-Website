import { COMPACT_MAX_WIDTH } from './constants'
import { createFrame, createFrameClock } from './frame'
import { createParallax } from './parallax'
import { createNebulaScene, type NebulaScene } from './scene'
import type { NebulaFieldOptions, NebulaFieldRuntime } from './types'

/** 构建一段可挂载/卸载的星云运行时。 */
export function createNebulaFieldRuntime(options: NebulaFieldOptions): NebulaFieldRuntime {
  const parallax = createParallax(() => options.heroRef.value?.getBoundingClientRect() ?? null)
  const clock = createFrameClock()
  let scene: NebulaScene | null = null
  let animId: number | null = null
  let resizeObs: ResizeObserver | null = null
  let visibleObs: IntersectionObserver | null = null
  let inView = true
  let compact = false
  let reduced = false

  const frame = createFrame(parallax, clock, () => inView, () => scene)

  /** 推进一帧后立刻排下一帧：视口外也保持 rAF 心跳，回来即续。 */
  function tick(now: number): void {
    frame(now)
    animId = requestAnimationFrame(tick)
  }

  /** 建立（或重建）场景、观察器与渲染循环；返回是否建成。 */
  function start(): boolean {
    const hero = options.heroRef.value
    const canvas = options.canvasRef.value
    if (!hero || !canvas) return false

    scene = createNebulaScene(canvas, hero, options.anchorRef?.value ?? null, compact, reduced)
    if (!scene) return false

    resizeObs = new ResizeObserver(() => scene?.resize())
    resizeObs.observe(hero)

    visibleObs = new IntersectionObserver((entries) => {
      for (const entry of entries) inView = entry.isIntersecting
    })
    visibleObs.observe(hero)

    if (reduced) return true // scene 内部已渲染静态帧
    animId = requestAnimationFrame(tick)
    return true
  }

  /** 停掉渲染循环与观察器；不动 scene，理由见 [`onContextLost`]。 */
  function stop(): void {
    if (animId !== null) cancelAnimationFrame(animId)
    animId = null
    resizeObs?.disconnect()
    resizeObs = null
    visibleObs?.disconnect()
    visibleObs = null
  }

  /**
   * 上下文丢失：必须 preventDefault，否则浏览器不会尝试恢复。丢弃 scene 而不
   * dispose——GL 资源已随上下文失效，逐个删除是无意义的空操作，还会误触 dispose
   * 里的 `loseContext()`；JS 侧对象随引用释放交给 GC。清 last 以免恢复后第一帧
   * 把长间隔当成大 dt（虽已被 0.05s 钳制）。
   */
  function onContextLost(event: Event): void {
    event.preventDefault()
    stop()
    scene = null
    clock.last = 0
  }

  /** 上下文恢复：在同一个 canvas 上重建全部 GL 资源。 */
  function onContextRestored(): void {
    start()
  }

  function attach(): void {
    const hero = options.heroRef.value
    const canvas = options.canvasRef.value
    if (!hero || !canvas) return

    compact = window.matchMedia(`(max-width: ${COMPACT_MAX_WIDTH}px)`).matches
    reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    // 视差是纯 JS 状态，跨上下文丢失/恢复保持注册
    canvas.addEventListener('webglcontextlost', onContextLost)
    canvas.addEventListener('webglcontextrestored', onContextRestored)

    if (start() && !reduced) {
      window.addEventListener('pointermove', parallax.onPointerMove, { passive: true })
    }
  }

  function detach(): void {
    const canvas = options.canvasRef.value
    canvas?.removeEventListener('webglcontextlost', onContextLost)
    canvas?.removeEventListener('webglcontextrestored', onContextRestored)
    stop()
    window.removeEventListener('pointermove', parallax.onPointerMove)
    scene?.dispose()
    scene = null
  }

  return { attach, detach }
}
