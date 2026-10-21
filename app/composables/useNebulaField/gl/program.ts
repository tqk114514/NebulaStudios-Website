/** 编译 + 链接。着色器编译失败在驱动侧是真实可能的（扩展缺失、精度限制），故不静默降级。 */
export function compileProgram(
  gl: WebGL2RenderingContext,
  vertexSource: string,
  fragmentSource: string,
): WebGLProgram {
  const vertex = compile(gl, gl.VERTEX_SHADER, vertexSource)
  const fragment = compile(gl, gl.FRAGMENT_SHADER, fragmentSource)

  const program = gl.createProgram()
  if (!program) throw new Error('WebGL2: createProgram 返回 null')
  gl.attachShader(program, vertex)
  gl.attachShader(program, fragment)
  gl.linkProgram(program)
  // 链接完成后 shader 对象不再参与绘制，立即释放
  gl.deleteShader(vertex)
  gl.deleteShader(fragment)

  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    const log = gl.getProgramInfoLog(program) ?? '无日志'
    gl.deleteProgram(program)
    throw new Error(`WebGL2 链接失败：${log}`)
  }
  return program
}

function compile(gl: WebGL2RenderingContext, type: number, source: string): WebGLShader {
  const shader = gl.createShader(type)
  if (!shader) throw new Error('WebGL2: createShader 返回 null')
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    const log = gl.getShaderInfoLog(shader) ?? '无日志'
    const label = type === gl.VERTEX_SHADER ? '顶点' : '片元'
    gl.deleteShader(shader)
    throw new Error(`WebGL2 ${label}着色器编译失败：${log}`)
  }
  return shader
}

/**
 * 一次性查出全部 uniform 位置。名字必须与着色器里声明的一致：
 * 未声明或未使用的 uniform 会被着色器优化掉，取到 null，写入时静默丢弃。
 */
export function uniformLocations<N extends string>(
  gl: WebGL2RenderingContext,
  program: WebGLProgram,
  names: readonly N[],
): Record<N, WebGLUniformLocation | null> {
  return Object.fromEntries(
    names.map((name) => [name, gl.getUniformLocation(program, name)]),
  ) as Record<N, WebGLUniformLocation | null>
}
