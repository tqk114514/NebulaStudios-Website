/**
 * 片元着色器：软核点精灵（中心实、边缘指数衰减）。
 * vAlpha > 1 的扫描增强转为 HDR 亮度，供泛光链的高通滤波拾取。
 * 输出为线性空间色值，转 sRGB 在最后一趟统一做。
 */
export const NEBULA_FRAG = /* glsl */ `#version 300 es
precision highp float;

uniform vec3 uColorA;
uniform vec3 uColorB;

in float vAlpha;
in float vMix;

out vec4 outColor;

void main() {
  vec2 uv = gl_PointCoord - 0.5;
  float d = length(uv);
  if (d > 0.5) discard;
  float core = 1.0 - smoothstep(0.0, 0.5, d);
  core *= core * core;
  float a = min(vAlpha, 1.25);
  float hdr = 1.0 + max(0.0, vAlpha - 1.0) * 2.0;
  outColor = vec4(mix(uColorA, uColorB, vMix) * hdr, core * a);
}
`
