import type { GaussianKernel } from '../gl/bloom-math'

/** 软膝高通：亮度落在 [threshold, threshold+smoothWidth] 内做平滑，硬截止会在模糊链里产生同心圈层 */
export const HIGH_PASS_FRAG = /* glsl */ `#version 300 es
precision highp float;

uniform sampler2D uSource;
uniform float uThreshold;
uniform float uSmoothWidth;

in vec2 vUv;

out vec4 outColor;

void main() {
  vec4 texel = texture(uSource, vUv);
  float v = dot(texel.rgb, vec3(0.2126, 0.7152, 0.0722));
  float alpha = smoothstep(uThreshold, uThreshold + uSmoothWidth, v);
  outColor = vec4(texel.rgb * alpha, 1.0);
}
`

const literal = (values: number[]): string =>
  values.map((value) => value.toFixed(6)).join(', ')

/**
 * 一维可分离高斯的一趟。抽样表按卷积半径在这里烤进常量——
 * 每级 mip 的半径不同，程序数量与 mip 数量一致，uniform 只留方向与纹素尺寸。
 */
export function blurFragSource(kernel: GaussianKernel, pairs: number): string {
  return /* glsl */ `#version 300 es
precision highp float;

uniform sampler2D uColor;
uniform vec2 uInvSize;
uniform vec2 uDirection;

const int KERNEL_PAIRS = ${pairs};
const float CENTER_WEIGHT = ${kernel.centerWeight.toFixed(6)};
const float OFFSETS[${pairs}] = float[](${literal(kernel.offsets)});
const float WEIGHTS[${pairs}] = float[](${literal(kernel.weights)});

in vec2 vUv;

out vec4 outColor;

void main() {
  vec3 sum = texture(uColor, vUv).rgb * CENTER_WEIGHT;
  for (int i = 0; i < KERNEL_PAIRS; i++) {
    vec2 uvOffset = uDirection * uInvSize * OFFSETS[i];
    sum += (texture(uColor, vUv + uvOffset).rgb + texture(uColor, vUv - uvOffset).rgb) * WEIGHTS[i];
  }
  outColor = vec4(sum, 1.0);
}
`
}

/**
 * 五级模糊结果加权相加。`3.0 × strength` 与镜像权重 `mix(factor, 1.2−factor, radius)`
 * 都取自原实现：radius 决定各级占比在 factor 与 1.2−factor 之间怎么插值。
 * 五级只能逐行写：sampler 数组的下标在 GLSL ES 里必须是常量表达式，循环变量取不了。
 * 顺序与 `BLOOM_KERNEL_SIZES` / `MIP_UNITS` 一一对应。
 */
export const COMPOSITE_FRAG = /* glsl */ `#version 300 es
precision highp float;

uniform sampler2D uMips[5];
uniform float uStrength;
uniform float uRadius;

in vec2 vUv;

out vec4 outColor;

float lerpBloomFactor(float factor) {
  return mix(factor, 1.2 - factor, uRadius);
}

void main() {
  vec3 bloom = lerpBloomFactor(1.0) * texture(uMips[0], vUv).rgb +
               lerpBloomFactor(0.8) * texture(uMips[1], vUv).rgb +
               lerpBloomFactor(0.6) * texture(uMips[2], vUv).rgb +
               lerpBloomFactor(0.4) * texture(uMips[3], vUv).rgb +
               lerpBloomFactor(0.2) * texture(uMips[4], vUv).rgb;
  outColor = vec4(3.0 * uStrength * bloom, 1.0);
}
`
