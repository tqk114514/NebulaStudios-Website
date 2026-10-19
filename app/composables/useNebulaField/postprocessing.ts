import * as THREE from 'three'
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js'
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js'
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js'
import { ShaderPass } from 'three/addons/postprocessing/ShaderPass.js'
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js'
import { BLOOM } from './constants'
import { DITHER_SHADER } from './dither-shader'

export interface Postprocessing {
  setSize: (width: number, height: number) => void
  /** 写入本帧时间，驱动抖动的噪声相位 */
  setTime: (time: number) => void
  render: () => void
  dispose: () => void
}

/** 后期链：RenderPass → UnrealBloom → OutputPass（线性→sRGB）→ 抖动 */
export function createPostprocessing(
  renderer: THREE.WebGLRenderer,
  scene: THREE.Scene,
  camera: THREE.Camera,
): Postprocessing {
  const composer = new EffectComposer(renderer)
  composer.addPass(new RenderPass(scene, camera))

  const bloom = new UnrealBloomPass(
    new THREE.Vector2(1, 1),
    BLOOM.strength,
    BLOOM.radius,
    BLOOM.threshold,
  )
  // 软化高通滤波的膝宽：默认 0.01 的硬截止经模糊链后就是同心圈层的源头
  const highPass = bloom.highPassUniforms as unknown as { smoothWidth: { value: number } }
  highPass.smoothWidth.value = BLOOM.knee
  composer.addPass(bloom)

  composer.addPass(new OutputPass())
  const dither = new ShaderPass(DITHER_SHADER)
  const ditherUniforms = dither.uniforms as unknown as { uTime: { value: number } }
  composer.addPass(dither)

  return {
    setSize: (width, height) => composer.setSize(width, height),
    setTime: (time) => {
      ditherUniforms.uTime.value = time
    },
    render: () => composer.render(),
    dispose: () => composer.dispose(),
  }
}
