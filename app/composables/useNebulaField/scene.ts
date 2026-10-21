import { SCAN_PERIOD, SCAN_RANGE, STATIC_ROT_Y, STATIC_TIME } from './constants'
import { createBackground } from './background'
import { createBloomChain } from './gl/bloom-chain'
import { createPointGeometry } from './gl/geometry'
import { createPointsLayer } from './gl/points-layer'
import {
  bindColorTarget,
  createColorTarget,
  resizeColorTarget,
  supportsHdrTarget,
} from './gl/target'
import { createScanClock } from './intro'
import { measureViewport, placeSculpture } from './layout'
import { createNebulaProgram, createNebulaUniforms } from './nebula-program'
import { applyFrame, applyLayout, type NebulaFrame } from './nebula-values'
import { createPostprocessing } from './postprocessing'
import { buildSculpture } from './sculpture'
import { createStage, type NebulaStage } from './stage'
import { buildStarField } from './star-field'

export interface NebulaScene {
  /** 视口变化后重算尺寸与雕塑布局；静态模式下顺带重绘一帧 */
  resize: () => void
  render: (frame: NebulaFrame) => void
  dispose: () => void
}

/** WebGL2 上下文取不到时返回 null，调用方据此静默降级。 */
export function createNebulaScene(
  canvas: HTMLCanvasElement,
  hero: HTMLElement,
  anchor: HTMLElement | null,
  compact: boolean,
  reduced: boolean,
): NebulaScene | null {
  const stage = createStage(canvas, compact)
  return stage ? buildScene(stage, hero, anchor, compact, reduced) : null
}

function buildScene(
  stage: NebulaStage,
  hero: HTMLElement,
  anchor: HTMLElement | null,
  compact: boolean,
  reduced: boolean,
): NebulaScene {
  const { gl } = stage
  const hdr = supportsHdrTarget(gl)
  const program = createNebulaProgram(gl)
  const sculptureSource = buildSculpture(compact)
  const fieldSource = buildStarField(compact)
  const layers = {
    sculpture: createPointsLayer(
      gl,
      program,
      createPointGeometry(gl, sculptureSource.attributes, sculptureSource.count),
      createNebulaUniforms(),
    ),
    field: createPointsLayer(
      gl,
      program,
      createPointGeometry(gl, fieldSource.attributes, fieldSource.count),
      createNebulaUniforms(),
    ),
  }
  const background = createBackground(gl)
  const linear = createColorTarget(gl, stage.width, stage.height, hdr)
  // 泛光需要存 >1 的亮度，无浮点可写目标时整条链不建，末趟加零
  const bloom = hdr ? createBloomChain(gl, stage.width, stage.height) : null
  const postprocessing = createPostprocessing(gl, bloom?.texture ?? null)
  const scanClock = createScanClock()
  let prevTime = 0

  function render(frame: NebulaFrame) {
    const scanT = scanClock(frame.time - prevTime, frame.intro)
    prevTime = frame.time
    const scanRange = (scanT % SCAN_PERIOD) / SCAN_PERIOD
    applyFrame(layers, frame, -SCAN_RANGE + scanRange * SCAN_RANGE * 2)

    bindColorTarget(gl, linear)
    background.draw()
    layers.sculpture.draw(stage.projection)
    layers.field.draw(stage.projection)
    bloom?.render(linear.texture, stage.width, stage.height)
    postprocessing.setTime(frame.time)
    postprocessing.render(linear, stage.width, stage.height)
  }

  function resize() {
    const rect = hero.getBoundingClientRect()
    if (stage.resize(rect.width, rect.height)) {
      resizeColorTarget(gl, linear, stage.width, stage.height, hdr)
    }
    const viewport = measureViewport(rect.width, rect.height)
    applyLayout(
      layers,
      viewport,
      placeSculpture(viewport, rect, anchor?.getBoundingClientRect() ?? null),
      stage.dpr,
    )
    if (reduced) render({ time: STATIC_TIME, spin: STATIC_ROT_Y, parX: 0, parY: 0, intro: 1 })
  }

  function dispose() {
    postprocessing.dispose()
    bloom?.dispose()
    background.dispose()
    layers.sculpture.dispose()
    layers.field.dispose()
    linear.dispose()
    gl.deleteProgram(program)
    stage.dispose()
  }

  resize()
  return { resize, render, dispose }
}
