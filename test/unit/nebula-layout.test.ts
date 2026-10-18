import { describe, expect, it } from 'vitest'
import { measureViewport, placeSculpture } from '~/composables/useNebulaField/layout'

const HERO_1280X800 = { left: 0, top: 0, width: 1280, height: 800 }

describe('measureViewport', () => {
  it('按 45° FOV 与相机距离 7 折算 z=0 平面上的视锥半尺寸', () => {
    const viewport = measureViewport(1280, 800)

    expect(viewport.halfH).toBeCloseTo(2.8995, 3)
    expect(viewport.halfW).toBeCloseTo(4.6392, 3)
    expect(viewport.worldPerPx).toBeCloseTo(0.0072487, 6)
  })

  it('粒子放大系数取视口高 / 900 并夹在 0.85 与 1.35 之间', () => {
    expect(measureViewport(1280, 800).pxScale).toBeCloseTo(0.8889, 3)
    expect(measureViewport(1280, 400).pxScale).toBe(0.85)
    expect(measureViewport(3840, 2160).pxScale).toBe(1.35)
  })

  it('零尺寸视口不产生除零', () => {
    const viewport = measureViewport(0, 0)

    expect(Number.isFinite(viewport.worldPerPx)).toBe(true)
    expect(Number.isFinite(viewport.pxScale)).toBe(true)
  })
})

describe('placeSculpture', () => {
  it('锚点存在时按锚点中心投影，直径取锚点宽 ×1.7 与视口高 ×0.85 的较大者', () => {
    const anchor = { left: 900, top: 300, width: 300, height: 300 }
    const placement = placeSculpture(measureViewport(1280, 800), HERO_1280X800, anchor)

    // 680 = 800 × 0.85 大于 510 = 300 × 1.7，故取视口高一路
    expect(placement.scale).toBeCloseTo(1.2022, 3)
    expect(placement.alpha).toBe(1)
    expect(placement.y).toBeCloseTo(-0.3624, 3)
  })

  it('右缘出血超限时整体左移，使超出部分不超过 30% 环半径', () => {
    const anchor = { left: 1100, top: 250, width: 300, height: 300 }
    const viewport = measureViewport(1280, 800)
    const placement = placeSculpture(viewport, HERO_1280X800, anchor)

    const radiusPx = (placement.scale * 4.1) / 2 / viewport.worldPerPx
    const centerPx = (placement.x / viewport.halfW + 1) / 2 * 1280

    expect(centerPx + radiusPx).toBeLessThanOrEqual(1280 + 0.3 * radiusPx)
  })

  it('锚点隐藏时居中、放大到视口宽 95%、压暗到 0.8', () => {
    const viewport = measureViewport(1280, 800)
    const placement = placeSculpture(viewport, HERO_1280X800, null)

    expect(placement.x).toBe(0)
    expect(placement.y).toBeCloseTo(0.4639, 3)
    expect(placement.alpha).toBe(0.8)
    // 环直径不超过视口宽的 95%
    expect(placement.scale).toBeCloseTo((viewport.halfW * 1.9) / 4.1, 5)
  })

  it('锚点宽度为零与 null 走同一条退化分支', () => {
    const viewport = measureViewport(1280, 800)
    const collapsed = { left: 900, top: 300, width: 0, height: 0 }

    expect(placeSculpture(viewport, HERO_1280X800, collapsed)).toEqual(
      placeSculpture(viewport, HERO_1280X800, null),
    )
  })
})
