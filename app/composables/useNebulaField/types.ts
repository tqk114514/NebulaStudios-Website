import type { Ref } from 'vue'

/** 星云的挂载目标：三个由调用方提供的元素引用。 */
export interface NebulaFieldOptions {
  /** 承载画布与视差判界的区域，通常是整个 hero */
  heroRef: Ref<HTMLElement | null>
  canvasRef: Ref<HTMLCanvasElement | null>
  /** 雕塑右锚点；其尺寸决定雕塑比例，宽度为 0 时退化为居中、缩小、压暗 */
  anchorRef?: Ref<HTMLElement | null>
}

/** 挂载期运行时；`index.ts` 只负责把它接到 Vue 生命周期上。 */
export interface NebulaFieldRuntime {
  /** 读取 ref、注册监听并启动；WebGL2 不可用时静默降级。 */
  attach: () => void
  /** 移除监听、停循环并释放 GL 资源。 */
  detach: () => void
}
