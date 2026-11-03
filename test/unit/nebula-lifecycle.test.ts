// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createNebulaFieldRuntime } from '~/composables/useNebulaField/lifecycle'
import { installFakeWebGL, type FakeWebGL } from './fake-webgl'

// rAF 不自动出帧：测试按需断言「排了几帧」「取消了几帧」
let rafCallbacks: Array<(now: number) => void>
let cancelledFrames: number[]
let fake: FakeWebGL
let canvas: HTMLCanvasElement

beforeEach(() => {
  canvas = document.createElement('canvas')
  document.body.append(canvas)
  fake = installFakeWebGL(canvas)
  rafCallbacks = []
  cancelledFrames = []
  vi.stubGlobal('requestAnimationFrame', (callback: (now: number) => void) => {
    rafCallbacks.push(callback)
    return rafCallbacks.length
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => void cancelledFrames.push(id))
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe(): void {}
      disconnect(): void {}
    },
  )
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe(): void {}
      disconnect(): void {}
    },
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function attach(): ReturnType<typeof createNebulaFieldRuntime> {
  const runtime = createNebulaFieldRuntime({
    heroRef: ref(document.createElement('div')),
    canvasRef: ref(canvas),
    anchorRef: ref(document.createElement('div')),
  })
  runtime.attach()
  return runtime
}

function dispatchLost(): Event {
  const event = new Event('webglcontextlost', { cancelable: true })
  canvas.dispatchEvent(event)
  return event
}

function callsOf(name: string): number {
  return fake.calls.filter((call) => call === name).length
}

it('挂载即建场景并排入首帧', () => {
  attach()
  expect(fake.contexts.length).toBe(1)
  expect(callsOf('createProgram')).toBeGreaterThan(0)
  expect(rafCallbacks.length).toBe(1)
})

it('上下文丢失 preventDefault、停帧且不逐个删资源', () => {
  attach()
  const event = dispatchLost()
  expect(event.defaultPrevented).toBe(true)
  expect(cancelledFrames.length).toBe(1)
  // 丢弃而不 dispose：GL 资源已随上下文失效，不得误触 loseContext
  expect(fake.calls).not.toContain('loseContext')
})

it('恢复后在同一 canvas 上重建全部资源并续上循环', () => {
  attach()
  const programsBefore = callsOf('createProgram')
  dispatchLost()
  canvas.dispatchEvent(new Event('webglcontextrestored'))
  expect(fake.contexts.length).toBe(2)
  expect(callsOf('createProgram')).toBeGreaterThan(programsBefore)
  expect(rafCallbacks.length).toBe(2)
})

it('detach 移除监听并释放资源', () => {
  const runtime = attach()
  runtime.detach()
  expect(fake.calls).toContain('loseContext')
  const contextsBefore = fake.contexts.length
  canvas.dispatchEvent(new Event('webglcontextrestored'))
  expect(fake.contexts.length).toBe(contextsBefore)
})

it('WebGL2 不可用时静默降级', () => {
  canvas.getContext = () => null
  expect(() => attach()).not.toThrow()
  expect(rafCallbacks.length).toBe(0)
})

it('prefers-reduced-motion 只渲染静态帧不进循环', () => {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('prefers-reduced-motion'),
  }))
  attach()
  expect(fake.contexts.length).toBe(1)
  expect(rafCallbacks.length).toBe(0)
})
