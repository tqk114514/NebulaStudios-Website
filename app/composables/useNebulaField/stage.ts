import { FOV } from './constants'
import { perspective, type Mat4 } from './gl/matrix'

export interface NebulaStage {
  gl: WebGL2RenderingContext
  /** 设备像素尺寸，视口与所有离屏目标按它分配 */
  width: number
  height: number
  dpr: number
  projection: Mat4
  /** 跟随 CSS 盒子重算尺寸与投影；尺寸未变时返回 false */
  resize: (cssWidth: number, cssHeight: number) => boolean
  dispose: () => void
}

/**
 * 建舞台：WebGL2 上下文 + 透视投影。拿不到上下文即返回 null，
 * 调用方因此可以静默降级，页面其余部分不受影响。
 * `alpha: false`——底色由不透明四边形铺满，无需与页面混合。
 */
export function createStage(canvas: HTMLCanvasElement, compact: boolean): NebulaStage | null {
  const gl = canvas.getContext('webgl2', {
    antialias: false, // 点精灵 + 泛光后期，MSAA 无用
    alpha: false,
    depth: false, // 全部深度测试关闭，绘制顺序由代码决定
    powerPreference: 'high-performance',
  })
  if (!gl) return null

  const dpr = Math.min(window.devicePixelRatio || 1, compact ? 1.5 : 2)
  const stage: NebulaStage = {
    gl,
    width: 1,
    height: 1,
    dpr,
    projection: perspective(FOV, 1, 0.1, 40),
    resize: (cssWidth, cssHeight) => {
      const width = Math.max(1, Math.round(cssWidth * dpr))
      const height = Math.max(1, Math.round(cssHeight * dpr))
      if (width === stage.width && height === stage.height) return false
      canvas.width = width
      canvas.height = height
      stage.width = width
      stage.height = height
      stage.projection = perspective(FOV, width / height, 0.1, 40)
      return true
    },
    dispose: () => {
      // 上下文留给浏览器回收太慢，主动让掉以立刻释放驱动侧内存
      gl.getExtension('WEBGL_lose_context')?.loseContext()
    },
  }

  return stage
}
