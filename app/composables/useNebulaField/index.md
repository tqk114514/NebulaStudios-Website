# useNebulaField

在指定区域内渲染 WebGL 粒子星云：N 字标粒子雕塑、三重星环、外围星尘与全视锥背景星场。
透视相机 + 自定义 `ShaderMaterial` 点精灵，轨道运动、闪烁、辉光扫描与透视点尺寸全部在
顶点着色器每帧计算；后期链为 `RenderPass → UnrealBloomPass → OutputPass → 抖动`。

仅客户端执行。WebGL2 不可用时静默跳过；`prefers-reduced-motion` 下渲染一帧静态图后不进循环。
hero 滚出视口或标签页隐藏时跳过渲染，rAF 保持心跳。

## 用法

```ts
import { NebulaHero } from '#components'
```

页面侧通常直接用 `NebulaHero` 组件；需要自定义 hero 元素时才直接调本 composable：

```ts
useNebulaField({ heroRef, canvasRef, anchorRef })
```

- `heroRef`：承载画布与视差判界的元素，需 `position: relative` 的父级
- `canvasRef`：由本组合式接管的 `<canvas>`
- `anchorRef`：雕塑右锚点，其尺寸决定雕塑比例；宽度为 0 时雕塑退化为居中、缩小、压暗

## 约束

- 底色与粒子色读自 CSS 令牌（`--bg`、`--fg`、`--nebula-ring`），不写死
- 泛光阈值 `0.8` 与粒子 alpha 是一对耦合值：提高单体亮度会让整个字标成为"大块中亮区域"，
  经模糊链摊成全屏白雾。调 alpha 必须同时复核 BLOOM.threshold
- `COMPACT_MAX_WIDTH` 与 `NebulaHero.vue` 里锚点的媒体查询断点必须一致
