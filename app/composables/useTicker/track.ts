/**
 * 跑马灯轨道的纯几何。双轨道各含完整序列、各自平移自身宽度的 -100%，
 * 复位点与相邻轨道的起点重合，接缝因此不依赖任何测量精度。
 */

export const TICKER_SPEED = 36 // px/s，与 v3 的 0.6px/帧@60fps 等速

/**
 * 轨道宽度需 ≥ 视口宽 × 此系数：t 处于周期末尾时第一条轨道已完全移出，
 * 视口全靠第二条覆盖。取 2 是为了横向拉伸窗口到两倍以内时不必重排轨道。
 */
export const TICKER_COVERAGE = 2

/** 单条轨道的最少条目数，保证窄视口下也是一条完整轨道而非半截字 */
export const TICKER_MIN_ITEMS = 8

/**
 * 覆盖所需条目数。itemWidth ≤ 0（字体未就绪或元素未布局）时退回下限，
 * 让下一次测量来修正，而不是把轨道算成空。
 */
export function requiredItemCount(viewportWidth: number, itemWidth: number): number {
  if (itemWidth <= 0) return TICKER_MIN_ITEMS
  const needed = Math.ceil((viewportWidth * TICKER_COVERAGE) / itemWidth)
  return Math.max(TICKER_MIN_ITEMS, needed)
}

/**
 * 一个周期的时长。位移量恒为轨道宽度，所以时长 = 宽度 ÷ 速度时，
 * 屏上速度与该速度常量一致，与轨道宽度和屏幕刷新率无关。
 */
export function tickerDuration(trackWidth: number, speed: number = TICKER_SPEED): string {
  if (trackWidth <= 0 || speed <= 0) return ''
  return `${(trackWidth / speed).toFixed(2)}s`
}
