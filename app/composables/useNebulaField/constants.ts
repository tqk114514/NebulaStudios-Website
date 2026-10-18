export const FOV = 45 // 透视相机 FOV
export const CAM_DIST = 7 // 相机到雕塑中心的距离
export const SPIN = 0.22 // 自旋角速度（rad/s）
export const TILT = 0.3 // 基础俯视角（rad）
export const SCAN_PERIOD = 6.5 // Logo 辉光扫描周期（s）
export const SCAN_RANGE = 1.35 // 扫描覆盖的 Logo 本地 X 范围
export const RING_SPAN = 4.1 // 星环外沿直径（世界单位，缩放前）
export const COMPACT_MAX_WIDTH = 900 // 该宽度以下锚点隐藏，雕塑居中缩小压暗

/**
 * 泛光预算：UnrealBloom 的模糊链最低级会把大块中亮区域摊成全屏均匀灰加回画面，
 * 近黑底色上哪怕 +0.03 线性亮度都读作蒙了一层白。因此 threshold 必须高到只有
 * 扫描波与最热的粒子重叠核进得了泛光链，主体亮度压在阈值之下保持锐利。
 * knee 是高通滤波的平滑膝宽：默认 0.01 的硬截止经模糊链后产生同心圈层伪影。
 * radius 拉高让各级模糊的光晕互相混合，削弱台阶感。
 */
export const BLOOM = { strength: 0.35, radius: 0.7, threshold: 0.8, knee: 0.55 }

/** 静态帧：prefers-reduced-motion 下渲染一次，姿态与时间取这两个值 */
export const STATIC_ROT_Y = 0.42
export const STATIC_TIME = 8.6
