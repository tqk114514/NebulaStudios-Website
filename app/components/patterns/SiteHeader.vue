<script setup lang="ts">
// 站点顶栏。v3 的顶栏只有字标与语言切换器，没有导航链接。
// tools 插槽由使用方填入语言切换器（依赖 @nuxtjs/i18n，尚未接入）。
import { LOGO_PATHS, LOGO_VIEW_BOX } from '~/utils/logo-mark'
</script>

<template>
  <header class="site-header">
    <NuxtLink to="/" class="site-header__brand" aria-label="Nebula Studios">
      <svg
        class="site-header__mark"
        :viewBox="LOGO_VIEW_BOX"
        fill="none"
        aria-hidden="true">
        <path
          v-for="d in LOGO_PATHS"
          :key="d"
          :d="d"
        />
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
