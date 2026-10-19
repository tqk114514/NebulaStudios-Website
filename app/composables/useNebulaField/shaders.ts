import { INTRO_TRAVEL } from './constants'

/**
 * 顶点着色器：入场汇聚、星环轨道运动、全局姿态（自旋 + 鼠标视差）、透视点尺寸、
 * 闪烁、星环近侧压暗、Logo 辉光扫描——全部在 GPU 每帧计算，CPU 只更新少量 uniform。
 */
export const NEBULA_VERT = /* glsl */ `
uniform float uTime;
uniform float uIntro;
uniform float uRotY;
uniform float uTiltX;
uniform float uScanX;
uniform float uPixelRatio;
uniform float uSizeScale;
uniform float uCamDist;
uniform float uGlobalAlpha;

attribute vec3 aScatter;
attribute float aDelay;
attribute float aKind;
attribute vec3 aParams;
attribute float aTilt;
attribute float aSize;
attribute vec2 aSeed;
attribute float aMix;
attribute float aAlpha;

varying float vAlpha;
varying float vMix;

const float TRAVEL = ${INTRO_TRAVEL};

float easeInOutCubic(float t) {
  return t < 0.5 ? 4.0 * t * t * t : 1.0 - pow(-2.0 * t + 2.0, 3.0) * 0.5;
}

void main() {
  vec3 pos = position;

  // 星环：轨道角 = 初始角 + 角速度 × 时间，绕 X 轴施加环倾角并叠离面波浪
  if (aKind > 0.5 && aKind < 1.5) {
    float ang = aParams.y + aParams.z * uTime;
    float x = cos(ang) * aParams.x;
    float z = sin(ang) * aParams.x;
    float ct = cos(aTilt);
    float st = sin(aTilt);
    pos = vec3(x, -z * st + sin(ang * 3.0 + aTilt * 5.0) * 0.04, z * ct);
  }

  // 入场：q 是减去错峰后的本地进度，q=1 那一瞬即落地。落地闪光只在
  // q ∈ [0.8, 1.2] 内非零，随错峰波前扫过雕塑
  float q = (uIntro - aDelay) / TRAVEL;
  float e = easeInOutCubic(clamp(q, 0.0, 1.0));
  float land = max(0.0, 1.0 - abs(q - 1.0) * 5.0);
  pos = mix(aScatter, pos, e);

  // 全局姿态：先绕 Y 自旋，再绕 X 俯视
  float cy = cos(uRotY);
  float sy = sin(uRotY);
  vec3 p = vec3(pos.x * cy - pos.z * sy, pos.y, pos.x * sy + pos.z * cy);
  float cx = cos(uTiltX);
  float sx = sin(uTiltX);
  p = vec3(p.x, p.y * cx - p.z * sx, p.y * sx + p.z * cx);

  vec4 mv = modelViewMatrix * vec4(p, 1.0);
  gl_Position = projectionMatrix * mv;

  // aSize 以相机参考距离处的 CSS 像素标定，随距离按 1/d 衰减
  float dist = max(0.001, -mv.z);
  gl_PointSize = aSize * uPixelRatio * uSizeScale * uCamDist / dist;
  // 途中是糊的大点，落位才收成锐点；落地瞬间再撑大一点强化波前
  gl_PointSize *= 1.0 + (1.0 - e) * 1.5 + land * 0.5;

  float alpha = aAlpha * (0.72 + 0.28 * sin(uTime * aSeed.y + aSeed.x)) * uGlobalAlpha;
  alpha *= 0.12 + 0.88 * e;
  alpha += land * 0.55 * uGlobalAlpha;

  // 星环转到雕塑前方时压暗，保住 Logo 主体的可读层次
  if (aKind > 0.5 && aKind < 1.5) {
    alpha *= 0.35 + 0.65 * clamp((dist - uCamDist + 1.6) / 3.2, 0.0, 1.0);
  }

  // 辉光扫描是泛光的主要燃料：把亮度推入 HDR 区间（阈值之上的只有扫描波
  // 与最热的重叠核），扫过时形成一道移动的热亮光带。未落地的粒子不参与
  if (aKind < 0.5) {
    float boost = (1.0 - smoothstep(0.0, 0.3, abs(pos.x - uScanX))) * e;
    alpha += boost;
    gl_PointSize *= 1.0 + boost * 0.35;
  }

  vAlpha = alpha;
  vMix = aMix;
}
`

/**
 * 片元着色器：软核点精灵（中心实、边缘指数衰减）。
 * vAlpha > 1 的扫描增强转为 HDR 亮度，供 UnrealBloom 在高通滤波里拾取。
 */
export const NEBULA_FRAG = /* glsl */ `
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
