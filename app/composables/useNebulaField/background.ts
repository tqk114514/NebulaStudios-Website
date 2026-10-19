import * as THREE from 'three'

/**
 * 背景板承载页面底色，不能走 GL 清屏色：three 的清屏路径假设直连 sRGB 帧缓冲，
 * 会把线性清屏色按 OETF 预编码，经 composer 的线性 RT + OutputPass 后等于被二次提亮
 * （实测 --bg 显示成 #404040，整个 hero 蒙一层白）。材质色走正常色彩管理，
 * 由 OutputPass 一次性转回。清屏保持纯黑——0 是任何编码的不动点。
 */
export function createBackground(): THREE.Mesh {
  const color = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim() || '#0d0d0d'
  const material = new THREE.MeshBasicMaterial({ color })
  material.depthTest = false
  material.depthWrite = false

  const mesh = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material)
  mesh.position.z = -8 // 相机 z=7，最远粒子 z≈-6，背景板垫底
  mesh.scale.set(60, 60, 1) // 覆盖任何视口下的整个视锥截面
  mesh.renderOrder = -1
  mesh.frustumCulled = false
  return mesh
}
