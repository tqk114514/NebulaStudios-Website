import { compileProgram, uniformLocations } from './program'

/**
 * 覆盖全屏的三角形，顶点由 `gl_VertexID` 生成，不占顶点缓冲与 VAO 属性。
 * uv 取 (0,0)(2,0)(0,2)，裁剪空间即 (-1,-1)(3,-1)(-1,3)。
 */
const FULLSCREEN_VERT = /* glsl */ `#version 300 es
out vec2 vUv;

void main() {
  vec2 uv = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
  vUv = uv;
  gl_Position = vec4(uv * 2.0 - 1.0, 0.0, 1.0);
}
`

function drawFullscreen(gl: WebGL2RenderingContext): void {
  // 空 VAO：着色器不读任何顶点属性
  gl.bindVertexArray(null)
  gl.drawArrays(gl.TRIANGLES, 0, 3)
}

/** 把纹理挂到指定单元。多趟之间靠固定单元号传递，uniform 只设一次 */
export function bindTextureUnit(
  gl: WebGL2RenderingContext,
  unit: number,
  texture: WebGLTexture,
): void {
  gl.activeTexture(gl.TEXTURE0 + unit)
  gl.bindTexture(gl.TEXTURE_2D, texture)
}

/**
 * 一张覆盖全屏的着色器趟。`draw` 不含 useProgram——uniform 只作用于当前激活的
 * program，调用方必须自己先 use 再写 uniform。
 */
export interface Pass<N extends string> {
  program: WebGLProgram
  locations: Record<N, WebGLUniformLocation | null>
  draw: () => void
  dispose: () => void
}

export function createPass<N extends string>(
  gl: WebGL2RenderingContext,
  fragmentSource: string,
  names: readonly N[],
): Pass<N> {
  const program = compileProgram(gl, FULLSCREEN_VERT, fragmentSource)
  const locations = uniformLocations(gl, program, names)
  return {
    program,
    locations,
    draw: () => drawFullscreen(gl),
    dispose: () => gl.deleteProgram(program),
  }
}

/** 回到默认帧缓冲（画布）并按设备像素设视口。 */
export function bindScreen(gl: WebGL2RenderingContext, width: number, height: number): void {
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)
  gl.viewport(0, 0, width, height)
}
