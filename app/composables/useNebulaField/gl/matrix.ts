/** 列主序 4×4，可直接交给 `uniformMatrix4fv`。 */
export type Mat4 = Float32Array

/**
 * 对称透视投影，与 three 的 PerspectiveCamera 同一约定（NDC z 取 [-1,1]）。
 * fovDeg 为竖直视场角，aspect 为宽/高。
 */
export function perspective(fovDeg: number, aspect: number, near: number, far: number): Mat4 {
  const f = 1 / Math.tan((fovDeg * Math.PI) / 180 / 2)
  const out = new Float32Array(16)
  out[0] = f / aspect
  out[5] = f
  out[10] = (far + near) / (near - far)
  out[11] = -1
  out[14] = (2 * far * near) / (near - far)
  return out
}

/** 对象在世界中的位置与三轴缩放；本项目无旋转，姿态全部在着色器里算 */
export interface ObjectTransform {
  x: number
  y: number
  z: number
  scaleX: number
  scaleY: number
  scaleZ: number
}

/**
 * 视图与模型变换合并后的矩阵：对象先按三轴缩放、再平移到 (x, y, z)，
 * 相机位于 (0, 0, camDist) 望向 −Z。平移不被缩放放大，故直接写进第四列。
 */
export function modelView(transform: ObjectTransform, camDist: number): Mat4 {
  const out = new Float32Array(16)
  out[0] = transform.scaleX
  out[5] = transform.scaleY
  out[10] = transform.scaleZ
  out[12] = transform.x
  out[13] = transform.y
  out[14] = transform.z - camDist
  out[15] = 1
  return out
}
