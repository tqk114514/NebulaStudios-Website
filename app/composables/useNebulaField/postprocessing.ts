import { bindScreen, createPass } from './gl/pass'
import type { ColorTarget } from './gl/target'
import { OUTPUT_FRAG } from './shaders/post'

export interface Postprocessing {
  /** 写入本帧时间，驱动抖动的噪声相位 */
  setTime: (time: number) => void
  render: (scene: ColorTarget, screenWidth: number, screenHeight: number) => void
  dispose: () => void
}

/**
 * 末趟：场景 + 泛光 → OETF → 抖动 → 画布。
 * `bloom` 为 null（无浮点可写目标）时绑一张 1×1 黑纹理——加零比再开一条 program 便宜，
 * 绘制路径也因此只有一条。
 */
export function createPostprocessing(
  gl: WebGL2RenderingContext,
  bloom: WebGLTexture | null,
): Postprocessing {
  const pass = createPass(gl, OUTPUT_FRAG, ['uSource', 'uBloom', 'uTime'] as const)
  const ownsBloom = bloom === null
  const bloomTexture = ownsBloom ? createBlackTexture(gl) : bloom

  gl.useProgram(pass.program)
  gl.uniform1i(pass.locations.uSource, 0)
  gl.uniform1i(pass.locations.uBloom, 1)

  return {
    setTime: (time) => {
      gl.useProgram(pass.program)
      gl.uniform1f(pass.locations.uTime, time)
    },
    render: (scene, screenWidth, screenHeight) => {
      gl.useProgram(pass.program)
      gl.activeTexture(gl.TEXTURE0)
      gl.bindTexture(gl.TEXTURE_2D, scene.texture)
      gl.activeTexture(gl.TEXTURE1)
      gl.bindTexture(gl.TEXTURE_2D, bloomTexture)
      bindScreen(gl, screenWidth, screenHeight)
      gl.disable(gl.BLEND)
      pass.draw()
    },
    dispose: () => {
      if (ownsBloom) gl.deleteTexture(bloomTexture)
      pass.dispose()
    },
  }
}

function createBlackTexture(gl: WebGL2RenderingContext): WebGLTexture {
  const texture = gl.createTexture()
  if (!texture) throw new Error('WebGL2: createTexture 返回 null')
  gl.bindTexture(gl.TEXTURE_2D, texture)
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([0, 0, 0, 0]))
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  gl.bindTexture(gl.TEXTURE_2D, null)
  return texture
}
