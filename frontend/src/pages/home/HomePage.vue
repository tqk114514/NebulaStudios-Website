<script setup lang="ts">
// 首页（迁移自 modules/home/pages/index.html + home.ts）。
// 自定义光标、Three.js 真 3D 粒子星云（铺满 hero）、ticker 无限滚动、reveal 滚动动画。
import { ref, onMounted, onBeforeUnmount } from 'vue'
import * as THREE from 'three'
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js'
import { ShaderPass } from 'three/addons/postprocessing/ShaderPass.js'

const isMobile = /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent)

// ---- 自定义光标 ----
const cursor = ref<HTMLDivElement | null>(null)
const ring = ref<HTMLDivElement | null>(null)
let mouseX = 0
let mouseY = 0
let ringX = 0
let ringY = 0
let cursorAnimId: number | null = null
const cursorVisible = ref(false)
let cursorHooks = false

function animateRing() {
  ringX += (mouseX - ringX) * 0.12
  ringY += (mouseY - ringY) * 0.12
  if (ring.value) {
    ring.value.style.left = `${ringX}px`
    ring.value.style.top = `${ringY}px`
  }
  cursorAnimId = requestAnimationFrame(animateRing)
}

function onCursorMove(e: MouseEvent) {
  mouseX = e.clientX
  mouseY = e.clientY
  if (cursor.value) {
    cursor.value.style.left = `${mouseX}px`
    cursor.value.style.top = `${mouseY}px`
    if (!cursorVisible.value) {
      cursorVisible.value = true
      cursor.value.classList.add('visible')
      ring.value?.classList.add('visible')
    }
  }
}

function setupCursor() {
  if (isMobile) return
  document.body.classList.add('has-custom-cursor')
  document.addEventListener('mousemove', onCursorMove, { passive: true })
  cursorAnimId = requestAnimationFrame(animateRing)
  cursorHooks = true
}

// ---- Three.js 真 3D 粒子星云 ----
// 画布铺满整个 hero：品牌 Logo 粒子雕塑（按到边缘的距离生成倒角厚度）+ 三重粒子星环
// + 雕塑星尘与全视锥背景星场。透视相机 + 自定义 ShaderMaterial 点精灵
// （轨道运动 / 闪烁 / 辉光扫描 / 透视点尺寸全部在 GPU 顶点着色器计算），
// EffectComposer + UnrealBloomPass 泛光后期——唯一裁切边界是视口本身，不再有方框。
// 雕塑位置由右侧锚点元素（老版方块的位置）投影定位；≤900px 锚点隐藏时居中缩小压暗。
// prefers-reduced-motion：静态渲染一帧；WebGL2 不可用：静默跳过 3D（页面其余部分不受影响）。

const NEBULA_FOV = 45 // 透视相机 FOV
const NEBULA_CAM_DIST = 7 // 相机到雕塑中心的距离
const NEBULA_SPIN = 0.22 // 自旋角速度（rad/s）
const NEBULA_TILT = 0.3 // 基础俯视角（rad）
const SCAN_PERIOD = 6.5 // Logo 辉光扫描周期（s）
const SCAN_RANGE = 1.35 // 扫描覆盖的 Logo 本地 X 范围
// 泛光预算：UnrealBloom 的模糊链最低降到 ~1/32 屏幕分辨率，任何"大块中亮"区域
// （如整个 Logo 叠加面）在最低级会被摊成全屏均匀灰加回画面——近黑底色上
// 哪怕 +0.03 线性亮度都读作"整个 hero 蒙了一层白"。因此阈值必须高到只有真正的
// 热点（扫描波、粒子重叠核）才进泛光链，主体亮度一律压在阈值之下保持锐利。
// knee：高通道滤波的平滑膝宽（默认 0.01 的硬截止会在模糊后产生同心圈层伪影）；
// radius：拉高让 5 级模糊的光晕互相混合，削弱台阶感。
const BLOOM = { strength: 0.35, radius: 0.7, threshold: 0.8, knee: 0.55 }

const heroEl = ref<HTMLElement | null>(null)
const nebulaCanvas = ref<HTMLCanvasElement | null>(null)
const nebulaAnchor = ref<HTMLDivElement | null>(null)
let nebulaAnimId: number | null = null
let nebulaCleanup: (() => void) | null = null

// Logo 的两个 path（与 src/assets/favicon.svg 保持一致）
const LOGO_PATHS = ['M56 48H96L144 96L112 128L96 112V208H56V48Z', 'M200 208H160L112 160L144 128L160 144V48H200V208Z']

function sampleLogoParticles(count: number): { x: number; y: number; t: number }[] {
  // 把 Logo path 画到离屏画布，读像素采样出轮廓内坐标；
  // t = 5x5 邻域实心率（1=深处内部，0=贴边），用于生成倒角厚度——
  // 中心厚、边缘薄，旋转到侧面时读作一块有倒角的立体板而非均匀薄片。
  const S = 256
  const off = document.createElement('canvas')
  off.width = S
  off.height = S
  const ctx = off.getContext('2d', { willReadFrequently: true })
  if (!ctx) return []
  ctx.fillStyle = '#fff'
  for (const d of LOGO_PATHS) ctx.fill(new Path2D(d))
  const data = ctx.getImageData(0, 0, S, S).data
  const filled = (px: number, py: number) =>
    px >= 0 && py >= 0 && px < S && py < S && data[(py * S + px) * 4 + 3] > 128
  const inside: { x: number; y: number; t: number }[] = []
  for (let py = 0; py < S; py += 2) {
    for (let px = 0; px < S; px += 2) {
      if (!filled(px, py)) continue
      let n = 0
      for (let dy = -4; dy <= 4; dy += 4) {
        for (let dx = -4; dx <= 4; dx += 4) {
          if (filled(px + dx, py + dy)) n++
        }
      }
      inside.push({ x: px, y: py, t: n / 9 })
    }
  }
  if (inside.length === 0) return []
  const pts: { x: number; y: number; t: number }[] = []
  for (let i = 0; i < count; i++) {
    const p = inside[Math.floor(Math.random() * inside.length)]
    // 以 128 为中心归一化到 [-1, 1]，Y 翻转（屏幕坐标向下）
    pts.push({ x: (p.x - 128) / 100, y: -(p.y - 128) / 100, t: p.t })
  }
  return pts
}

// 粒子属性构建器：一次性填充全部顶点属性；
// 星环粒子的轨道参数（半径/初始角/角速度）写进 aParams，运动在 GPU 中计算。
class NebulaBuilder {
  private pos: number[] = []
  private kind: number[] = []
  private params: number[] = []
  private tilt: number[] = []
  private size: number[] = []
  private seed: number[] = []
  private mix: number[] = []
  private alpha: number[] = []

  add(
    p: readonly [number, number, number],
    kind: 0 | 1 | 2, // 0=Logo 1=星环 2=星尘
    size: number,
    alpha: number,
    mix: number, // 颜色混合：0=暖白(#f0ede8) → 1=冷白(#cfd6e4)
    seed: readonly [number, number], // 闪烁相位 / 速度
    params: readonly [number, number, number] = [0, 0, 0],
    tilt = 0,
  ) {
    this.pos.push(p[0], p[1], p[2])
    this.kind.push(kind)
    this.params.push(params[0], params[1], params[2])
    this.tilt.push(tilt)
    this.size.push(size)
    this.seed.push(seed[0], seed[1])
    this.mix.push(mix)
    this.alpha.push(alpha)
  }

  geometry(): THREE.BufferGeometry {
    const f = (a: number[]) => new THREE.BufferAttribute(new Float32Array(a), 1)
    const g = new THREE.BufferGeometry()
    g.setAttribute('position', new THREE.BufferAttribute(new Float32Array(this.pos), 3))
    g.setAttribute('aKind', f(this.kind))
    g.setAttribute('aParams', new THREE.BufferAttribute(new Float32Array(this.params), 3))
    g.setAttribute('aTilt', f(this.tilt))
    g.setAttribute('aSize', f(this.size))
    g.setAttribute('aSeed', new THREE.BufferAttribute(new Float32Array(this.seed), 2))
    g.setAttribute('aMix', f(this.mix))
    g.setAttribute('aAlpha', f(this.alpha))
    return g
  }
}

// 雕塑本体：Logo + 三重星环 + 环绕星尘（同一局部空间，随雕塑整体自旋）
function buildSculpture(compact: boolean) {
  const b = new NebulaBuilder()

  // Logo 粒子：按实心率施加倒角厚度，组成一块有立体感的发光板
  // （aSize 以相机参考距离处的 CSS 像素直径标定。alpha 必须压低——加法混合下
  //   亮度靠叠加累积，单体过亮会让整个 N 字内部大面积越过 Bloom 阈值，
  //   变成"大块中亮区域"喂爆模糊链，产生全屏白雾；宁可粒子多而单体暗）
  for (const p of sampleLogoParticles(compact ? 720 : 1400)) {
    b.add(
      [p.x, p.y, (Math.random() * 2 - 1) * 0.16 * p.t],
      0,
      2.4 + Math.random() * 1.7,
      0.2 + Math.random() * 0.22,
      Math.random() * 0.18,
      [Math.random() * Math.PI * 2, 0.6 + Math.random() * 1.4],
    )
  }

  // 粒子星环（类土星环）：3 条不同倾角，GPU 轨道运动 + 离面波浪
  const rings = [
    { r: 1.45, tilt: 0.42, n: compact ? 110 : 190, speed: 0.1 },
    { r: 1.75, tilt: -0.3, n: compact ? 95 : 160, speed: -0.07 },
    { r: 2.05, tilt: 0.62, n: compact ? 75 : 130, speed: 0.05 },
  ]
  for (const ring of rings) {
    for (let i = 0; i < ring.n; i++) {
      b.add(
        [0, 0, 0],
        1,
        1.9 + Math.random() * 1.1,
        0.12 + Math.random() * 0.2,
        0.65 + Math.random() * 0.3,
        [Math.random() * Math.PI * 2, 0.5 + Math.random() * 1.2],
        [ring.r + (Math.random() - 0.5) * 0.12, Math.random() * Math.PI * 2, ring.speed * (0.85 + Math.random() * 0.3)],
        ring.tilt,
      )
    }
  }

  // 雕塑外围星尘：球壳分布
  const dustN = compact ? 130 : 240
  for (let i = 0; i < dustN; i++) {
    const theta = Math.random() * Math.PI * 2
    const phi = Math.acos(2 * Math.random() - 1)
    const r = 2.3 + Math.random() * 2.3
    b.add(
      [r * Math.sin(phi) * Math.cos(theta), r * Math.sin(phi) * Math.sin(theta), r * Math.cos(phi)],
      2,
      1.5 + Math.random() * 1.2,
      0.06 + Math.random() * 0.1,
      0.3 + Math.random() * 0.4,
      [Math.random() * Math.PI * 2, 0.4 + Math.random() * 1.0],
    )
  }
  return b
}

// 背景星场：分布在单位立方体内，由对象 scale 拉伸铺满整个视锥（含前后景深），
// 只做轻微视差倾斜，不随雕塑自旋——保证任何视口下整个 hero 都有星尘而非只有右侧
function buildField(compact: boolean) {
  const b = new NebulaBuilder()
  const n = compact ? 150 : 340
  for (let i = 0; i < n; i++) {
    b.add(
      [Math.random() * 2 - 1, Math.random() * 2 - 1, Math.random() * 2 - 1],
      2,
      1.2 + Math.random() * 1.2,
      0.04 + Math.random() * 0.08,
      0.25 + Math.random() * 0.45,
      [Math.random() * Math.PI * 2, 0.3 + Math.random() * 0.7],
    )
  }
  return b
}

// 顶点着色器：星环轨道运动、全局姿态（自旋+视差）、透视点尺寸、闪烁、
// 星环近侧压暗、Logo 辉光扫描——全部在 GPU 每帧计算，CPU 只更新少量 uniform。
const NEBULA_VERT = /* glsl */ `
uniform float uTime;
uniform float uRotY;
uniform float uTiltX;
uniform float uScanX;
uniform float uPixelRatio;
uniform float uSizeScale;
uniform float uCamDist;
uniform float uGlobalAlpha;

attribute float aKind;
attribute vec3 aParams;
attribute float aTilt;
attribute float aSize;
attribute vec2 aSeed;
attribute float aMix;
attribute float aAlpha;

varying float vAlpha;
varying float vMix;

void main() {
  vec3 pos = position;

  // 星环粒子：轨道角 = 初始角 + 角速度 * 时间，绕 X 轴施加环倾角 + 离面波浪
  if (aKind > 0.5 && aKind < 1.5) {
    float ang = aParams.y + aParams.z * uTime;
    float x = cos(ang) * aParams.x;
    float z = sin(ang) * aParams.x;
    float ct = cos(aTilt);
    float st = sin(aTilt);
    pos = vec3(x, -z * st + sin(ang * 3.0 + aTilt * 5.0) * 0.04, z * ct);
  }

  // 全局姿态：绕 Y 自旋（含鼠标视差），再绕 X 俯视
  float cy = cos(uRotY);
  float sy = sin(uRotY);
  vec3 p = vec3(pos.x * cy - pos.z * sy, pos.y, pos.x * sy + pos.z * cy);
  float cx = cos(uTiltX);
  float sx = sin(uTiltX);
  p = vec3(p.x, p.y * cx - p.z * sx, p.y * sx + p.z * cx);

  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  gl_Position = projectionMatrix * mv;

  // 透视点尺寸：aSize 以"相机参考距离处的 CSS 像素"标定，随距离 1/d 衰减
  float dist = max(0.001, -mv.z);
  gl_PointSize = aSize * uPixelRatio * uSizeScale * uCamDist / dist;

  // 闪烁
  float alpha = aAlpha * (0.72 + 0.28 * sin(uTime * aSeed.y + aSeed.x)) * uGlobalAlpha;

  // 星环：转到雕塑前方（更靠近相机）时压暗，保持 Logo 主体可读、层次分明
  if (aKind > 0.5 && aKind < 1.5) {
    alpha *= 0.35 + 0.65 * clamp((dist - uCamDist + 1.6) / 3.2, 0.0, 1.0);
  }

  // Logo 辉光扫描：一道亮度波沿本地 X 周期扫过，把亮度推入 HDR 区间——
  // 它是泛光的唯一主要"燃料"（主体粒子亮度都在 0.8 阈值之下，只有扫描波
  // 与最热的重叠核泛光），扫过时形成一道移动的热亮光带
  if (aKind < 0.5) {
    float boost = (1.0 - smoothstep(0.0, 0.3, abs(pos.x - uScanX))) * 1.0;
    alpha += boost;
    gl_PointSize *= 1.0 + boost * 0.35;
  }

  vAlpha = alpha;
  vMix = aMix;
}
`

// 片元着色器：软核点精灵（中心实、边缘指数衰减）；
// vAlpha > 1 的扫描增强转为 HDR 亮度（HalfFloat 渲染目标），供 UnrealBloom 拾取
const NEBULA_FRAG = /* glsl */ `
uniform vec3 uColorA;
uniform vec3 uColorB;

varying float vAlpha;
varying float vMix;

void main() {
  vec2 uv = gl_PointCoord - 0.5;
  float d = length(uv);
  if (d > 0.5) discard;
  float core = 1.0 - smoothstep(0.0, 0.5, d);
  core *= core * core;
  float a = min(vAlpha, 1.25);
  float hdr = 1.0 + max(0.0, vAlpha - 1.0) * 2.0;
  gl_FragColor = vec4(mix(uColorA, uColorB, vMix) * hdr, core * a);
}
`

// 抖动：Bloom 光晕在暗部渐变上会被 8bit 输出量化成一圈一圈的色带（圈层感），
// 在 OutputPass（已转到显示空间）之后叠一层随时间变化的微噪声打散量化台阶。
// 只作用于 3D 画布（文字都是 DOM），±1/255 的幅度肉眼只感知为"更顺滑"。
const DitherShader = {
  uniforms: {
    tDiffuse: { value: null },
    uTime: { value: 0 },
  },
  vertexShader: /* glsl */ `
    varying vec2 vUv;
    void main() {
      vUv = uv;
      gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
    }
  `,
  fragmentShader: /* glsl */ `
    uniform sampler2D tDiffuse;
    uniform float uTime;
    varying vec2 vUv;
    float hash(vec2 p) {
      vec3 p3 = fract(vec3(p.xyx) * 0.1031);
      p3 += dot(p3, p3.yzx + 33.33);
      return fract((p3.x + p3.y) * p3.z);
    }
    void main() {
      vec4 color = texture2D(tDiffuse, vUv);
      float n = hash(gl_FragCoord.xy + vec2(fract(uTime) * 913.0, fract(uTime * 0.618) * 517.0));
      color.rgb += (n - 0.5) * (2.0 / 255.0);
      gl_FragColor = color;
    }
  `,
}

function createNebulaMaterial(): THREE.ShaderMaterial {
  return new THREE.ShaderMaterial({
    vertexShader: NEBULA_VERT,
    fragmentShader: NEBULA_FRAG,
    transparent: true,
    depthWrite: false,
    depthTest: false,
    // 加法混合：星尘叠加自然变亮，与 Bloom 相性好，且无绘制顺序问题
    blending: THREE.AdditiveBlending,
    uniforms: {
      uTime: { value: 0 },
      uRotY: { value: 0 },
      uTiltX: { value: NEBULA_TILT },
      uScanX: { value: -SCAN_RANGE },
      uPixelRatio: { value: 1 },
      uSizeScale: { value: 1 },
      uCamDist: { value: NEBULA_CAM_DIST },
      uGlobalAlpha: { value: 1 },
      uColorA: { value: new THREE.Color('#f0ede8') }, // 暖白（品牌前景色）
      uColorB: { value: new THREE.Color('#cfd6e4') }, // 冷白（星环）
    },
  })
}

function setupNebula3D() {
  const canvas = nebulaCanvas.value
  const hero = heroEl.value
  const anchor = nebulaAnchor.value
  if (!canvas || !hero || !anchor) return

  // WebGL2 不可用时静默降级：保留 CSS 柔光，不渲染 3D
  const probe = document.createElement('canvas').getContext('webgl2')
  if (!probe) return

  const compact = window.matchMedia('(max-width: 900px)').matches
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const dpr = Math.min(window.devicePixelRatio || 1, compact ? 1.5 : 2)

  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: false, // 点精灵 + Bloom 后期，MSAA 无用
    powerPreference: 'high-performance',
  })
  renderer.setPixelRatio(dpr)
  // 清屏纯黑：0 是 OETF 编码的不动点，不会像 --bg 那样被二次提亮；底色由 bgMesh 承载
  renderer.setClearColor(0x000000, 1)

  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(NEBULA_FOV, 1, 0.1, 40)
  camera.position.set(0, 0, NEBULA_CAM_DIST)

  // 背景板：承载页面底色。不能走 GL 清屏色——three 的清屏路径假设直连 sRGB 帧缓冲，
  // 会把线性清屏色按 OETF 预编码；经 composer 的线性 RT + OutputPass 后等于被
  // 二次提亮（实测 #0d0d0d 显示成 #404040，整个 hero 蒙一层白）。
  // 材质颜色走正常色彩管理（线性写入、OutputPass 一次性转回），精确还原；
  // 清屏保持纯黑——0 是任何编码的不动点，不可能被提亮。
  const bgRaw = getComputedStyle(document.documentElement).getPropertyValue('--bg').trim()
  const bgMesh = new THREE.Mesh(
    new THREE.PlaneGeometry(2, 2),
    new THREE.MeshBasicMaterial({ color: bgRaw || '#0d0d0d' }),
  )
  bgMesh.material.depthTest = false
  bgMesh.material.depthWrite = false
  bgMesh.position.z = -8 // 相机 z=7，最远粒子 z≈-6，背景板垫底
  bgMesh.scale.set(60, 60, 1) // 覆盖任何视口下的整个视锥截面
  bgMesh.renderOrder = -1
  bgMesh.frustumCulled = false
  scene.add(bgMesh)

  const sculptureMat = createNebulaMaterial()
  const fieldMat = createNebulaMaterial()
  const sculpture = new THREE.Points(buildSculpture(compact).geometry(), sculptureMat)
  const field = new THREE.Points(buildField(compact).geometry(), fieldMat)
  // 位置由着色器位移（星环轨道），关掉视锥剔除防止被错误裁掉
  sculpture.frustumCulled = false
  field.frustumCulled = false
  scene.add(sculpture, field)

  // 泛光后期：RenderPass → UnrealBloom → OutputPass（线性→sRGB）→ 抖动
  const composer = new EffectComposer(renderer)
  composer.setPixelRatio(dpr)
  composer.addPass(new RenderPass(scene, camera))
  const bloom = new UnrealBloomPass(new THREE.Vector2(1, 1), BLOOM.strength, BLOOM.radius, BLOOM.threshold)
  // 软化高通道滤波的截止膝宽：默认 0.01 的硬边经模糊链后就是同心圈层的源头
  ;(bloom.highPassUniforms as Record<string, { value: number }>)['smoothWidth'].value = BLOOM.knee
  composer.addPass(bloom)
  composer.addPass(new OutputPass())
  const ditherPass = new ShaderPass(DitherShader)
  composer.addPass(ditherPass)

  // 视角与动画状态
  let rotY = 0
  let parX = 0 // 鼠标视差目标（rad）
  let parY = 0
  let curX = 0 // 缓动后的当前值
  let curY = 0
  let lastT = 0
  let heroVisible = true

  function applyUniforms(t: number) {
    const scanX = -SCAN_RANGE + ((t % SCAN_PERIOD) / SCAN_PERIOD) * SCAN_RANGE * 2
    for (const m of [sculptureMat, fieldMat]) {
      m.uniforms.uTime.value = t
      m.uniforms.uScanX.value = scanX
    }
    ditherPass.uniforms.uTime.value = t
    // 雕塑：自旋 + 完整视差；星场：仅轻微视差倾斜，不累积自旋
    sculptureMat.uniforms.uRotY.value = rotY + curX
    sculptureMat.uniforms.uTiltX.value = NEBULA_TILT + curY
    fieldMat.uniforms.uRotY.value = curX * 0.25
    fieldMat.uniforms.uTiltX.value = NEBULA_TILT * 0.4 + curY * 0.15
  }

  function renderStatic() {
    rotY = 0.42
    applyUniforms(8.6)
    composer.render()
  }

  function resize() {
    const rect = hero!.getBoundingClientRect()
    const w = Math.max(1, rect.width)
    const h = Math.max(1, rect.height)
    renderer.setSize(w, h, false)
    composer.setSize(w, h)
    camera.aspect = w / h
    camera.updateProjectionMatrix()

    const halfH = Math.tan(THREE.MathUtils.degToRad(NEBULA_FOV) / 2) * NEBULA_CAM_DIST
    const halfW = halfH * camera.aspect
    const worldPerPx = (halfW * 2) / w // z=0 平面上每 CSS 像素对应的世界单位
    const pxScale = THREE.MathUtils.clamp(h / 900, 0.85, 1.35) // 大屏适当放大粒子
    for (const m of [sculptureMat, fieldMat]) {
      m.uniforms.uPixelRatio.value = dpr
    }

    // 雕塑定位与缩放：把右侧锚点（老版方块）的中心投影为 z=0 平面上的世界坐标，
    // 星环外沿直径取锚点宽度的 1.25 倍——比老版更舒展又不出画；
    // ≤900px 锚点随 .hero-right 隐藏 → 居中、取视口宽 85%、压暗
    const RING_SPAN = 4.1 // 星环外沿直径（世界单位，缩放前）
    const ar = anchor!.getBoundingClientRect()
    let s: number
    let alpha = 1
    let cx: number
    let cy: number
    if (ar.width > 1) {
      cx = ar.left + ar.width / 2 - rect.left
      cy = ar.top + ar.height / 2 - rect.top
      s = (ar.width * 1.25 * worldPerPx) / RING_SPAN
    } else {
      cx = w / 2
      cy = h * 0.42
      s = (w * 0.85 * worldPerPx) / RING_SPAN
      alpha = 0.8
    }
    s = Math.min(s, (halfW * 1.9) / RING_SPAN) // 兜底：环直径不超过视口宽的 95%
    sculpture.position.set(((cx / w) * 2 - 1) * halfW, (1 - (cy / h) * 2) * halfH, 0)
    sculpture.scale.setScalar(s)
    // 雕塑粒子尺寸跟随整体缩放（点大小不受对象 scale 影响，需手动折算）
    sculptureMat.uniforms.uSizeScale.value = pxScale * s
    fieldMat.uniforms.uSizeScale.value = pxScale
    sculptureMat.uniforms.uGlobalAlpha.value = alpha

    // 背景星场拉伸铺满整个视锥，带前后景深
    field.scale.set(halfW * 1.15, halfH * 1.15, 4.5)
    field.position.set(0, 0, -1.5)

    if (reduced) renderStatic()
  }
  resize()
  // DEBUG: 布局诊断写入 DOM
  const dbg = hero.querySelector('.nebula-anchor')!
  dbg.setAttribute('data-dbg', JSON.stringify({
    hero: hero.getBoundingClientRect().toJSON(),
    anchor: anchor.getBoundingClientRect().toJSON(),
    dpr: window.devicePixelRatio,
    vw: window.innerWidth,
    vh: window.innerHeight,
  }))
  const resizeObs = new ResizeObserver(resize)
  resizeObs.observe(hero)

  function onPointerMove(e: PointerEvent) {
    const r = hero!.getBoundingClientRect()
    parX = ((e.clientX - (r.left + r.width / 2)) / r.width) * 0.5
    parY = ((e.clientY - (r.top + r.height / 2)) / r.height) * 0.3
  }

  function frame(now: number) {
    nebulaAnimId = requestAnimationFrame(frame)
    // hero 滚出视口或标签页隐藏时跳过渲染（rAF 保持心跳，恢复即续）
    if (!heroVisible || document.hidden) {
      lastT = now
      return
    }
    const t = now / 1000
    const dt = Math.min(0.05, lastT === 0 ? 0.016 : (now - lastT) / 1000)
    lastT = now
    rotY += dt * NEBULA_SPIN
    curX += (parX - curX) * 0.05
    curY += (parY - curY) * 0.05
    applyUniforms(t)
    composer.render()
  }

  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) heroVisible = e.isIntersecting
    },
    { threshold: 0 },
  )
  io.observe(hero)

  if (reduced) {
    renderStatic()
  } else {
    hero.addEventListener('pointermove', onPointerMove, { passive: true })
    nebulaAnimId = requestAnimationFrame(frame)
  }

  nebulaCleanup = () => {
    if (nebulaAnimId !== null) {
      cancelAnimationFrame(nebulaAnimId)
      nebulaAnimId = null
    }
    hero.removeEventListener('pointermove', onPointerMove)
    resizeObs.disconnect()
    io.disconnect()
    sculpture.geometry.dispose()
    field.geometry.dispose()
    sculptureMat.dispose()
    fieldMat.dispose()
    composer.dispose()
    renderer.dispose()
    renderer.forceContextLoss()
  }
}

// ---- Ticker 无缝跑马灯 ----
// 旧实现（rAF 逐帧位移 + offsetWidth 测量拼接）有接缝缺陷：
// offsetWidth 取整丢失小数、字体 swap 后不重测、resize 后失效——每循环一次接缝跳一下（"抽搐"）。
// 现改为模板内双轨道 + CSS translateX(-100%)：位移量恒等于轨道自身宽度，接缝数学上必然对齐；
// 动画跑在合成器线程，主线程忙（自定义光标 rAF / 星云 WebGL 渲染）也不掉帧，速度与屏幕刷新率无关。
// JS 只负责按轨道实测宽度折算动画时长，保持与原版 0.6px/帧@60fps（≈36px/s）一致的速度。
const TICKER_SPEED = 36 // px/s
const tickerEl = ref<HTMLElement | null>(null)

function applyTickerDuration() {
  const tracks = tickerEl.value?.querySelectorAll<HTMLElement>('.ticker-track')
  if (!tracks || tracks.length === 0) return
  const w = tracks[0].getBoundingClientRect().width
  if (w > 0) {
    const dur = `${(w / TICKER_SPEED).toFixed(2)}s`
    tracks.forEach((t) => {
      t.style.animationDuration = dur
    })
  }
}

async function setupTicker() {
  applyTickerDuration() // 字体就绪前先按回退字体给近似时长
  try {
    await document.fonts.ready
    applyTickerDuration() // Unbounded 换载完成后按真实轨道宽度校准（仅影响速度，接缝始终精确）
  } catch {
    /* 不支持 FontFaceSet 时保持近似时长 */
  }
}

// ---- Reveal 滚动动画 ----
function initReveal() {
  const els = document.querySelectorAll<HTMLElement>('.reveal')
  const obs = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible')
          obs.unobserve(entry.target)
        }
      })
    },
    { threshold: 0.12 }
  )
  els.forEach((el) => obs.observe(el))
}

onMounted(() => {
  setupCursor()
  setupNebula3D()
  setupTicker()
  initReveal()
})

onBeforeUnmount(() => {
  if (cursorAnimId !== null) cancelAnimationFrame(cursorAnimId)
  if (nebulaAnimId !== null) cancelAnimationFrame(nebulaAnimId)
  if (cursorHooks) {
    document.removeEventListener('mousemove', onCursorMove)
    document.body.classList.remove('has-custom-cursor')
  }
  nebulaCleanup?.()
  nebulaCleanup = null
})
</script>

<template>
  <div class="home-root" :class="{ 'no-custom-cursor': isMobile }">
    <div v-if="!isMobile" ref="cursor" class="cursor" :class="{ visible: cursorVisible }"></div>
    <div v-if="!isMobile" ref="ring" class="cursor-ring"></div>

    <!-- Hero：Three.js 画布铺满整个 hero 作为最底层（星场 + 粒子雕塑 + 泛光），
         左侧文字与统计浮于其上 -->
    <section ref="heroEl" class="hero">
      <canvas ref="nebulaCanvas" class="nebula-canvas" aria-hidden="true"></canvas>
      <div class="hero-left">
        <h1 class="hero-title" translate="no">ONE<br />IDENTITY<br /><em>Everywhere</em></h1>
        <p class="hero-desc">{{ $t('home.hero.description') }}</p>
        <div class="hero-cta">
          <RouterLink class="cta-btn cta-btn--primary" to="/account/register">{{ $t('home.hero.createAccount') }}</RouterLink>
          <RouterLink class="cta-btn cta-btn--secondary" to="/account/login">{{ $t('home.hero.login') }}</RouterLink>
        </div>
      </div>
      <div class="hero-right">
        <!-- 3D 雕塑定位锚点：仅占位投影用（不可见），雕塑尺寸随它自适应 -->
        <div ref="nebulaAnchor" class="nebula-anchor" aria-hidden="true"></div>
        <div class="hero-stats">
          <div class="stat-item">
            <span class="stat-num" translate="no">99.9%</span>
            <span class="stat-label">{{ $t('home.stats.availability') }}</span>
          </div>
          <div class="stat-item">
            <span class="stat-num" translate="no">&lt; 80ms</span>
            <span class="stat-label">{{ $t('home.stats.authResponse') }}</span>
          </div>
        </div>
      </div>
    </section>

    <!-- Ticker：双轨道各含完整副本序列，translateX(-100%) 循环，接缝天然对齐 -->
    <div class="ticker" ref="tickerEl" aria-hidden="true">
      <div class="ticker-track" v-for="t in 2" :key="t">
        <span class="ticker-item" translate="no" v-for="i in 30" :key="i">NEBULA</span>
      </div>
    </div>

    <!-- Features -->
    <section class="features">
      <div class="section-header">
        <div class="reveal">
          <p class="section-label">{{ $t('home.features.label') }}</p>
          <h2 class="section-title" translate="no">Everything<br />You <em>Need</em></h2>
        </div>
        <p class="section-body reveal reveal-delay-2">{{ $t('home.features.description') }}</p>
      </div>

      <div class="feature-grid">
        <div v-for="(f, i) in [
          { n: '01', key: 'identitySecurity' },
          { n: '02', key: 'profile' },
          { n: '03', key: 'thirdParty' },
          { n: '04', key: 'activityLog' },
          { n: '05', key: 'oauth' },
          { n: '06', key: 'dataExport' },
        ]" :key="f.key" class="feature-card reveal" :class="`reveal-delay-${i % 3}`">
          <p class="feature-number" translate="no">{{ f.n }}</p>
          <h3 class="feature-name">{{ $t(`home.feature.${f.key}.name`) }}</h3>
          <p class="feature-desc">{{ $t(`home.feature.${f.key}.desc`) }}</p>
        </div>
      </div>
    </section>

    <!-- Manifesto -->
    <section class="manifesto">
      <div class="manifesto-left reveal">
        <div class="manifesto-quote" translate="no">YOUR<br /><span class="accent">DATA</span><br />YOUR<br />RULES</div>
      </div>
      <div class="manifesto-right">
        <p class="manifesto-text reveal">
          <span>{{ $t('home.manifesto.text1.before') }}</span>
          <strong>{{ $t('home.manifesto.text1.strong') }}</strong>
          <span>{{ $t('home.manifesto.text1.after') }}</span>
        </p>
        <p class="manifesto-text reveal reveal-delay-1">
          <span>{{ $t('home.manifesto.text2.before') }}</span>
          <strong>{{ $t('home.manifesto.text2.strong') }}</strong>
          <span>{{ $t('home.manifesto.text2.after') }}</span>
        </p>
        <p class="manifesto-text reveal reveal-delay-2">
          <span>{{ $t('home.manifesto.text3.part1') }}</span>
          <strong>{{ $t('home.manifesto.text3.strong1') }}</strong>
          <span>{{ $t('home.manifesto.text3.part2') }}</span>
          <strong>{{ $t('home.manifesto.text3.strong2') }}</strong>
          <span>{{ $t('home.manifesto.text3.part3') }}</span>
        </p>
      </div>
    </section>

    <!-- CTA -->
    <section class="cta-section">
      <div class="cta-title reveal" translate="no">Start<br /><em>Today</em><br />Free</div>
      <div class="cta-right reveal reveal-delay-2">
        <p class="cta-sub">{{ $t('home.cta.description') }}</p>
        <div class="cta-actions">
          <RouterLink class="cta-btn cta-btn--primary" to="/account/register">{{ $t('home.cta.registerNow') }}</RouterLink>
          <RouterLink class="cta-btn cta-btn--secondary" to="/account/login">{{ $t('home.cta.alreadyHaveAccount') }}</RouterLink>
        </div>
      </div>
    </section>

    <footer class="home-footer">
      <span class="footer-logo" translate="no">Nebula Studios</span>
      <div class="footer-links">
        <RouterLink class="footer-link" to="/policy#privacy">{{ $t('policy.privacyPolicy') }}</RouterLink>
        <RouterLink class="footer-link" to="/policy#terms">{{ $t('policy.termsOfService') }}</RouterLink>
        <RouterLink class="footer-link" to="/policy#cookies">{{ $t('policy.cookiePolicy') }}</RouterLink>
      </div>
      <div class="page-footer" translate="no">{{ $t('footer.copyright', { year: new Date().getFullYear() }) }}</div>
    </footer>
  </div>
</template>

<style scoped>
/* 主页专用样式（迁移自 modules/home/assets/css/home.css，scoped 隔离同名类）。
   原站的全局副作用选择器（body::before 噪点、html/body 滚动、自定义光标、.reveal 隐藏态）
   已限定到 .home-root 内，避免污染其它页面。 */

/* ---- 自定义光标：隐藏系统光标（仅在 home 页子树内生效） ---- */
body.has-custom-cursor .home-root,
body.has-custom-cursor .home-root * {
  cursor: none !important;
}
/* 移动端恢复默认光标（根节点带 .no-custom-cursor 时） */
.home-root.no-custom-cursor,
.home-root.no-custom-cursor * {
  cursor: auto !important;
}

.home-root {
  scroll-behavior: smooth;
  overflow-x: hidden;
}

/* ---- 噪点覆盖层（限定到 home-root） ---- */
.home-root::before {
  content: '';
  position: fixed;
  inset: 0;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)' opacity='1'/%3E%3C/svg%3E");
  opacity: 0.015;
  pointer-events: none;
  z-index: 1000;
}

/* ---- 自定义光标 ---- */
.cursor {
  position: fixed;
  width: 6px;
  height: 6px;
  background: var(--fg);
  border-radius: 50%;
  pointer-events: none;
  z-index: 9999;
  transform: translate(-50%, -50%);
  transition: transform 0.1s, width 0.3s var(--ease), height 0.3s var(--ease), opacity 0.3s;
  mix-blend-mode: difference;
  opacity: 0;
}
.cursor.visible {
  opacity: 1;
}
.cursor-ring {
  position: fixed;
  width: 32px;
  height: 32px;
  border: 1px solid rgba(240, 237, 232, 0.3);
  border-radius: 50%;
  pointer-events: none;
  z-index: 9998;
  transform: translate(-50%, -50%);
  transition: transform 0.18s var(--ease), width 0.4s var(--ease), height 0.4s var(--ease), border-color 0.3s;
  opacity: 0;
}
.cursor-ring.visible {
  opacity: 1;
}
.home-root:has(a:hover) .cursor-ring,
.home-root:has(button:hover) .cursor-ring {
  width: 48px;
  height: 48px;
  border-color: rgba(240, 237, 232, 0.6);
}

/* ---- Hero ---- */
.hero {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 1fr 1fr;
  position: relative;
  overflow: hidden;
  padding-top: 60px;
}
.hero::before {
  content: '';
  position: absolute;
  left: 50%;
  top: 60px;
  bottom: 0;
  width: 1px;
  background: var(--line);
  /* 画布（不透明 WebGL 层）是 hero 的第一个子元素，定位元素按树序绘制；
     中线需抬到画布之上才能可见 */
  z-index: 1;
}
.hero-left {
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 120px 64px 80px 80px;
  position: relative;
}
.hero-eyebrow {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--mid);
  margin-bottom: 32px;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.2s forwards;
}
.hero-eyebrow span {
  display: inline-block;
  width: 24px;
  height: 1px;
  background: var(--mid);
  vertical-align: middle;
  margin-right: 12px;
}
.hero-title {
  font-family: var(--font-display);
  font-size: clamp(48px, 6vw, 88px);
  font-weight: 900;
  line-height: 0.92;
  letter-spacing: -0.02em;
  text-transform: uppercase;
  color: var(--fg);
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.4s forwards;
}
.hero-title em {
  font-style: italic;
  font-family: var(--font-serif);
  font-weight: 300;
  display: block;
  font-size: 0.85em;
  letter-spacing: 0;
}
.hero-desc {
  margin-top: 40px;
  font-family: var(--font-sans);
  font-size: 17px;
  font-style: normal;
  color: var(--mid);
  max-width: 380px;
  line-height: 1.8;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.6s forwards;
}
.hero-cta {
  margin-top: 56px;
  display: flex;
  gap: 16px;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.8s forwards;
}
.hero-cta .cta-btn,
.cta-actions .cta-btn {
  width: auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 48px;
  padding: 0 20px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s;
}
.hero-cta .cta-btn--primary,
.cta-actions .cta-btn--primary {
  background: var(--fg);
  color: var(--bg);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.hero-cta .cta-btn--primary:hover,
.cta-actions .cta-btn--primary:hover {
  background: var(--fg-hover);
}
.hero-cta .cta-btn--secondary,
.cta-actions .cta-btn--secondary {
  background: transparent;
  border: 1px solid var(--dim);
  color: var(--fg);
  font-family: var(--font-mono);
  font-weight: 400;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
}
.hero-cta .cta-btn--secondary:hover,
.cta-actions .cta-btn--secondary:hover {
  border-color: var(--mid);
  background: var(--dim);
}
.hero-right {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-end;
  padding: 120px 80px 80px 64px;
  position: relative;
}
/* 3D 雕塑出现动画：锚点不可见，改为对 hero-right 整体淡入（统计数字随之出现） */
.hero-right {
  opacity: 0;
  animation: fadeIn 1.5s var(--ease) 0.6s forwards;
}

/* ---- Three.js 星云画布：铺满整个 hero ----
 * WebGL 渲染清屏色取自 --bg，画布与页面底色浑然一体；
 * 不透明（alpha:false）+ 顶层噪点覆盖，边缘与页面无缝衔接 */
.nebula-canvas {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: block;
  pointer-events: none;
}
/* 3D 雕塑定位锚点：不可见，仅用于把雕塑中心投影到老版方块位置 */
.nebula-anchor {
  width: min(480px, 42vw);
  aspect-ratio: 1;
  position: relative;
  pointer-events: none;
}

/* ---- Hero 统计数据 ---- */
.hero-stats {
  position: absolute;
  bottom: 80px;
  right: 80px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  align-items: flex-end;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 1s forwards;
}
.stat-item {
  text-align: right;
}
.stat-num {
  font-family: var(--font-display);
  font-size: 28px;
  font-weight: 700;
  color: var(--fg);
  letter-spacing: -0.02em;
  display: block;
}
.stat-label {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.15em;
  color: var(--mid);
  text-transform: uppercase;
}

/* ---- 滚动 Ticker（双轨道无缝循环）----
 * 每条轨道平移自身宽度的 -100% 后复位，复位点与相邻轨道的起点严格重合，接缝零误差。
 * 动画时长由 setupTicker() 按实测轨道宽度写入，默认值仅为字体加载前的近似速度。 */
.ticker {
  border-top: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
  display: flex;
  overflow: hidden;
  white-space: nowrap;
  padding: 12px 0;
  background: var(--bg);
  position: relative;
  z-index: 2;
}
.ticker-track {
  display: flex;
  flex-shrink: 0;
  will-change: transform;
  animation: ticker-scroll 140s linear infinite;
}
@keyframes ticker-scroll {
  to {
    transform: translateX(-100%);
  }
}
@media (prefers-reduced-motion: reduce) {
  .ticker-track {
    animation: none;
  }
}
.ticker-item {
  font-family: var(--font-display);
  font-size: var(--text-sm);
  font-weight: 700;
  letter-spacing: 0.35em;
  text-transform: uppercase;
  color: var(--mid);
  padding: 0 48px;
  white-space: nowrap;
}

/* ---- 功能特性 ---- */
.features {
  padding: 120px 80px;
  border-top: 1px solid var(--line);
}
.section-header {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 64px;
  margin-bottom: 80px;
  align-items: end;
}
.section-label {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--mid);
  margin-bottom: 20px;
}
.section-label::before {
  content: '//';
  margin-right: 8px;
  opacity: 0.5;
}
.section-title {
  font-family: var(--font-display);
  font-size: clamp(28px, 3.5vw, 48px);
  font-weight: 700;
  line-height: 1.05;
  letter-spacing: -0.02em;
  text-transform: uppercase;
  color: var(--fg);
}
.section-title em {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 300;
}
.section-body {
  font-family: var(--font-sans);
  font-size: var(--text-md);
  font-style: normal;
  color: var(--mid);
  line-height: 1.8;
  max-width: 400px;
  align-self: end;
}
.feature-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  border-top: 1px solid var(--line);
}
.feature-card {
  padding: 48px 40px;
  border-right: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
  position: relative;
  overflow: hidden;
  transition: background 0.5s var(--ease);
}
.feature-card:nth-child(3n) {
  border-right: none;
}
.feature-card:nth-last-child(-n+3) {
  border-bottom: none;
}
.feature-card::before {
  content: '';
  position: absolute;
  inset: 0;
  background: var(--line);
  transform: scaleY(0);
  transform-origin: bottom;
  transition: transform 0.5s var(--ease);
}
.feature-card:hover::before {
  transform: scaleY(1);
}
.feature-number {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  color: var(--mid);
  margin-bottom: 32px;
  position: relative;
}
.feature-icon {
  width: 36px;
  height: 36px;
  margin-bottom: 24px;
  position: relative;
  opacity: 0.7;
}
.feature-icon svg {
  width: 100%;
  height: 100%;
  stroke: var(--fg);
  fill: none;
  stroke-width: 1;
  stroke-linecap: square;
}
.feature-name {
  font-family: var(--font-display);
  font-size: var(--text-md);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg);
  margin-bottom: 12px;
  position: relative;
}
.feature-desc {
  font-family: var(--font-sans);
  font-size: var(--text-base);
  font-style: normal;
  color: var(--mid);
  line-height: 1.75;
  position: relative;
}

/* ---- 宣言区域 ---- */
.manifesto {
  padding: 140px 80px;
  border-top: 1px solid var(--line);
  display: grid;
  grid-template-columns: 1fr 2fr;
  gap: 80px;
  align-items: start;
}
.manifesto-left {
  position: sticky;
  top: 80px;
}
.manifesto-quote {
  font-family: var(--font-display);
  font-size: clamp(36px, 5vw, 72px);
  font-weight: 900;
  line-height: 0.95;
  text-transform: uppercase;
  letter-spacing: -0.02em;
  color: var(--fg);
}
.manifesto-quote .accent {
  color: var(--mid);
}
.manifesto-right {
  padding-top: 8px;
}
.manifesto-text {
  font-family: var(--font-sans);
  font-size: 17px;
  font-style: normal;
  color: var(--mid);
  line-height: 1.9;
  margin-bottom: 32px;
}
.manifesto-text strong {
  color: var(--fg);
  font-style: normal;
  font-weight: 600;
}

/* ---- CTA 区域 ---- */
.cta-section {
  border-top: 1px solid var(--line);
  padding: 120px 80px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 80px;
  align-items: center;
}
.cta-title {
  font-family: var(--font-display);
  font-size: clamp(32px, 4vw, 60px);
  font-weight: 900;
  line-height: 0.95;
  text-transform: uppercase;
  letter-spacing: -0.02em;
  color: var(--fg);
}
.cta-title em {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 300;
  display: block;
  font-size: 0.8em;
}
.cta-right {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
.cta-sub {
  font-family: var(--font-sans);
  font-style: normal;
  color: var(--mid);
  font-size: var(--text-md);
  line-height: 1.75;
}
.cta-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

/* ---- 页脚 ---- */
.home-footer {
  border-top: 1px solid var(--line);
  padding: 32px 80px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.footer-logo {
  font-family: var(--font-display);
  font-size: var(--text-xs);
  letter-spacing: 0.15em;
  text-transform: uppercase;
  color: var(--mid);
}
.footer-copy {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  color: var(--mid);
}
.footer-links {
  display: flex;
  gap: 24px;
}
.footer-link {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--mid);
  transition: color 0.3s;
}
.footer-link:hover {
  color: var(--fg);
}

/* ---- 动画 ---- */
@keyframes fadeUp {
  from {
    opacity: 0;
    transform: translateY(24px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
@keyframes fadeIn {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

/* ---- 滚动显示动画（限 home-root，保留 reveal 滚动显示） ---- */
.reveal {
  transition: opacity 0.9s var(--ease), transform 0.9s var(--ease);
  opacity: 0;
  transform: translateY(32px);
}
.reveal.visible {
  opacity: 1;
  transform: none;
}
.reveal-delay-1 {
  transition-delay: 0.1s;
}
.reveal-delay-2 {
  transition-delay: 0.2s;
}
.reveal-delay-3 {
  transition-delay: 0.3s;
}
.reveal-delay-4 {
  transition-delay: 0.4s;
}
.reveal-delay-5 {
  transition-delay: 0.5s;
}

/* ---- 响应式 ---- */
@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
    min-height: auto;
  }
  .hero::before {
    display: none;
  }
  .hero-left {
    padding: 100px 32px 48px;
  }
  .hero-right {
    display: none;
  }
  .features {
    padding: 80px 32px;
  }
  .section-header {
    grid-template-columns: 1fr;
    gap: 24px;
  }
  .feature-grid {
    grid-template-columns: 1fr;
  }
  .feature-card {
    border-right: none;
  }
  .feature-card:nth-last-child(-n+3) {
    border-bottom: 1px solid var(--line);
  }
  .feature-card:last-child {
    border-bottom: none;
  }
  .manifesto {
    grid-template-columns: 1fr;
    padding: 80px 32px;
  }
  .manifesto-left {
    position: static;
  }
  .cta-section {
    grid-template-columns: 1fr;
    padding: 80px 32px;
  }
  .home-footer {
    padding: 24px 32px;
    flex-direction: column;
    gap: 16px;
    text-align: center;
  }
  .footer-links {
    justify-content: center;
  }
}
</style>