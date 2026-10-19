import { onMounted, onScopeDispose, type Ref } from 'vue'

/** 每次进入视口时揭示一次；揭示后不再干预 */
export function useReveal(rootRef: Ref<HTMLElement | null>): void {
  const animations = new Map<HTMLElement, Animation>()
  let observer: IntersectionObserver | null = null

  onMounted(() => {
    const root = rootRef.value
    if (!root) return
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return

    const targets = Array.from(root.querySelectorAll<HTMLElement>('[data-reveal]'))
    if (targets.length === 0) return

    const tokens = getComputedStyle(document.documentElement)
    const duration = Number.parseFloat(tokens.getPropertyValue('--dur-reveal')) || 900
    const stagger = Number.parseFloat(tokens.getPropertyValue('--stagger')) || 200
    const easing = tokens.getPropertyValue('--ease-emphasize').trim()
    const shift = tokens.getPropertyValue('--shift-reveal').trim() || '32px'

    observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const target = entry.target as HTMLElement
        animations.get(target)?.play()
        observer?.unobserve(target)
      }
    }, { threshold: 0.12 })

    // 隐藏态写在这里而不是 CSS 默认值：脚本没跑起来时正文必须可读。
    // 动画先建好再暂停在首帧，fill: both 让它停在"未显形"那一帧，
    // 避免同一帧内先改 opacity 再挂 transition 造成的淡出假象
    for (const el of targets) {
      const step = Number(el.dataset.reveal) || 0
      const animation = el.animate(
        [
          { opacity: 0, transform: `translateY(${shift})` },
          { opacity: 1, transform: 'none' },
        ],
        { duration, delay: step * stagger, easing, fill: 'both' },
      )
      animation.pause()
      animation.currentTime = 0
      animations.set(el, animation)
      observer.observe(el)
    }
  })

  onScopeDispose(() => {
    observer?.disconnect()
    observer = null
    for (const animation of animations.values()) animation.cancel()
    animations.clear()
  })
}
