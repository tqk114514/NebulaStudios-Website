<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { RouterView } from 'vue-router'
import SiteHeader from '@/components/SiteHeader.vue'
import AppToast from '@/components/AppToast.vue'
import PageLoader from '@/components/PageLoader.vue'
import PolicyConsentModal from '@/components/PolicyConsentModal.vue'

// 管理后台是独立全屏页面（老版即无站头），不渲染全局 SiteHeader
const route = useRoute()
const showSiteHeader = computed(() => !route.path.startsWith('/admin'))
</script>

<template>
  <!-- 原站所有页面都有顶部导航栏（品牌 + 语言切换）；管理后台独立布局除外 -->
  <SiteHeader v-if="showSiteHeader" />
  <RouterView />
  <!-- 路由切换/初始加载遮罩（懒加载 chunk 期间立即反馈） -->
  <PageLoader />
  <AppToast />
  <PolicyConsentModal />
</template>