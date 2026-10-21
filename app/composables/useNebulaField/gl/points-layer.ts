import type { LinearColor } from './color'
import { modelView, type Mat4, type ObjectTransform } from './matrix'
import type { PointGeometry } from './geometry'
import { uniformLocations } from './program'

/** uniform 名与着色器里的声明逐字相同，取值即写在这份记录上 */
export interface PointUniforms {
  uTime: number
  uIntro: number
  uRotY: number
  uTiltX: number
  uScanX: number
  uPixelRatio: number
  uSizeScale: number
  uCamDist: number
  uGlobalAlpha: number
  uColorA: LinearColor
  uColorB: LinearColor
}

export interface PointsLayer {
  uniforms: PointUniforms
  setTransform: (transform: ObjectTransform) => void
  draw: (projection: Mat4) => void
  dispose: () => void
}

const NAMES = [
  'uTime',
  'uIntro',
  'uRotY',
  'uTiltX',
  'uScanX',
  'uPixelRatio',
  'uSizeScale',
  'uCamDist',
  'uGlobalAlpha',
  'uColorA',
  'uColorB',
] as const satisfies readonly (keyof PointUniforms)[]

/** 上传时按值类型分流，矩阵两个单独走 uniformMatrix4fv */
const LOCATIONS = [...NAMES, 'modelViewMatrix', 'projectionMatrix'] as const

/**
 * 一层点精灵：共享 program，各自持有顶点数组与 uniform 取值。
 * program 的 uniform 是全局的，故每次绘制都把自己这份取值重新上传。
 */
export function createPointsLayer(
  gl: WebGL2RenderingContext,
  program: WebGLProgram,
  geometry: PointGeometry,
  uniforms: PointUniforms,
): PointsLayer {
  const locations = uniformLocations(gl, program, LOCATIONS)
  let transform: ObjectTransform = { x: 0, y: 0, z: 0, scaleX: 1, scaleY: 1, scaleZ: 1 }

  return {
    uniforms,
    setTransform: (next) => {
      transform = next
    },
    draw: (projection) => {
      gl.useProgram(program)
      gl.bindVertexArray(geometry.vao)
      // 加法混合且无深度：绘制顺序无关紧要，星尘叠加自然变亮
      gl.enable(gl.BLEND)
      gl.blendFunc(gl.SRC_ALPHA, gl.ONE)
      gl.uniformMatrix4fv(locations.projectionMatrix, false, projection)
      gl.uniformMatrix4fv(
        locations.modelViewMatrix,
        false,
        modelView(transform, uniforms.uCamDist),
      )
      for (const name of NAMES) {
        const value = uniforms[name]
        const location = locations[name]
        if (typeof value === 'number') gl.uniform1f(location, value)
        else gl.uniform3fv(location, value)
      }
      gl.drawArrays(gl.POINTS, 0, geometry.count)
    },
    dispose: geometry.dispose,
  }
}
