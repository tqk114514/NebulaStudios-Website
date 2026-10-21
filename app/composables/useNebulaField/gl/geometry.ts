export type PointField =
  | 'position'
  | 'aScatter'
  | 'aDelay'
  | 'aKind'
  | 'aParams'
  | 'aTilt'
  | 'aSize'
  | 'aSeed'
  | 'aMix'
  | 'aAlpha'

/**
 * 顶点属性的顺序即 GLSL 里的 `layout(location = …)` 序号，两处必须逐项对应。
 * 交错布局只在这里声明一次，构建器与 VAO 都从它取偏移。
 */
const POINT_FIELDS: readonly { name: PointField; size: number }[] = [
  { name: 'position', size: 3 },
  { name: 'aScatter', size: 3 },
  { name: 'aDelay', size: 1 },
  { name: 'aKind', size: 1 },
  { name: 'aParams', size: 3 },
  { name: 'aTilt', size: 1 },
  { name: 'aSize', size: 1 },
  { name: 'aSeed', size: 2 },
  { name: 'aMix', size: 1 },
  { name: 'aAlpha', size: 1 },
]

/** 每顶点浮点数：17 */
const POINT_STRIDE = POINT_FIELDS.reduce((total, f) => total + f.size, 0)

/** GL 的 stride 与 offset 一律按字节计 */
const STRIDE_BYTES = POINT_STRIDE * 4

export type PointArrays = Record<PointField, number[]>

export interface PointGeometry {
  readonly vao: WebGLVertexArrayObject
  readonly count: number
  dispose: () => void
}

/**
 * 把按字段分组的数组交错成单条 STATIC_DRAW 缓冲并配好 VAO。
 * 粒子只在初始化时生成，运行期不更新顶点，故不需要 bufferSubData 路径。
 */
export function createPointGeometry(
  gl: WebGL2RenderingContext,
  arrays: PointArrays,
  count: number,
): PointGeometry {
  const data = new Float32Array(count * POINT_STRIDE)
  const cursors = Object.fromEntries(POINT_FIELDS.map((f) => [f.name, 0])) as Record<
    PointField,
    number
  >

  for (let vertex = 0; vertex < count; vertex++) {
    let offset = vertex * POINT_STRIDE
    for (const field of POINT_FIELDS) {
      const from = cursors[field.name]
      data.set(arrays[field.name].slice(from, from + field.size), offset)
      cursors[field.name] = from + field.size
      offset += field.size
    }
  }

  const buffer = gl.createBuffer()
  if (!buffer) throw new Error('WebGL2: createBuffer 返回 null')
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
  gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW)

  const vao = gl.createVertexArray()
  if (!vao) throw new Error('WebGL2: createVertexArray 返回 null')
  gl.bindVertexArray(vao)
  POINT_FIELDS.forEach((field, location) => {
    gl.enableVertexAttribArray(location)
    gl.vertexAttribPointer(
      location,
      field.size,
      gl.FLOAT,
      false,
      STRIDE_BYTES,
      byteOffset(location),
    )
  })
  gl.bindVertexArray(null)
  gl.bindBuffer(gl.ARRAY_BUFFER, null)

  return {
    vao,
    count,
    dispose: () => {
      gl.deleteBuffer(buffer)
      gl.deleteVertexArray(vao)
    },
  }
}

/** 第 location 个属性在本顶点记录内的字节偏移。 */
function byteOffset(location: number): number {
  return POINT_FIELDS.slice(0, location).reduce((total, f) => total + f.size, 0) * 4
}
