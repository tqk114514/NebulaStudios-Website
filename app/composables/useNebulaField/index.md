# useNebulaField

在指定区域内渲染 WebGL 粒子星云：N 字标粒子雕塑、三重星环、外围星尘与全视锥背景星场。
透视相机 + 自持 WebGL2 点精灵着色器，入场汇聚、轨道运动、闪烁、辉光扫描与透视点尺寸
全部在顶点着色器每帧计算；后期链为 `背景填充 → 粒子层 → 高通 → 五级高斯 mip → 加权合成 → OETF + 抖动`
（见 ADR-0001，不依赖 three）。

仅客户端执行。WebGL2 不可用时静默跳过；`prefers-reduced-motion` 下渲染一帧静态图后不进循环。
hero 滚出视口或标签页隐藏时跳过渲染，rAF 保持心跳。

## 入场

`intro` 从 0 推进到 1 用时 `INTRO_DURATION`，之后进入巡航。时钟只在真正出帧的帧上累加，
所以中途滚出视口会暂停、回来接着播，不会跳过一段。

每个粒子带 `aScatter`（球壳上的起点）与 `aDelay`（错峰量，`[0, INTRO_STAGGER]`），
本地进度 `q = (intro - aDelay) / INTRO_TRAVEL`，`q = 1` 那一瞬记作落地闪光。
延迟分配：星尘 0–0.05 → 星环按环序 0.06–0.24 → 字标 0.28–0.55 且按 x 自左向右，
读作"先有星尘再有字"。自旋速度由 `spinScale(intro)` 从 `INTRO_SPIN_FLOOR` 抬到 1。

`INTRO_STAGGER + INTRO_TRAVEL × 1.2 < 1` 是硬约束：闪光窗口在 `q ∈ [0.8, 1.2]`，
最后一批粒子必须在 `intro` 触顶前把窗口走完，否则它们会停在峰值上——字标右缘常驻
一层额外亮度，扫描带扫到右侧时叠成一条过亮的光带。`nebula-intro.test.ts` 守着这条。

## 用法

hero 元素目前直接写在页面里（`app/pages/index.vue`），需要换承载元素时传三个 ref：

```ts
useNebulaField({ heroRef, canvasRef, anchorRef })
```

- `heroRef`：承载画布与视差判界的元素，需 `position: relative` 的父级
- `canvasRef`：由本组合式接管的 `<canvas>`
- `anchorRef`：雕塑右锚点，其尺寸决定雕塑比例；宽度为 0 时雕塑退化为居中、缩小、压暗

## 约束

- 底色与粒子色读自 CSS 令牌（`--bg`、`--fg`、`--nebula-ring`），不写死
- 泛光阈值 `0.8` 与粒子 alpha 是一对耦合值：提高单体亮度会让整个字标成为"大块中亮区域"，
  经模糊链摊成全屏白雾；两者需一同复核。落地闪光是瞬时的、逐粒子的小面积热核，不在此列
- `aScatter` 与 `aDelay` 是 `ParticleBuilder` 的必填参数：缺了会在 WebGL 里退化成
  `(0,0,0)` 默认值，星场粒子会全部先聚到原点
- `COMPACT_MAX_WIDTH` 与 hero 里锚点的媒体查询断点必须一致

