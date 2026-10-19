import * as THREE from 'three'
import { CAM_DIST, SCAN_PERIOD, SCAN_RANGE, STATIC_ROT_Y, STATIC_TIME, TILT } from './constants'
import { createBackground } from './background'
import { createScanClock, spinScale } from './intro'
import { measureViewport, placeSculpture } from './layout'
import { createNebulaMaterial } from './material'
import { createPostprocessing } from './postprocessing'
import { buildSculpture } from './sculpture'
import { createStage } from './stage'
import { buildStarField } from './star-field'

/** time 驱动闪烁；spin 为累计自旋角；parX/parY 是缓动后的视差；intro 0=散点、1=巡航 */
interface NebulaFrame {
  time: number
  spin: number
  parX: number
  parY: number
  intro: number
}

export interface NebulaScene {
  /** 视口变化后重算尺寸与雕塑布局；静态模式下顺带重绘一帧 */
  resize: () => void
  render: (frame: NebulaFrame) => void
  dispose: () => void
}

export function createNebulaScene(
  canvas: HTMLCanvasElement,
  hero: HTMLElement,
  anchor: HTMLElement | null,
  compact: boolean,
  reduced: boolean,
): NebulaScene | null {
  const stage = createStage(canvas, compact)
  if (!stage) return null
  const { renderer, scene, camera, dpr } = stage

  const background = createBackground()
  scene.add(background)

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

  const postprocessing = createPostprocessing(renderer, scene, camera)
  const scanClock = createScanClock()
  let prevTime = 0

  function render(frame: NebulaFrame) {
    const { time, spin, parX, parY, intro } = frame
    const scanT = scanClock(time - prevTime, intro)
    prevTime = time
    const scanX = -SCAN_RANGE + ((scanT % SCAN_PERIOD) / SCAN_PERIOD) * SCAN_RANGE * 2
    // 雕塑自旋 + 完整视差；星场只做轻微视差倾斜，不累积自旋
    sculpture.uniforms.uRotY.value = spin * spinScale(intro) + parX
    sculpture.uniforms.uTiltX.value = TILT + parY
    field.uniforms.uRotY.value = parX * 0.25
    field.uniforms.uTiltX.value = TILT * 0.4 + parY * 0.15
    for (const uniforms of [sculpture.uniforms, field.uniforms]) {
      uniforms.uTime.value = time
      uniforms.uIntro.value = intro
      uniforms.uScanX.value = scanX
    }
    postprocessing.setTime(time)
    postprocessing.render()
  }

  function resize() {
    const rect = hero.getBoundingClientRect()
    const w = Math.max(1, rect.width)
    const h = Math.max(1, rect.height)
    renderer.setSize(w, h, false)
    postprocessing.setSize(w, h)
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

    if (reduced) render({ time: STATIC_TIME, spin: STATIC_ROT_Y, parX: 0, parY: 0, intro: 1 })
  }

  function dispose() {
    postprocessing.dispose()
    renderer.dispose()
    renderer.forceContextLoss()
    sculptureGeometry.dispose()
    fieldGeometry.dispose()
    background.geometry.dispose()
    ;(background.material as THREE.Material).dispose()
    sculpture.material.dispose()
    field.material.dispose()
  }

  resize()
  return { resize, render, dispose }
}
