import * as THREE from 'three'
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js'
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js'
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js'
import { ShaderPass } from 'three/addons/postprocessing/ShaderPass.js'
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js'
import { BLOOM, CAM_DIST, FOV, SCAN_PERIOD, SCAN_RANGE, STATIC_ROT_Y, STATIC_TIME, TILT } from './constants'
import { measureViewport, placeSculpture } from './layout'
import { createNebulaMaterial } from './material'
import { buildSculpture } from './sculpture'
import { DITHER_SHADER } from './shaders'
import { buildStarField } from './star-field'

export interface NebulaScene {
  /** 视口变化后重算尺寸与雕塑布局；静态模式下顺带重绘一帧 */
  resize: () => void
  /** 渲染一帧。rotY 为累计自旋，parX/parY 为缓动后的鼠标视差 */
  render: (time: number, rotY: number, parX: number, parY: number) => void
  dispose: () => void
}

interface DitherUniforms {
  uTime: { value: number }
}

function readBg(): string {
  return getComputedStyle(document.documentElement).getPropertyValue('--bg').trim() || '#0d0d0d'
}

export function createNebulaScene(
  canvas: HTMLCanvasElement,
  hero: HTMLElement,
  anchor: HTMLElement | null,
  compact: boolean,
  reduced: boolean,
): NebulaScene | null {
  // WebGL2 不可用时返回 null，由调用方静默降级：页面其余部分不受影响
  if (!document.createElement('canvas').getContext('webgl2')) return null

  const dpr = Math.min(window.devicePixelRatio || 1, compact ? 1.5 : 2)

  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: false, // 点精灵 + 泛光后期，MSAA 无用
    powerPreference: 'high-performance',
  })
  renderer.setPixelRatio(dpr)
  // 清屏纯黑：0 是 OETF 编码的不动点，不会被二次提亮；底色由 bgMesh 承载
  renderer.setClearColor(0x000000, 1)

  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(FOV, 1, 0.1, 40)
  camera.position.set(0, 0, CAM_DIST)

  // 背景板不能走 GL 清屏色：three 的清屏路径假设直连 sRGB 帧缓冲，会把线性清屏色按
  // OETF 预编码，经 composer 的线性 RT + OutputPass 后等于被二次提亮（实测 --bg 显示成
  // #404040，整个 hero 蒙一层白）。材质色走正常色彩管理，由 OutputPass 一次性转回。
  const bgGeometry = new THREE.PlaneGeometry(2, 2)
  const bgMaterial = new THREE.MeshBasicMaterial({ color: readBg() })
  bgMaterial.depthTest = false
  bgMaterial.depthWrite = false
  const bgMesh = new THREE.Mesh(bgGeometry, bgMaterial)
  bgMesh.position.z = -8 // 相机 z=7，最远粒子 z≈-6，背景板垫底
  bgMesh.scale.set(60, 60, 1) // 覆盖任何视口下的整个视锥截面
  bgMesh.renderOrder = -1
  bgMesh.frustumCulled = false
  scene.add(bgMesh)

  const sculptureGeometry = buildSculpture(compact).geometry()
  const fieldGeometry = buildStarField(compact).geometry()
  const sculpture = createNebulaMaterial()
  const field = createNebulaMaterial()
  const sculpturePoints = new THREE.Points(sculptureGeometry, sculpture.material)
  const fieldPoints = new THREE.Points(fieldGeometry, field.material)
  // 星环位置由着色器位移，关掉视锥剔除防止被错误裁掉
  sculpturePoints.frustumCulled = false
  fieldPoints.frustumCulled = false
  scene.add(sculpturePoints, fieldPoints)

  const composer = new EffectComposer(renderer)
  composer.setPixelRatio(dpr)
  composer.addPass(new RenderPass(scene, camera))
  const bloom = new UnrealBloomPass(new THREE.Vector2(1, 1), BLOOM.strength, BLOOM.radius, BLOOM.threshold)
  // 软化高通滤波的膝宽：默认 0.01 的硬截止经模糊链后就是同心圈层的源头
  const highPass = bloom.highPassUniforms as unknown as { smoothWidth: { value: number } }
  highPass.smoothWidth.value = BLOOM.knee
  composer.addPass(bloom)
  composer.addPass(new OutputPass())
  const dither = new ShaderPass(DITHER_SHADER)
  const ditherUniforms = dither.uniforms as unknown as DitherUniforms
  composer.addPass(dither)

  function render(time: number, rotY: number, parX: number, parY: number) {
    const scanX = -SCAN_RANGE + ((time % SCAN_PERIOD) / SCAN_PERIOD) * SCAN_RANGE * 2
    // 雕塑自旋 + 完整视差；星场只做轻微视差倾斜，不累积自旋
    sculpture.uniforms.uRotY.value = rotY + parX
    sculpture.uniforms.uTiltX.value = TILT + parY
    field.uniforms.uRotY.value = parX * 0.25
    field.uniforms.uTiltX.value = TILT * 0.4 + parY * 0.15
    for (const uniforms of [sculpture.uniforms, field.uniforms]) {
      uniforms.uTime.value = time
      uniforms.uScanX.value = scanX
    }
    ditherUniforms.uTime.value = time
    composer.render()
  }

  function resize() {
    const rect = hero.getBoundingClientRect()
    const w = Math.max(1, rect.width)
    const h = Math.max(1, rect.height)
    renderer.setSize(w, h, false)
    composer.setSize(w, h)
    camera.aspect = w / h
    camera.updateProjectionMatrix()

    const viewport = measureViewport(w, h)
    const placement = placeSculpture(viewport, rect, anchor?.getBoundingClientRect() ?? null)

    sculpturePoints.position.set(placement.x, placement.y, 0)
    sculpturePoints.scale.setScalar(placement.scale)
    // 点尺寸不受对象 scale 影响，需手动折算进 uniform
    sculpture.uniforms.uSizeScale.value = viewport.pxScale * placement.scale
    sculpture.uniforms.uGlobalAlpha.value = placement.alpha
    field.uniforms.uSizeScale.value = viewport.pxScale
    for (const uniforms of [sculpture.uniforms, field.uniforms]) {
      uniforms.uPixelRatio.value = dpr
      uniforms.uCamDist.value = CAM_DIST
    }

    // 背景星场拉伸铺满整个视锥，带前后景深
    fieldPoints.scale.set(viewport.halfW * 1.15, viewport.halfH * 1.15, 4.5)
    fieldPoints.position.set(0, 0, -1.5)

    if (reduced) render(STATIC_TIME, STATIC_ROT_Y, 0, 0)
  }

  function dispose() {
    composer.dispose()
    renderer.dispose()
    renderer.forceContextLoss()
    sculptureGeometry.dispose()
    fieldGeometry.dispose()
    bgGeometry.dispose()
    sculpture.material.dispose()
    field.material.dispose()
    bgMaterial.dispose()
  }

  resize()
  return { resize, render, dispose }
}
