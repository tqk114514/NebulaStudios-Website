import { onMounted, onScopeDispose } from 'vue'
import { createNebulaFieldRuntime } from './lifecycle'
import type { NebulaFieldOptions } from './types'

export type { NebulaFieldOptions }

/**
 * 挂载 WebGL 星云。仅客户端执行；WebGL2 不可用时静默跳过，
 * prefers-reduced-motion 下渲染一帧静态图后不进循环。
 * 上下文丢失后由 `webglcontextrestored` 在同一 canvas 上重建（见 `lifecycle.ts`）。
 */
export function useNebulaField(options: NebulaFieldOptions): void {
  // 运行时在 setup 期构建，ref 到 attach 时才赋值
  const runtime = createNebulaFieldRuntime(options)
  onMounted(() => runtime.attach())
  onScopeDispose(() => runtime.detach())
}
