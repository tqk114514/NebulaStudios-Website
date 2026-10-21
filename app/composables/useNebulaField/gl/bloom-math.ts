/** UnrealBloom 的五级 mip 卷积半径；权重表在合成着色器里，与此一一对应 */
export const BLOOM_KERNEL_SIZES = [6, 10, 14, 18, 22] as const

export interface MipSize {
  width: number
  height: number
}

/** 逐级对半；奇数边长向上取整，取整规则只在这里定义一次 */
function halfOf(size: MipSize): MipSize {
  return { width: Math.round(size.width / 2), height: Math.round(size.height / 2) }
}

/**
 * 第 depth 级模糊的尺寸：场景尺寸连续对半 depth 次。depth 取 0 即原尺寸。
 * 构造与 resize 都走这里，故各级之间的比例关系只有一处定义。
 */
export function mipSize(width: number, height: number, depth: number): MipSize {
  let size = { width, height }
  for (let level = 0; level < depth; level++) size = halfOf(size)
  return size
}

export interface GaussianKernel {
  centerWeight: number
  offsets: number[]
  weights: number[]
}

/**
 * 一维高斯的抽样表，相邻两个抽样合并为一次双线性取样，故 pairs 数为 floor(radius/2)。
 * 权重不做归一化：与 three 一致，模糊后的能量按未归一化的 PDF 相加，
 * 归一化会改变亮度，进而改变 threshold 的实际落点。
 */
export function gaussianKernel(radius: number): GaussianKernel {
  const sigma = radius / 3
  const pdf = (i: number): number => (0.39894 * Math.exp((-0.5 * i * i) / (sigma * sigma))) / sigma

  const offsets: number[] = []
  const weights: number[] = []
  for (let i = 1; i < radius; i += 2) {
    const wa = pdf(i)
    const wb = i + 1 < radius ? pdf(i + 1) : 0
    const weight = wa + wb
    offsets.push((i * wa + (i + 1) * wb) / weight)
    weights.push(weight)
  }

  return { centerWeight: pdf(0), offsets, weights }
}
