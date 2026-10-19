# useTicker

跑马灯的轨道条目数与动画时长。接缝由构造保证：两条等宽轨道并排，各自平移自身宽度的
-100%，复位点正是相邻轨道的起点，因此接缝精度与任何测量无关。

```ts
const rootRef = ref<HTMLElement | null>(null)
const { duration, itemCount } = useTicker(rootRef)
```

组件侧把 `duration` 写成 `--ticker-duration`，`itemCount` 决定每条轨道的条目数；
两条轨道内容完全相同。

## 两个坑各自落在哪里

- **覆盖**：周期末尾第一条轨道完全移出，视口全靠第二条顶着，所以轨道总宽必须
  ≥ 视口宽 × `TICKER_COVERAGE`。取 2 让横向拉伸窗口到两倍以内都不必重排。
  超出时由 ResizeObserver 重测条目数。
- **重排引起的瞬移**：CSS 动画的进度按 `elapsed / duration` 算，改 `duration` 会
  跳相位。屏速恒为 `TICKER_SPEED`（时长 = 轨道宽 ÷ 屏速，位移量 = 轨道宽），
  所以只在条目数真的变了时才改时长，字体换载与 resize 之外不会发生。

字体度量决定条目宽度，`document.fonts.ready` 后重测一次；首帧之前的近似时长由
CSS 里的回退值承担。

## 约束

- 条目左右等距（`padding: 0 var(--space-12)`），轨道首尾相接处才会与条目间距一致；
  改成 margin 或给容器加 gap 都会造出一条可见的缝
- 根元素的第一个子节点必须是一条轨道，`measure()` 靠它取宽度
