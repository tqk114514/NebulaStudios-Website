# useReveal

给区域内的块加上滚动显形：进入视口一次后播放并取消观察，之后不再干预。

```vue
<template>
  <div ref="rootRef">
    <p data-reveal>…</p>
    <p data-reveal="1">…</p>
    <div data-reveal="2">…</div>
  </div>
</template>

<script setup lang="ts">
const rootRef = ref<HTMLElement | null>(null)
useReveal(rootRef)
```

`data-reveal` 的值是错峰步数，乘以 `--stagger` 得到延迟。位移与时长取 `--shift-reveal`
与 `--dur-reveal`，缓动取 `--ease-emphasize`。

隐藏态由本组合式在挂载时写入，不放在 CSS 默认值里：脚本没执行（禁用 JS、报错、
爬虫首屏）时正文必须可读。`prefers-reduced-motion: reduce` 下整个机制跳过。
