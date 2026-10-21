import { CAM_DIST, TILT } from './constants'
import type { PointsLayer } from './gl/points-layer'
import { spinScale } from './intro'
import type { Placement, Viewport } from './layout'

/** time 驱动闪烁；spin 为累计自旋角；parX/parY 是缓动后的视差；intro 0=散点、1=巡航 */
export interface NebulaFrame {
  time: number
  spin: number
  parX: number
  parY: number
  intro: number
}

export interface NebulaLayers {
  sculpture: PointsLayer
  field: PointsLayer
}

/** 星场在深度方向上的铺展：中心压到 z=−1.5，沿视线拉开 4.5 个单位形成前后景深 */
const FIELD_DEPTH = 4.5
const FIELD_OFFSET_Z = -1.5

/**
 * 视口测量与雕塑定位的结果写进两层：变换、点尺寸折算与像素比。
 * 这些量只在 resize 时变，绘制路径因此只处理逐帧变化的部分。
 */
export function applyLayout(
  layers: NebulaLayers,
  viewport: Viewport,
  placement: Placement,
  dpr: number,
): void {
  const { sculpture, field } = layers
  const { scale } = placement

  sculpture.setTransform({
    x: placement.x,
    y: placement.y,
    z: 0,
    scaleX: scale,
    scaleY: scale,
    scaleZ: scale,
  })
  // 点尺寸不受对象缩放影响，需手动折算进 uniform
  sculpture.uniforms.uSizeScale = viewport.pxScale * scale
  sculpture.uniforms.uGlobalAlpha = placement.alpha

  field.setTransform({
    x: 0,
    y: 0,
    z: FIELD_OFFSET_Z,
    scaleX: viewport.halfW * 1.15,
    scaleY: viewport.halfH * 1.15,
    scaleZ: FIELD_DEPTH,
  })
  field.uniforms.uSizeScale = viewport.pxScale

  for (const layer of [sculpture, field]) {
    layer.uniforms.uPixelRatio = dpr
    layer.uniforms.uCamDist = CAM_DIST
  }
}

/**
 * 逐帧 uniform：时钟、入场进度、扫描位置与姿态。
 * `spinX` 由调用方按扫描时钟算好传入，本函数不做时间推演。
 */
export function applyFrame(layers: NebulaLayers, frame: NebulaFrame, scanX: number): void {
  const { sculpture, field } = layers
  const { time, spin, parX, parY, intro } = frame

  for (const layer of [sculpture, field]) {
    layer.uniforms.uTime = time
    layer.uniforms.uIntro = intro
    layer.uniforms.uScanX = scanX
  }
  // 雕塑自旋 + 完整视差；星场只做轻微视差倾斜，不累积自旋
  sculpture.uniforms.uRotY = spin * spinScale(intro) + parX
  sculpture.uniforms.uTiltX = TILT + parY
  field.uniforms.uRotY = parX * 0.25
  field.uniforms.uTiltX = TILT * 0.4 + parY * 0.15
}
