/**
 * 假 WebGL2 上下文：让 scene 层在 Node 里跑通而不碰真 GPU，用于检验
 * `lifecycle.ts` 的编排（建场景、停循环、上下文丢失/恢复），不检验成像结果
 * （ADR-0001 的验证边界）。
 *
 * 未显式实现的方法经代理兜底为「记录调用并返回空对象」；GL 常量按名字发放
 * 稳定编号——`checkFramebufferStatus()` 与 `FRAMEBUFFER_COMPLETE` 因此可比较
 * （`gl/target.ts` 的完整性断言依赖这一点）。
 */

/** 按名字发放稳定编号的常量表；同进程内同名同值。 */
const constants = new Map<string, number>()

function constantOf(name: string): number {
  let value = constants.get(name)
  if (value === undefined) {
    value = constants.size + 1
    constants.set(name, value)
  }
  return value
}

type Handler = (args: unknown[]) => unknown

/** 返回值有语义要求的方法；其余走代理兜底。 */
function explicitHandlers(calls: string[]): Record<string, Handler> {
  return {
    getExtension: (args) =>
      args[0] === 'EXT_color_buffer_float' || args[0] === 'WEBGL_lose_context'
        ? {
            loseContext: (): void => void calls.push('loseContext'),
            restoreContext: (): void => void calls.push('restoreContext'),
          }
        : null,
    // 编译/链接结果只被真值判断；返回 true 即走通正常路径
    getShaderParameter: () => true,
    getProgramParameter: () => true,
    getShaderInfoLog: () => '',
    getProgramInfoLog: () => '',
    checkFramebufferStatus: () => constantOf('FRAMEBUFFER_COMPLETE'),
  }
}

function createFakeContext(calls: string[]): WebGL2RenderingContext {
  const target: Record<string | symbol, unknown> = {}
  for (const [name, handler] of Object.entries(explicitHandlers(calls))) {
    target[name] = (...args: unknown[]): unknown => {
      calls.push(name)
      return handler(args)
    }
  }
  return new Proxy(target, {
    get(self, property): unknown {
      if (property in self) return self[property]
      const name = String(property)
      if (/^[A-Z][A-Z0-9_]*$/.test(name)) return constantOf(name)
      return (): unknown => {
        calls.push(name)
        return {}
      }
    },
  }) as unknown as WebGL2RenderingContext
}

export interface FakeWebGL {
  /** 按调用顺序记录的方法名（含getExtension 的选择结果之外的调用） */
  calls: string[]
  /** 每次 `getContext('webgl2')` 产出的上下文；长度即建场景次数 */
  contexts: WebGL2RenderingContext[]
}

/** 在该 canvas 上安装假 WebGL2；非 webgl2 的请求（如 logo 采样的 2d）返回 null。 */
export function installFakeWebGL(canvas: HTMLCanvasElement): FakeWebGL {
  const calls: string[] = []
  const contexts: WebGL2RenderingContext[] = []
  const getContext = (type: string): WebGL2RenderingContext | null => {
    if (type !== 'webgl2') return null
    const context = createFakeContext(calls)
    contexts.push(context)
    return context
  }
  canvas.getContext = getContext as unknown as HTMLCanvasElement['getContext']
  return { calls, contexts }
}
