import { blurFragSource } from '../shaders/bloom'
import { gaussianKernel, mipSize, type MipSize } from './bloom-math'
import { bindTextureUnit, createPass, type Pass } from './pass'
import { bindColorTarget, createColorTarget, resizeColorTarget, type ColorTarget } from './target'

const BLUR_UNIFORMS = ['uColor', 'uInvSize', 'uDirection'] as const

type BlurPass = Pass<(typeof BLUR_UNIFORMS)[number]>

/** 一级模糊：一个 program 用两次（横、纵），加两张同尺寸目标 */
export interface BloomMip {
  /** 相对场景尺寸的减半次数，1 即半尺寸；resize 按它重算本级尺寸 */
  depth: number
  size: MipSize
  pass: BlurPass
  horizontal: ColorTarget
  vertical: ColorTarget
}

export function createMip(
  gl: WebGL2RenderingContext,
  radius: number,
  width: number,
  height: number,
  depth: number,
): BloomMip {
  const size = mipSize(width, height, depth)
  return {
    depth,
    size,
    pass: createPass(
      gl,
      blurFragSource(gaussianKernel(radius), Math.floor(radius / 2)),
      BLUR_UNIFORMS,
    ),
    horizontal: createColorTarget(gl, size.width, size.height, true),
    vertical: createColorTarget(gl, size.width, size.height, true),
  }
}

/** 先横后纵；降采样由「大纹理画进小目标」这一步顺带完成 */
export function blurMip(
  gl: WebGL2RenderingContext,
  mip: BloomMip,
  input: WebGLTexture,
): WebGLTexture {
  const { pass, size } = mip
  gl.useProgram(pass.program)
  gl.uniform1i(pass.locations.uColor, 0)
  gl.uniform2f(pass.locations.uInvSize, 1 / size.width, 1 / size.height)

  bindTextureUnit(gl, 0, input)
  gl.uniform2f(pass.locations.uDirection, 1, 0)
  bindColorTarget(gl, mip.horizontal)
  pass.draw()

  bindTextureUnit(gl, 0, mip.horizontal.texture)
  gl.uniform2f(pass.locations.uDirection, 0, 1)
  bindColorTarget(gl, mip.vertical)
  pass.draw()

  return mip.vertical.texture
}

export function resizeMip(
  gl: WebGL2RenderingContext,
  mip: BloomMip,
  width: number,
  height: number,
): void {
  const size = mipSize(width, height, mip.depth)
  resizeColorTarget(gl, mip.horizontal, size.width, size.height, true)
  resizeColorTarget(gl, mip.vertical, size.width, size.height, true)
  mip.size = size
}

export function disposeMip(mip: BloomMip): void {
  mip.pass.dispose()
  mip.horizontal.dispose()
  mip.vertical.dispose()
}
