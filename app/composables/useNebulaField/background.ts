import { hexToLinear, readToken } from './gl/color'
import { createPass } from './gl/pass'
import { BACKGROUND_FRAG } from './shaders/post'

/**
 * 底色铺满当前目标，替代 GL 清屏：清屏路径的色彩约定与这条线性链不同源，
 * 混用会把 --bg 二次提亮。色值在此处一次性取好——主题是固定的，
 * uniform 存在 program 上，后续换 program 不影响它。
 */
export function createBackground(gl: WebGL2RenderingContext) {
  const pass = createPass(gl, BACKGROUND_FRAG, ['uColor'])
  gl.useProgram(pass.program)
  gl.uniform3fv(pass.locations.uColor, hexToLinear(readToken('--bg', '#0d0d0d')))
  return { draw: pass.draw, dispose: pass.dispose }
}
