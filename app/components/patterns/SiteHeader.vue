<script setup lang="ts">
// 站点顶栏。v3 的顶栏只有字标与语言切换器，没有导航链接。
// tools 插槽由使用方填入语言切换器（依赖 @nuxtjs/i18n，尚未接入）。
</script>

<template>
  <header class="site-header">
    <NuxtLink to="/" class="site-header__brand" aria-label="Nebula Studios">
      <svg class="site-header__mark" viewBox="0 0 256 256" fill="none" aria-hidden="true">
        <path d="M56 48H96L144 96L112 128L96 112V208H56V48Z" />
        <path d="M200 208H160L112 160L144 128L160 144V48H200V208Z" />
      </svg>
    </NuxtLink>

    <div class="site-header__tools">
      <slot name="tools" />
    </div>
  </header>
</template>

<style scoped>
.site-header {
  position: fixed;
  inset-inline: 0;
  top: 0;
  z-index: var(--z-header);
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: var(--header-h);
  padding: 0 var(--space-10);
  background: var(--bg);
  border-bottom: 1px solid var(--line);
}

.site-header__brand {
  display: inline-flex;
  align-items: center;
  color: var(--fg);
}

/* 字标尺寸与 60px 高的顶栏成比例，非刻度值 */
.site-header__mark {
  width: 28px;
  height: 28px;
  fill: currentColor;
}

@media (max-width: 640px) {
  .site-header {
    padding: 0 var(--space-4);
  }
}
</style>
