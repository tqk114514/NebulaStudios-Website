/** Float32Array 直接就是 uniform3fv 的入参形状，不再另做转换。 */
export type LinearColor = Float32Array

/**
 * sRGB 分量（[0,1]）→ 线性工作空间。与 three r152 起的色彩管理同一公式，
 * 色值必须在进着色器前完成这一步：泛光在线性空间算，最终输出再一次性转回 sRGB。
 */
export function srgbToLinear(channel: number): number {
  return channel < 0.04045
    ? channel / 12.92
    : Math.pow(channel * 0.9478672986 + 0.0521327014, 2.4)
}

/**
 * `#rgb` / `#rrggbb` → 线性三分量。CSS 令牌此刻只用十六进制，
 * 其余写法（rgb()、color()）需要另一套解析，出现时再补。
 */
export function hexToLinear(value: string): LinearColor {
  const hex = value.trim().replace('#', '')
  const full = hex.length === 3 ? hex.replace(/./g, (c) => c + c) : hex
  const out = new Float32Array(3)
  ;[0, 2, 4].forEach((i, channel) => {
    out[channel] = srgbToLinear(Number.parseInt(full.slice(i, i + 2), 16) / 255)
  })
  return out
}

/** 读取根元素上的颜色令牌；只在客户端路径调用，SSR 下没有 getComputedStyle。 */
export function readToken(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}
