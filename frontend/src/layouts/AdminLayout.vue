<script setup lang="ts">
// 管理后台布局壳（迁移自 modules/admin/pages/index.html + assets/js/admin.ts + assets/css/admin.css）。
// 独立全屏页面：App.vue 对 /admin 路由不渲染全局 SiteHeader，本布局自带完整结构。
// 结构：固定侧边栏（品牌/导航/页脚）+ 顶栏（移动端抽屉开关/页面标题/当前头像）+ RouterView。
// 角色门控：操作日志所有管理员可见；OAuth/白名单/数据仅超管（与后端 SuperAdminMiddleware 对应，
// 页面入口显隐只是体验层，权限以后端中间件为准）。
// 设计令牌：老版后台独立配色（common.css :root，靛蓝 accent 系）桥接在 .admin-layout 上，
// 子页面沿用同一套变量名，CSS 值迁移保持不变。
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { logout } from '@/api/auth'
import { CDN_URL } from '@/config/cdn'

interface NavItem {
  name: string
  key: string
  icon: string
  superOnly: boolean
  /** 阶段迁移中：对应页面就绪后置 true */
  enabled: boolean
}

// SVG path 迁移自旧版 index.html 侧边栏图标
const NAV_ITEMS: NavItem[] = [
  {
    name: 'admin',
    key: 'admin.nav.dashboard',
    icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z',
    superOnly: false,
    enabled: true,
  },
  {
    name: 'admin-users',
    key: 'admin.nav.users',
    icon: 'M16 11c1.66 0 2.99-1.34 2.99-3S17.66 5 16 5c-1.66 0-3 1.34-3 3s1.34 3 3 3zm-8 0c1.66 0 2.99-1.34 2.99-3S9.66 5 8 5C6.34 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z',
    superOnly: false,
    enabled: false,
  },
  {
    name: 'admin-logs',
    key: 'admin.nav.logs',
    icon: 'M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z',
    superOnly: false,
    enabled: false,
  },
  {
    name: 'admin-oauth',
    key: 'admin.nav.oauth',
    icon: 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z',
    superOnly: true,
    enabled: false,
  },
  {
    name: 'admin-whitelist',
    key: 'admin.nav.whitelist',
    icon: 'M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z',
    superOnly: true,
    enabled: false,
  },
  {
    name: 'admin-data',
    key: 'admin.nav.data',
    icon: 'M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z',
    superOnly: true,
    enabled: false,
  },
]

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const sidebarOpen = ref(false)

const visibleNav = computed(() =>
  NAV_ITEMS.filter((item) => item.enabled && (!item.superOnly || auth.isSuperAdmin)),
)

const pageTitle = computed(() => {
  const key = route.meta.title
  return typeof key === 'string' && key ? key : 'admin.nav.dashboard'
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
  <div class="admin-layout">
    <!-- 侧边栏 -->
    <aside class="admin-sidebar" :class="{ 'is-open': sidebarOpen }">
      <div class="admin-sidebar-header">
        <h1 class="admin-sidebar-title">Nebula Admin</h1>
      </div>
      <nav class="admin-sidebar-nav">
        <RouterLink
          v-for="item in visibleNav"
          :key="item.name"
          :to="{ name: item.name }"
          class="admin-nav-item"
          active-class="active"
          @click="sidebarOpen = false"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
            <path :d="item.icon" />
          </svg>
          <span>{{ $t(item.key) }}</span>
        </RouterLink>
      </nav>
      <div class="admin-sidebar-footer">
        <RouterLink to="/account/dashboard" class="admin-nav-item" @click="sidebarOpen = false">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
            <path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z" />
          </svg>
          <span>{{ $t('admin.nav.backToAccount') }}</span>
        </RouterLink>
        <button type="button" class="admin-nav-item admin-logout-btn" @click="handleLogout">
          <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
            <path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z" />
          </svg>
          <span>{{ $t('admin.nav.logout') }}</span>
        </button>
      </div>
    </aside>

    <!-- 主内容区 -->
    <div class="admin-main">
      <!-- 顶栏 -->
      <header class="admin-topbar">
        <button
          type="button"
          class="admin-sidebar-toggle"
          @click="sidebarOpen = !sidebarOpen"
        >
          <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
            <path d="M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z" />
          </svg>
        </button>
        <div class="admin-topbar-title">{{ $t(pageTitle) }}</div>
        <div class="admin-topbar-avatar">
          <img v-if="avatarSrc" :src="avatarSrc" :alt="auth.user?.username ?? ''" />
        </div>
      </header>

      <!-- 页面内容 -->
      <div class="admin-page-container" @click="sidebarOpen = false">
        <RouterView />
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 设计令牌桥接（迁移自 common.css :root，值不变；子页面沿用同一套变量名） ---- */
.admin-layout {
  --bg-primary: #0a0a0f;
  --bg-secondary: #12121a;
  --bg-card: #1a1a24;
  --bg-hover: #22222e;
  --text-primary: #ffffff;
  --text-secondary: #a0a0b0;
  --text-muted: #606070;
  --border: #2a2a3a;
  --accent: #6366f1;
  --accent-hover: #818cf8;
  --success: #22c55e;
  --warning: #f59e0b;
  --danger: #ef4444;
  --sidebar-width: 240px;
  --topbar-height: 60px;
  --transition: 0.2s ease;

  display: flex;
  align-items: stretch;
  min-height: 100vh;
  background: var(--bg-primary);
  color: var(--text-primary);
}

/* ---- 侧边栏（迁移自 admin.css .sidebar，值不变） ---- */
.admin-sidebar {
  position: fixed;
  top: 0;
  bottom: 0;
  left: 0;
  width: var(--sidebar-width);
  background: var(--bg-secondary);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  z-index: 100;
  transition: transform var(--transition);
}

.admin-sidebar-header {
  height: var(--topbar-height);
  padding: 0 20px;
  display: flex;
  align-items: center;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.admin-sidebar-title {
  font-size: 1.25rem;
  font-weight: 600;
  color: var(--text-primary);
}

.admin-sidebar-nav {
  flex: 1;
  padding: 12px;
  overflow-y: auto;
  scrollbar-width: none;
}

.admin-sidebar-nav::-webkit-scrollbar {
  display: none;
}

.admin-nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: 8px;
  color: var(--text-secondary);
  transition: all var(--transition);
  margin-bottom: 4px;
  font-size: 0.9375rem;
  text-align: left;
}

.admin-nav-item:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.admin-nav-item.active {
  background: var(--accent);
  color: var(--text-primary);
}

.admin-nav-item svg {
  flex-shrink: 0;
}

.admin-sidebar-footer {
  padding: 12px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.admin-logout-btn {
  width: 100%;
  color: var(--danger);
}

.admin-logout-btn:hover {
  background: rgba(239, 68, 68, 0.1);
  color: var(--danger);
}

/* ---- 主内容区 ---- */
.admin-main {
  flex: 1;
  margin-left: var(--sidebar-width);
  min-width: 0;
  min-height: 100vh;
}

/* ---- 顶栏（迁移自 admin.css .topbar，值不变） ---- */
.admin-topbar {
  position: sticky;
  top: 0;
  height: var(--topbar-height);
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  padding: 0 24px;
  gap: 16px;
  z-index: 50;
}

.admin-sidebar-toggle {
  display: none;
  padding: 8px;
  border-radius: 8px;
  color: var(--text-secondary);
  transition: all var(--transition);
}

.admin-sidebar-toggle:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.admin-topbar-title {
  font-size: 1.125rem;
  font-weight: 500;
}

.admin-topbar-avatar {
  margin-left: auto;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  overflow: hidden;
  flex-shrink: 0;
}

.admin-topbar-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

/* ---- 页面容器 ---- */
.admin-page-container {
  padding: 24px;
}

/* ---- 响应式（迁移自 admin.css @media，值不变） ---- */
@media (max-width: 768px) {
  .admin-sidebar {
    transform: translateX(-100%);
  }

  .admin-sidebar.is-open {
    transform: translateX(0);
  }

  .admin-main {
    margin-left: 0;
  }

  .admin-sidebar-toggle {
    display: block;
  }
}
</style>
