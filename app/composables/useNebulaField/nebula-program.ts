import { CAM_DIST, SCAN_RANGE, TILT } from './constants'
import { hexToLinear, readToken } from './gl/color'
import { compileProgram } from './gl/program'
import type { PointUniforms } from './gl/points-layer'
import { NEBULA_FRAG } from './shaders/nebula-frag'
import { NEBULA_VERT } from './shaders/nebula-vert'

/** 雕塑层与星场层共用同一个 program，取值各自一份 */
export function createNebulaProgram(gl: WebGL2RenderingContext): WebGLProgram {
  return compileProgram(gl, NEBULA_VERT, NEBULA_FRAG)
}

/**
 * 点层的 uniform 初值。色值取自 CSS 令牌并转进线性空间——画布只认令牌，
 * 换主题时须重建场景（读取只发生在构造期，不在每帧）。
 */
export function createNebulaUniforms(): PointUniforms {
  return {
    uTime: 0,
    uIntro: 0,
    uRotY: 0,
    uTiltX: TILT,
    uScanX: -SCAN_RANGE,
    uPixelRatio: 1,
    uSizeScale: 1,
    uCamDist: CAM_DIST,
    uGlobalAlpha: 1,
    uColorA: hexToLinear(readToken('--fg', '#f0ede8')),
    uColorB: hexToLinear(readToken('--nebula-ring', '#cfd6e4')),
  }
}
