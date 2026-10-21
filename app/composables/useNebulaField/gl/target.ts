export interface ColorTarget {
  readonly framebuffer: WebGLFramebuffer
  readonly texture: WebGLTexture
  width: number
  height: number
  dispose: () => void
}

/**
 * 半浮点可写缓冲需要 EXT_color_buffer_float。缺失时线性 HDR 区间无处存放，
 * 泛光链的高通滤波也就没有输入——调用方据此走无泛光的 8bit 直出路径。
 */
export function supportsHdrTarget(gl: WebGL2RenderingContext): boolean {
  return gl.getExtension('EXT_color_buffer_float') !== null
}

/**
 * 线性色彩离屏目标。取样用线性过滤、边缘钳制、无 mip：泛光的降采样自己按
 * 2 倍步长取样，不依赖 mipmap 链。
 */
export function createColorTarget(
  gl: WebGL2RenderingContext,
  width: number,
  height: number,
  hdr: boolean,
): ColorTarget {
  const texture = gl.createTexture()
  if (!texture) throw new Error('WebGL2: createTexture 返回 null')
  const framebuffer = gl.createFramebuffer()
  if (!framebuffer) throw new Error('WebGL2: createFramebuffer 返回 null')

  const target: ColorTarget = {
    framebuffer,
    texture,
    width: 0,
    height: 0,
    dispose: () => {
      gl.deleteTexture(texture)
      gl.deleteFramebuffer(framebuffer)
    },
  }

  gl.bindTexture(gl.TEXTURE_2D, texture)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer)
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0)
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)

  allocate(gl, target, Math.max(1, Math.round(width)), Math.max(1, Math.round(height)), hdr)
  assertComplete(gl, target)
  return target
}

function allocate(
  gl: WebGL2RenderingContext,
  target: ColorTarget,
  width: number,
  height: number,
  hdr: boolean,
): void {
  gl.bindTexture(gl.TEXTURE_2D, target.texture)
  gl.texImage2D(
    gl.TEXTURE_2D,
    0,
    hdr ? gl.RGBA16F : gl.RGBA8,
    width,
    height,
    0,
    gl.RGBA,
    gl.HALF_FLOAT,
    null,
  )
  gl.bindTexture(gl.TEXTURE_2D, null)
  target.width = width
  target.height = height
}

/** 尺寸变化即重分配纹理内容，无需保留上一帧。 */
export function resizeColorTarget(
  gl: WebGL2RenderingContext,
  target: ColorTarget,
  width: number,
  height: number,
  hdr: boolean,
): void {
  const nextWidth = Math.max(1, Math.round(width))
  const nextHeight = Math.max(1, Math.round(height))
  if (nextWidth === target.width && nextHeight === target.height) return
  allocate(gl, target, nextWidth, nextHeight, hdr)
  assertComplete(gl, target)
}

/**
 * 附件是否可写。显存不足或格式不受支持都在这里现形——不查的话症状是
 * 一块静默的空白画布，只在真机上看得见。
 */
function assertComplete(gl: WebGL2RenderingContext, target: ColorTarget): void {
  gl.bindFramebuffer(gl.FRAMEBUFFER, target.framebuffer)
  const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER)
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)
  if (status !== gl.FRAMEBUFFER_COMPLETE) {
    throw new Error(`WebGL2 帧缓冲不完整：0x${status.toString(16)}`)
  }
}

export function bindColorTarget(gl: WebGL2RenderingContext, target: ColorTarget): void {
  gl.bindFramebuffer(gl.FRAMEBUFFER, target.framebuffer)
  gl.viewport(0, 0, target.width, target.height)
}
