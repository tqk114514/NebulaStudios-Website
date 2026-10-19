import { onMounted, onScopeDispose, ref, shallowReadonly, type Ref } from 'vue'
import { requiredItemCount, tickerDuration, TICKER_MIN_ITEMS } from './track'

/**
 * 跑马灯的轨道条目数与周期时长。
 *
 * 接缝不靠测量：每条轨道平移自身宽度的 -100%，复位点就是相邻轨道的起点。
 * JS 只决定两件事——轨道要宽到能盖住视口（`requiredItemCount`），
 * 以及时长换算成恒定屏速（`tickerDuration`）。
 *
 * 覆盖条件与条目宽度都依赖字体度量，所以 `document.fonts.ready` 后重测一次；
 * 视口变宽由 ResizeObserver 触发重排。两者都只改时长，接缝始终精确。
 */
export function useTicker(rootRef: Ref<HTMLElement | null>) {
  const itemCount = ref(TICKER_MIN_ITEMS)
  const duration = ref('')
  let observer: ResizeObserver | null = null

  function measure(): void {
    // 轨道是根元素的子节点，取第一条即可代表整体：两条内容完全相同
    const root = rootRef.value
    const track = root?.firstElementChild
    if (!root || !(track instanceof HTMLElement)) return

    const viewportWidth = root.getBoundingClientRect().width
    const trackWidth = track.getBoundingClientRect().width
    if (viewportWidth <= 0 || trackWidth <= 0) return

    const itemWidth = trackWidth / itemCount.value
    itemCount.value = requiredItemCount(viewportWidth, itemWidth)
    // 条目同宽，新轨道宽度可解析得出，无需等重排后再读一次
    duration.value = tickerDuration(itemWidth * itemCount.value)
  }

  onMounted(() => {
    measure()
    if (rootRef.value) {
      observer = new ResizeObserver(measure)
      observer.observe(rootRef.value)
    }
    void document.fonts.ready.then(measure)
  })

  onScopeDispose(() => {
    observer?.disconnect()
    observer = null
  })

  return { itemCount: shallowReadonly(itemCount), duration: shallowReadonly(duration) }
}
