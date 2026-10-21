import { INTRO_TRAVEL } from '../constants'

/**
 * 顶点着色器：入场汇聚、星环轨道运动、全局姿态（自旋 + 鼠标视差）、透视点尺寸、
 * 闪烁、星环近侧压暗、Logo 辉光扫描——全部在 GPU 每帧计算，CPU 只更新少量 uniform。
 * 属性序号与 `gl/geometry.ts` 的 POINT_FIELDS 逐项对应。
 */
export const NEBULA_VERT = /* glsl */ `#version 300 es
layout(location = 0) in vec3 position;
layout(location = 1) in vec3 aScatter;
layout(location = 2) in float aDelay;
layout(location = 3) in float aKind;
layout(location = 4) in vec3 aParams;
layout(location = 5) in float aTilt;
layout(location = 6) in float aSize;
layout(location = 7) in vec2 aSeed;
layout(location = 8) in float aMix;
layout(location = 9) in float aAlpha;

uniform mat4 modelViewMatrix;
uniform mat4 projectionMatrix;
uniform float uTime;
uniform float uIntro;
uniform float uRotY;
uniform float uTiltX;
uniform float uScanX;
uniform float uPixelRatio;
uniform float uSizeScale;
uniform float uCamDist;
uniform float uGlobalAlpha;

out float vAlpha;
out float vMix;

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
  // 整层随入场时钟淡入：uIntro 从首个真正出帧的帧起算，淡入起点即"有像素"那一刻。
  // 画布级 CSS 淡入做不到——它从首屏起算，等粒子出现早已放完
  alpha *= smoothstep(0.0, 0.3, uIntro);
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
