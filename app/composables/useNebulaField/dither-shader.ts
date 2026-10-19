/**
 * 抖动：Bloom 光晕在暗部渐变上会被 8bit 输出量化成一圈圈的色带，
 * 在 OutputPass 之后叠一层随时间变化的微噪声打散量化台阶。
 * 幅度 ±1/255，只作用于 3D 画布（文字都是 DOM）。
 */
export const DITHER_SHADER = {
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
