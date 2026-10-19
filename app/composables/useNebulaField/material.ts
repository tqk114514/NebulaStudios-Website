import * as THREE from 'three'
import { CAM_DIST, SCAN_RANGE, TILT } from './constants'
import { NEBULA_FRAG, NEBULA_VERT } from './shaders'

/** 具名字段供调用方使用，索引签名满足 ShaderMaterial.uniforms 的类型要求 */
export interface NebulaUniforms extends Record<string, THREE.IUniform> {
  uTime: { value: number }
  uIntro: { value: number }
  uRotY: { value: number }
  uTiltX: { value: number }
  uScanX: { value: number }
  uPixelRatio: { value: number }
  uSizeScale: { value: number }
  uCamDist: { value: number }
  uGlobalAlpha: { value: number }
  uColorA: { value: THREE.Color }
  uColorB: { value: THREE.Color }
}

export interface NebulaMaterial {
  material: THREE.ShaderMaterial
  uniforms: NebulaUniforms
}

function readColor(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

/**
 * 点精灵材质。uniforms 与 material 一起返回，调用方因此拿到的是有类型的字段，
 * 不必对 material.uniforms 做索引断言。
 * depthTest/depthWrite 关闭 + 加法混合：绘制顺序无关紧要，星尘叠加自然变亮。
 */
export function createNebulaMaterial(): NebulaMaterial {
  const uniforms: NebulaUniforms = {
    uTime: { value: 0 },
    uIntro: { value: 0 },
    uRotY: { value: 0 },
    uTiltX: { value: TILT },
    uScanX: { value: -SCAN_RANGE },
    uPixelRatio: { value: 1 },
    uSizeScale: { value: 1 },
    uCamDist: { value: CAM_DIST },
    uGlobalAlpha: { value: 1 },
    uColorA: { value: new THREE.Color(readColor('--fg', '#f0ede8')) },
    uColorB: { value: new THREE.Color(readColor('--nebula-ring', '#cfd6e4')) },
  }

  const material = new THREE.ShaderMaterial({
    vertexShader: NEBULA_VERT,
    fragmentShader: NEBULA_FRAG,
    transparent: true,
    depthWrite: false,
    depthTest: false,
    blending: THREE.AdditiveBlending,
    uniforms,
  })

  return { material, uniforms }
}
