import * as THREE from 'three'
import { CAM_DIST, FOV } from './constants'

export interface NebulaStage {
  renderer: THREE.WebGLRenderer
  scene: THREE.Scene
  camera: THREE.PerspectiveCamera
  /** 渲染分辨率倍率，窄屏压到 1.5 以保住帧预算 */
  dpr: number
}

/**
 * 建舞台：渲染器 + 场景 + 透视相机。WebGL2 不可用时返回 null，
 * 调用方因此可以静默降级，页面其余部分不受影响。
 */
export function createStage(canvas: HTMLCanvasElement, compact: boolean): NebulaStage | null {
  if (!document.createElement('canvas').getContext('webgl2')) return null

  const dpr = Math.min(window.devicePixelRatio || 1, compact ? 1.5 : 2)
  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: false, // 点精灵 + 泛光后期，MSAA 无用
    powerPreference: 'high-performance',
  })
  renderer.setPixelRatio(dpr)
  renderer.setClearColor(0x000000, 1)

  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(FOV, 1, 0.1, 40)
  camera.position.set(0, 0, CAM_DIST)

  return { renderer, scene, camera, dpr }
}
