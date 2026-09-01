// 页面加载遮罩状态（全局单例，PageLoader.vue 仅负责渲染）。
// 触发显示的场景：
// 1. /account 路由的懒加载 chunk 尚未解析（首次访问，网络加载期间）
// 2. 页面数据加载（如 Dashboard 的 fetchMe）：页面挂载时 hold、数据就绪后 release
// 隐藏前等待 document.fonts.ready——字体加载也在遮罩之内，避免文字闪替。
import { ref } from 'vue'
import type { RouteRecordNormalized } from 'vue-router'
import router from '@/router'

const visible = ref(false)

let hideTimer: number | undefined
let holdCount = 0

// 已解析过的路由组件（懒加载 chunk 已就绪）
const resolvedComponents = new WeakSet<object>()

/** 路由记录涉及的组件是否全部就绪（未解析的懒组件说明 chunk 需要网络加载） */
function isResolved(route: RouteRecordNormalized): boolean {
  return route.components
    ? Object.values(route.components).every((c) => c && resolvedComponents.has(c))
    : true
}

function markResolved(matched: RouteRecordNormalized[]) {
  for (const record of matched) {
    if (!record.components) continue
    for (const c of Object.values(record.components)) {
      if (c) resolvedComponents.add(c)
    }
  }
}

function show() {
  window.clearTimeout(hideTimer)
  visible.value = true
}

async function hide() {
  if (holdCount > 0) return
  // 字体加载纳入遮罩：就绪后再淡出，避免正文先以回退字体渲染再跳变
  try {
    await document.fonts.ready
  } catch {
    // fonts API 异常时直接隐藏
  }
  if (holdCount > 0) return
  // 短延迟淡出，避免极快的导航让遮罩闪烁
  hideTimer = window.setTimeout(() => (visible.value = false), 150)
}

// 仅 /account 页面启用路由级遮罩（依赖后端会话，切换有可感知延迟；
// home/policy/admin 不遮罩）；且仅在该路由组件尚未解析时显示。
router.beforeEach((to) => {
  if (to.path.startsWith('/account') && to.matched.some((r) => !isResolved(r))) show()
})
router.afterEach((to) => {
  markResolved(to.matched)
  void hide()
})
router.onError(() => {
  holdCount = 0
  void hide()
})

/** 页面数据加载期间持有遮罩（配对调用 release）。 */
export function holdPageLoader() {
  holdCount++
  show()
}

/** 数据就绪后释放遮罩（计数归零才隐藏）。 */
export function releasePageLoader() {
  holdCount = Math.max(0, holdCount - 1)
  if (holdCount === 0) void hide()
}

export function pageLoaderVisible() {
  return visible
}
