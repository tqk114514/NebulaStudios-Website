<template>
  <SiteHeader />
  <div class="default-layout">
    <slot />
  </div>
</template>

<style scoped>
/* 固定顶栏的偏移只在这里承担一次，页面与面板不再各自加 */
.default-layout {
  padding-top: var(--header-h);
}
</style>
