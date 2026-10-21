/** 底色铺满画布。走几何体而非清屏色：清屏路径的色彩约定与这条链不同源，混用会二次提亮。 */
export const BACKGROUND_FRAG = /* glsl */ `#version 300 es
precision highp float;

uniform vec3 uColor;

out vec4 outColor;

void main() {
  outColor = vec4(uColor, 1.0);
}
`

/**
 * 末趟：线性场景 + 线性泛光相加，再做 OETF，最后叠 ±1/255 的微噪声。
 * 相加必须发生在编码之前——泛光是亮度叠加，编码后再加就改了伽马曲线下的每一级。
 * 抖动同理：色带是 8bit 输出量化产生的，作用在线性值上等于没做。
 */
export const OUTPUT_FRAG = /* glsl */ `#version 300 es
precision highp float;

uniform sampler2D uSource;
uniform sampler2D uBloom;
uniform float uTime;

in vec2 vUv;

out vec4 outColor;

float toSrgb(float channel) {
  float c = clamp(channel, 0.0, 1.0);
  return c <= 0.0031308 ? c * 12.92 : 1.055 * pow(c, 1.0 / 2.4) - 0.055;
}

float hash(vec2 p) {
  vec3 p3 = fract(vec3(p.xyx) * 0.1031);
  p3 += dot(p3, p3.yzx + 33.33);
  return fract((p3.x + p3.y) * p3.z);
}

void main() {
  vec3 linear = texture(uSource, vUv).rgb + texture(uBloom, vUv).rgb;
  vec3 encoded = vec3(toSrgb(linear.r), toSrgb(linear.g), toSrgb(linear.b));
  float n = hash(gl_FragCoord.xy + vec2(fract(uTime) * 913.0, fract(uTime * 0.618) * 517.0));
  outColor = vec4(encoded + (n - 0.5) * (2.0 / 255.0), 1.0);
}
`
