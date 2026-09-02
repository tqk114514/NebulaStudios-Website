// 页面加载遮罩状态（全局单例，PageLoader.vue 仅负责渲染）。
// 每次路由跳转都走完整遮罩流程：渐显（ENTER_MS 保底）→ 切换页面 → 渐隐，
// 保证用户永远看不到路由切换本身——切页只发生在遮罩完全不透明时：
// - 同路径导航（admin 单页内 hash 区块切换）组件不变、无可见切换，跳过遮罩
// - 懒加载 chunk 在渐显期间并行预热（warmComponents），加载赶在渐显内完成则不额外等待，
//   否则遮罩保持不透明直到组件就绪
// - enterDeadline 以"遮罩首次出现"为基准：守卫重定向、快速连点、渐隐中再次导航
//   都会从当前动画状态续满整个渐显期，切换永远不提前
// - 页面数据加载（如 Dashboard 的 fetchMe）通过 hold/release 继续持有遮罩
// - 隐藏前等待 document.fonts.ready，避免正文先以回退字体渲染再跳变
import { ref } from 'vue'
import type { RouteLocationNormalized } from 'vue-router'
import router from '@/router'

const visible = ref(false)

/** 渐显保底时长：略大于 0.3s 的透明度过渡，留少量全不透明缓冲 */
const ENTER_MS = 380

/** 当前渐显期截止时刻（时间戳）；遮罩已完全可见时为未来时刻 */
let enterDeadline = 0

let holdCount = 0

function show(): void {
  const first = !visible.value
  visible.value = true
  if (first) enterDeadline = Date.now() + ENTER_MS
}

async function hide(): Promise<void> {
  if (holdCount > 0) return
  try {
    await document.fonts.ready
  } catch {
    // fonts API 异常时直接隐藏
  }
  if (holdCount > 0) return
  visible.value = false
}

/** 懒加载 chunk 预热：触发动态 import 与渐显动画并行（模块缓存使路由解析瞬间完成） */
function warmComponents(to: RouteLocationNormalized): void {
  for (const record of to.matched) {
    for (const comp of Object.values(record.components ?? {})) {
      if (typeof comp === 'function') {
        void (comp as () => Promise<unknown>)().catch(() => {})
      }
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

router.beforeEach(async (to, from) => {
  // 同路径导航（如 admin 单页内 hash 区块切换）：组件不变、无可见切换，跳过遮罩
  if (to.path === from.path) return
  show()
  warmComponents(to)
  // 守卫内等待渐显期结束（auth bootstrap 等守卫逻辑与此并行执行）
  const remaining = enterDeadline - Date.now()
  if (remaining > 0) await sleep(remaining)
})

router.afterEach(() => {
  void hide()
})

router.onError(() => {
  holdCount = 0
  void hide()
})

/** 页面数据加载期间持有遮罩（配对调用 release）。 */
export function holdPageLoader(): void {
  holdCount++
  show()
}

/** 数据就绪后释放遮罩（计数归零才隐藏）。 */
export function releasePageLoader(): void {
  holdCount = Math.max(0, holdCount - 1)
  if (holdCount === 0) void hide()
}

export function pageLoaderVisible() {
  return visible
}
