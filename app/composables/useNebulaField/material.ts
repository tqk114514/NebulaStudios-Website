import * as THREE from 'three'
import { CAM_DIST, SCAN_RANGE, TILT } from './constants'
import { NEBULA_FRAG, NEBULA_VERT } from './shaders'

function readColor(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

/**
 * 点精灵材质。颜色取自 CSS 令牌而非写死，暖白随 --fg 变化，冷白是星环专用色。
 * depthTest/depthWrite 关闭 + 加法混合：绘制顺序因此无关紧要，
 * 星尘叠加自然变亮，与 Bloom 相性最好。
 */
export function createNebulaMaterial(): THREE.ShaderMaterial {
  return new THREE.ShaderMaterial({
    vertexShader: NEBULA_VERT,
    fragmentShader: NEBULA_FRAG,
    transparent: true,
    depthWrite: false,
    depthTest: false,
    blending: THREE.AdditiveBlending,
    uniforms: {
      uTime: { value: 0 },
      uRotY: { value: 0 },
      uTiltX: { value: TILT },
      uScanX: { value: -SCAN_RANGE },
      uPixelRatio: { value: 1 },
      uSizeScale: { value: 1 },
      uCamDist: { value: CAM_DIST },
      uGlobalAlpha: { value: 1 },
      uColorA: { value: new THREE.Color(readColor('--fg', '#f0ede8')) },
      uColorB: { value: new THREE.Color(readColor('--nebula-ring', '#cfd6e4')) },
    },
  })
}
