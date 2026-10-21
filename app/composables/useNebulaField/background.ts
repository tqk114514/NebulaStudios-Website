import { hexToLinear, readToken } from './gl/color'
import { createPass } from './gl/pass'
import { BACKGROUND_FRAG } from './shaders/post'

/**
 * 底色铺满当前目标，替代 GL 清屏：清屏路径的色彩约定与这条线性链不同源，
 * 混用会把 --bg 二次提亮。它同时充当每帧的擦除——加法混合的点叠在它上面，
 * 不重铺就会逐帧累积成拖尾与过曝。
 */
export function createBackground(gl: WebGL2RenderingContext) {
  const pass = createPass(gl, BACKGROUND_FRAG, ['uColor'])
  gl.useProgram(pass.program)
  gl.uniform3fv(pass.locations.uColor, hexToLinear(readToken('--bg', '#0d0d0d')))
  return {
    draw: () => {
      gl.useProgram(pass.program)
      gl.disable(gl.BLEND)
      pass.draw()
    },
    dispose: pass.dispose,
  }
}
