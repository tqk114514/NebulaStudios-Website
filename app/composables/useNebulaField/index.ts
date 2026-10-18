import { onMounted, onScopeDispose, type Ref } from 'vue'
import { COMPACT_MAX_WIDTH, SPIN } from './constants'
import { createNebulaScene, type NebulaScene } from './scene'

export interface NebulaFieldOptions {
  /** 承载画布与指针监听的区域，通常是整个 hero */
  heroRef: Ref<HTMLElement | null>
  canvasRef: Ref<HTMLCanvasElement | null>
  /** 雕塑右锚点；隐藏（宽度归零）时雕塑退化为居中缩小压暗 */
  anchorRef?: Ref<HTMLElement | null>
}

/**
 * 挂载 WebGL 星云。仅客户端执行；WebGL2 不可用时静默跳过，
 * prefers-reduced-motion 下渲染一帧静态图后不再进循环。
 */
export function useNebulaField(options: NebulaFieldOptions): void {
  let scene: NebulaScene | null = null
  let animId: number | null = null
  let resizeObs: ResizeObserver | null = null
  let visibleObs: IntersectionObserver | null = null
  let last = 0
  let rotY = 0
  let targetX = 0
  let targetY = 0
  let curX = 0
  let curY = 0
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
    curX += (targetX - curX) * 0.05
    curY += (targetY - curY) * 0.05
    scene?.render(now / 1000, rotY, curX, curY)
  }

  function onPointerMove(e: PointerEvent) {
    const hero = options.heroRef.value
    if (!hero) return
    const r = hero.getBoundingClientRect()
    // 画布不接管指针事件，因此监听 window 并按 hero 矩形判界
    if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) return
    targetX = ((e.clientX - (r.left + r.width / 2)) / r.width) * 0.5
    targetY = ((e.clientY - (r.top + r.height / 2)) / r.height) * 0.3
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
    window.addEventListener('pointermove', onPointerMove, { passive: true })
    animId = requestAnimationFrame(frame)
  })

  onScopeDispose(() => {
    if (animId !== null) cancelAnimationFrame(animId)
    animId = null
    window.removeEventListener('pointermove', onPointerMove)
    resizeObs?.disconnect()
    visibleObs?.disconnect()
    scene?.dispose()
    scene = null
  })
}
