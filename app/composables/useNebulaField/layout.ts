import { CAM_DIST, FOV, RING_SPAN } from './constants'

export interface Viewport {
  /** z=0 平面上视锥半高（世界单位） */
  halfH: number
  /** z=0 平面上视锥半宽（世界单位） */
  halfW: number
  /** z=0 平面上每 CSS 像素对应的世界单位 */
  worldPerPx: number
  /** 大屏适当放大粒子的系数 */
  pxScale: number
}

export interface Placement {
  x: number
  y: number
  scale: number
  alpha: number
}

export function measureViewport(width: number, height: number): Viewport {
  const w = Math.max(1, width)
  const h = Math.max(1, height)
  const halfH = Math.tan((FOV * Math.PI) / 180 / 2) * CAM_DIST
  const halfW = halfH * (w / h)
  return {
    halfH,
    halfW,
    worldPerPx: (halfW * 2) / w,
    pxScale: Math.min(1.35, Math.max(0.85, h / 900)),
  }
}

/** 视口坐标系下的矩形，DOMRect 的可读子集 */
export interface Rect {
  left: number
  top: number
  width: number
  height: number
}

/**
 * 雕塑定位：把右侧锚点的中心投影为 z=0 平面上的世界坐标（坐标以 hero 左上角为原点）。
 * 星环外沿直径取「锚点宽 ×1.7」与「视口高 ×0.85」的较大者，使其随屏幕撑满 hero。
 * 锚点不存在或宽度为 0（窄屏隐藏）时居中、取视口宽 95%、整体压暗到 0.8。
 */
export function placeSculpture(
  viewport: Viewport,
  hero: Rect,
  anchor: Rect | null,
): Placement {
  const w = Math.max(1, hero.width)
  const h = Math.max(1, hero.height)
  const { halfW, halfH, worldPerPx } = viewport

  let cx: number
  let cy: number
  let scale: number
  let alpha = 1

  if (anchor && anchor.width > 1) {
    cx = anchor.left + anchor.width / 2 - hero.left
    cy = anchor.top + anchor.height / 2 - hero.top
    scale = (Math.max(anchor.width * 1.7, h * 0.85) * worldPerPx) / RING_SPAN
  } else {
    cx = w / 2
    cy = h * 0.42
    scale = (w * 0.95 * worldPerPx) / RING_SPAN
    alpha = 0.8
  }

  // 环直径不超过视口宽的 95%
  scale = Math.min(scale, (halfW * 1.9) / RING_SPAN)
  // 右缘出血收束：环最右超出视口的部分不超过 30% 环半径，超出则整体左移
  cx = Math.min(cx, w - 0.7 * ((RING_SPAN / 2) * (scale / worldPerPx)))

  return {
    x: ((cx / w) * 2 - 1) * halfW,
    y: (1 - (cy / h) * 2) * halfH,
    scale,
    alpha,
  }
}
