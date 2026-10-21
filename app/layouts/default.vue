<template>
  <div class="default-layout">
    <SiteHeader>
      <template #tools>
        <LangSwitcher />
      </template>
    </SiteHeader>
    <slot />
    <SiteFooter />
  </div>
</template>

<style scoped>
/*
 * 固定顶栏的偏移只在这里承担一次，页面与面板不再各自加。
 * 列向 flex + 满视口最小高度：内容不足一屏时底栏仍贴住下沿，而不是浮在半空。
 */
.default-layout {
  display: flex;
  flex-direction: column;
  min-height: 100svh;
  padding-top: var(--header-h);
}
</style>
