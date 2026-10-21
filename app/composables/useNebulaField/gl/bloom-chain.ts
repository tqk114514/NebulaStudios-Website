import { BLOOM } from '../constants'
import { COMPOSITE_FRAG, HIGH_PASS_FRAG } from '../shaders/bloom'
import { BLOOM_KERNEL_SIZES, mipSize } from './bloom-math'
import { blurMip, createMip, disposeMip, resizeMip } from './bloom-mips'
import { bindTextureUnit, createPass } from './pass'
import { bindColorTarget, createColorTarget, resizeColorTarget } from './target'

/** 五级模糊结果占 1..5 号纹理单元；0 号留给当趟的输入 */
const MIP_UNITS = new Int32Array([1, 2, 3, 4, 5])

export interface BloomChain {
  /** 合成后的泛光色，线性空间；末趟把它加到场景上 */
  texture: WebGLTexture
  /** source 是场景的线性目标；宽高取其设备像素尺寸，各级尺寸在内部跟随重算 */
  render: (source: WebGLTexture, width: number, height: number) => void
  dispose: () => void
}

/**
 * 高通 → 逐级可分离高斯 → 加权合成，结构与 UnrealBloomPass 一致。
 * 只在浮点可写目标可用时构造：8bit 存不下大于 1 的亮度，链子跑通也是错的。
 */
export function createBloomChain(
  gl: WebGL2RenderingContext,
  width: number,
  height: number,
): BloomChain {
  const highPass = createPass(gl, HIGH_PASS_FRAG, [
    'uSource',
    'uThreshold',
    'uSmoothWidth',
  ] as const)
  const composite = createPass(gl, COMPOSITE_FRAG, ['uMips', 'uStrength', 'uRadius'] as const)
  // bright 与第一级模糊同尺寸；合成结果写回 bright，此时它已无人读取
  const brightSize = mipSize(width, height, 1)
  const bright = createColorTarget(gl, brightSize.width, brightSize.height, true)
  const mips = BLOOM_KERNEL_SIZES.map((radius, index) =>
    createMip(gl, radius, width, height, index + 1),
  )

  function setSize(nextWidth: number, nextHeight: number): void {
    const first = mipSize(nextWidth, nextHeight, 1)
    resizeColorTarget(gl, bright, first.width, first.height, true)
    for (const mip of mips) resizeMip(gl, mip, nextWidth, nextHeight)
  }

  function render(source: WebGLTexture, width: number, height: number): void {
    setSize(width, height)
    gl.disable(gl.BLEND)

    gl.useProgram(highPass.program)
    gl.uniform1i(highPass.locations.uSource, 0)
    gl.uniform1f(highPass.locations.uThreshold, BLOOM.threshold)
    gl.uniform1f(highPass.locations.uSmoothWidth, BLOOM.knee)
    bindTextureUnit(gl, 0, source)
    bindColorTarget(gl, bright)
    highPass.draw()

    let input = bright.texture
    for (const mip of mips) input = blurMip(gl, mip, input)

    gl.useProgram(composite.program)
    gl.uniform1iv(composite.locations.uMips, MIP_UNITS)
    mips.forEach((mip, index) => bindTextureUnit(gl, index + 1, mip.vertical.texture))
    gl.uniform1f(composite.locations.uStrength, BLOOM.strength)
    gl.uniform1f(composite.locations.uRadius, BLOOM.radius)
    bindColorTarget(gl, bright)
    composite.draw()
  }

  return {
    texture: bright.texture,
    render,
    dispose: () => {
      highPass.dispose()
      composite.dispose()
      bright.dispose()
      mips.forEach(disposeMip)
    },
  }
}
