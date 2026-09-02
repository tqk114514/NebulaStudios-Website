<script setup lang="ts">
// 管理后台单页（迁移自 modules/admin/pages/index.html + assets/js/admin.ts + assets/css/admin.css）。
// 独立全屏页面：App.vue 对 /admin 路由不渲染全局 SiteHeader，本布局自带完整结构。
// 单页结构：固定侧边栏（品牌/导航/页脚）+ 顶栏（移动端抽屉开关/页面标题/当前头像）+ 内容区。
// 区块切换与旧版一致走 location.hash（/admin#users），不走 vue-router 子路由；
// 六个区块全部静态导入，随主包加载，切换零延迟。
// 角色门控：操作日志所有管理员可见；OAuth/白名单/数据仅超管（与后端 SuperAdminMiddleware 对应，
// 页面入口显隐只是体验层，权限以后端中间件为准）。
// 样式：adm- 命名空间，令牌定义在 body.adm-page-open（见 pages/admin/admin-shared.css），
// 本组件的 scoped 样式直接引用这些变量，与前台样式完全解耦。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { Component } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { logout } from '@/api/auth'
import { CDN_URL } from '@/config/cdn'
import AdminHomePage from '@/pages/admin/AdminHomePage.vue'
import UsersPage from '@/pages/admin/UsersPage.vue'
import LogsPage from '@/pages/admin/LogsPage.vue'
import OAuthClientsPage from '@/pages/admin/OAuthClientsPage.vue'
import WhitelistPage from '@/pages/admin/WhitelistPage.vue'
import DataPage from '@/pages/admin/DataPage.vue'

interface NavItem {
  /** hash 名，与旧版 /admin#users 一致 */
  name: string
  key: string
  icon: string
  superOnly: boolean
  component: Component
}

// SVG path 迁移自旧版 index.html 侧边栏图标
const SECTIONS: NavItem[] = [
  {
    name: 'dashboard',
    key: 'admin.nav.dashboard',
    icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z',
    superOnly: false,
    component: AdminHomePage,
  },
  {
    name: 'users',
    key: 'admin.nav.users',
    icon: 'M16 11c1.66 0 2.99-1.34 2.99-3S17.66 5 16 5c-1.66 0-3 1.34-3 3s1.34 3 3 3zm-8 0c1.66 0 2.99-1.34 2.99-3S9.66 5 8 5C6.34 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z',
    superOnly: false,
    component: UsersPage,
  },
  {
    name: 'logs',
    key: 'admin.nav.logs',
    icon: 'M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z',
    superOnly: false,
    component: LogsPage,
  },
  {
    name: 'oauth',
    key: 'admin.nav.oauth',
    icon: 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z',
    superOnly: true,
    component: OAuthClientsPage,
  },
  {
    name: 'whitelist',
    key: 'admin.nav.whitelist',
    icon: 'M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z',
    superOnly: true,
    component: WhitelistPage,
  },
  {
    name: 'data',
    key: 'admin.nav.data',
    icon: 'M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z',
    superOnly: true,
    component: DataPage,
  },
]

const router = useRouter()
const auth = useAuthStore()

const sidebarOpen = ref(false)
const currentPage = ref('dashboard')

const visibleNav = computed(() => SECTIONS.filter((item) => !item.superOnly || auth.isSuperAdmin))

const pageTitle = computed(
  () => SECTIONS.find((item) => item.name === currentPage.value)?.key ?? 'admin.nav.dashboard',
)

const activeComponent = computed(
  () => SECTIONS.find((item) => item.name === currentPage.value)?.component ?? AdminHomePage,
)

/** hash 名是否可达（存在且非超管不可达超管区块） */
function isReachable(name: string): boolean {
  const item = SECTIONS.find((s) => s.name === name)
  return !!item && (!item.superOnly || auth.isSuperAdmin)
}

function applyHash(): void {
  const name = window.location.hash.replace(/^#/, '') || 'dashboard'
  if (isReachable(name)) currentPage.value = name
}

function onHashChange(): void {
  applyHash()
}

onMounted(() => {
  applyHash()
  window.addEventListener('hashchange', onHashChange)
  // 后台挂载期间给 body 打标记：令牌作用域 + 弹窗/Toast Teleport 到 body 外，
  // admin-shared.css 据此把共享组件也重绘为后台外观
  document.body.classList.add('adm-page-open')
})

onBeforeUnmount(() => {
  window.removeEventListener('hashchange', onHashChange)
  document.body.classList.remove('adm-page-open')
})

const avatarSrc = computed(() => {
  const u = auth.user
  if (!u) return ''
  if (u.avatar_url === 'microsoft') return u.microsoft_avatar_url ?? ''
  if (u.avatar_url === 'google') return u.google_avatar_url ?? ''
  return u.avatar_url || CDN_URL + '/images/default-avatar.svg'
})

async function handleLogout(): Promise<void> {
  try {
    await logout()
  } catch {
    // 登出接口失败不阻断本地清理
  }
  auth.clear()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="adm-shell">
    <!-- 侧边栏 -->
    <aside class="adm-side" :class="{ 'is-open': sidebarOpen }">
      <div class="adm-side-head">
        <h1 class="adm-brand">
          <span class="adm-brand-mark" aria-hidden="true"></span>
          <span class="adm-brand-name">Nebula</span>
          <span class="adm-brand-sub">Admin</span>
        </h1>
      </div>
      <nav class="adm-nav">
        <!-- hash 锚点：浏览器原生写 hash，hashchange 监听器负责切换（与旧版交互一致） -->
        <a
          v-for="item in visibleNav"
          :key="item.name"
          :href="`#${item.name}`"
          class="adm-nav-item"
          :class="{ active: currentPage === item.name }"
          @click="sidebarOpen = false"
        >
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path :d="item.icon" />
          </svg>
          <span>{{ $t(item.key) }}</span>
        </a>
      </nav>
      <div class="adm-side-foot">
        <RouterLink to="/account/dashboard" class="adm-nav-item" @click="sidebarOpen = false">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z" />
          </svg>
          <span>{{ $t('admin.nav.backToAccount') }}</span>
        </RouterLink>
        <button type="button" class="adm-nav-item adm-logout" @click="handleLogout">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z" />
          </svg>
          <span>{{ $t('admin.nav.logout') }}</span>
        </button>
      </div>
    </aside>

    <!-- 主内容区 -->
    <div class="adm-main">
      <!-- 顶栏 -->
      <header class="adm-topbar">
        <button
          type="button"
          class="adm-toggle"
          :aria-label="$t('admin.nav.dashboard')"
          @click="sidebarOpen = !sidebarOpen"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
            <path d="M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z" />
          </svg>
        </button>
        <div class="adm-topbar-title">{{ $t(pageTitle) }}</div>
        <div class="adm-avatar">
          <img v-if="avatarSrc" :src="avatarSrc" :alt="auth.user?.username ?? ''" />
        </div>
      </header>

      <!-- 页面内容 -->
      <div class="adm-content" @click="sidebarOpen = false">
        <component :is="activeComponent" />
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 壳层（令牌来自 body.adm-page-open，见 admin-shared.css） ---- */
.adm-shell {
  display: flex;
  align-items: stretch;
  min-height: 100vh;
  background: var(--adm-canvas);
  color: var(--adm-ink);
  font-family: var(--adm-font);
  font-weight: 400;
  letter-spacing: normal;
}

/* ---- 侧边栏 ---- */
.adm-side {
  position: fixed;
  top: 0;
  bottom: 0;
  left: 0;
  width: var(--adm-side-w);
  background: var(--adm-surface);
  border-right: 1px solid var(--adm-line);
  display: flex;
  flex-direction: column;
  z-index: 100;
  transition: transform 0.25s ease;
}

.adm-side-head {
  height: var(--adm-header-h);
  padding: 0 20px;
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--adm-line);
  flex-shrink: 0;
}

/* 品牌：墨色方块标记 + 字重对比 */
.adm-brand {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--adm-ink);
}

.adm-brand-mark {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  background: var(--adm-ink);
}

.adm-brand-sub {
  font-weight: 400;
  color: var(--adm-ink-3);
}

.adm-nav {
  flex: 1;
  padding: 8px;
  overflow-y: auto;
  scrollbar-width: none;
}

.adm-nav::-webkit-scrollbar {
  display: none;
}

.adm-nav-item {
  position: relative;
  width: 100%;
  height: 36px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 10px 0 12px;
  border: none;
  border-radius: var(--adm-radius-s);
  background: none;
  color: var(--adm-ink-2);
  font-family: inherit;
  font-size: 13px;
  font-weight: 400;
  text-align: left;
  cursor: pointer;
  transition: background var(--adm-dur), color var(--adm-dur);
}

.adm-nav-item svg {
  flex-shrink: 0;
  color: var(--adm-ink-3);
  transition: color var(--adm-dur);
}

.adm-nav-item:hover {
  background: var(--adm-surface-2);
  color: var(--adm-ink);
}

.adm-nav-item:hover svg {
  color: var(--adm-ink-2);
}

.adm-nav-item.active {
  background: var(--adm-surface-3);
  color: var(--adm-ink);
  font-weight: 500;
}

.adm-nav-item.active svg {
  color: var(--adm-ink);
}

/* 激活项左侧的墨色指示条 */
.adm-nav-item.active::before {
  content: '';
  position: absolute;
  left: 2px;
  top: 9px;
  bottom: 9px;
  width: 2px;
  border-radius: 1px;
  background: var(--adm-ink);
}

.adm-side-foot {
  padding: 8px;
  border-top: 1px solid var(--adm-line);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* 登出：默认与导航同级的弱化项，hover 才浮出危险色 */
.adm-logout {
  color: var(--adm-ink-3);
}

.adm-logout:hover {
  background: var(--adm-danger-bg);
  color: var(--adm-danger);
}

.adm-logout:hover svg {
  color: var(--adm-danger);
}

/* ---- 主内容区 ---- */
.adm-main {
  flex: 1;
  margin-left: var(--adm-side-w);
  min-width: 0;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}

/* ---- 顶栏 ---- */
.adm-topbar {
  position: sticky;
  top: 0;
  z-index: 50;
  height: var(--adm-header-h);
  background: var(--adm-surface);
  border-bottom: 1px solid var(--adm-line);
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 24px;
}

/* 标题前的墨色方块：与品牌标记同一节奏 */
.adm-topbar-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--adm-ink);
}

.adm-topbar-title::before {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background: var(--adm-ink);
}

.adm-toggle {
  display: none;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: var(--adm-radius-s);
  background: none;
  color: var(--adm-ink-2);
  cursor: pointer;
  transition: background var(--adm-dur), color var(--adm-dur);
}

.adm-toggle:hover {
  background: var(--adm-surface-2);
  color: var(--adm-ink);
}

.adm-avatar {
  margin-left: auto;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  overflow: hidden;
  border: 1px solid var(--adm-line);
  background: var(--adm-surface-2);
  flex-shrink: 0;
}

.adm-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

/* ---- 页面容器：限宽居中，超宽屏不无限拉伸 ---- */
.adm-content {
  flex: 1;
  width: 100%;
  max-width: 1240px;
  margin: 0 auto;
  padding: 24px 32px 48px;
}

/* ---- 响应式：移动端抽屉 ---- */
@media (max-width: 768px) {
  .adm-side {
    transform: translateX(-100%);
  }

  .adm-side.is-open {
    transform: translateX(0);
  }

  .adm-main {
    margin-left: 0;
  }

  .adm-toggle {
    display: flex;
  }

  .adm-content {
    padding: 16px 16px 40px;
  }
}
</style>
