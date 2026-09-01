<script setup lang="ts">
// 管理后台仪表盘（迁移自 modules/admin/assets/js/stats.ts + index.html 仪表盘 section）。
// 四项统计：总用户数 / 今日新增 / 管理员数 / 封禁用户，来自 GET /admin/api/stats。
import { onMounted, ref } from 'vue'
import { fetchAdminStats, type AdminStats } from '@/api/admin'

const stats = ref<AdminStats | null>(null)
const failed = ref(false)

onMounted(async () => {
  try {
    stats.value = await fetchAdminStats()
  } catch {
    failed.value = true
  }
})
</script>

<template>
  <div v-if="failed" class="admin-stats-failed">{{ $t('admin.stats.loadFailed') }}</div>

  <div v-else class="stats-grid">
    <div class="stat-card">
      <div class="stat-icon users">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
          <path d="M16 11c1.66 0 2.99-1.34 2.99-3S17.66 5 16 5c-1.66 0-3 1.34-3 3s1.34 3 3 3zm-8 0c1.66 0 2.99-1.34 2.99-3S9.66 5 8 5C6.34 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ stats?.totalUsers ?? '-' }}</span>
        <span class="stat-label">{{ $t('admin.stats.totalUsers') }}</span>
      </div>
    </div>

    <div class="stat-card">
      <div class="stat-icon new">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
          <path d="M15 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm-9-2V7H4v3H1v2h3v3h2v-3h3v-2H6zm9 4c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ stats?.todayNewUsers ?? '-' }}</span>
        <span class="stat-label">{{ $t('admin.stats.todayNewUsers') }}</span>
      </div>
    </div>

    <div class="stat-card">
      <div class="stat-icon admin">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
          <path d="M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ stats?.adminCount ?? '-' }}</span>
        <span class="stat-label">{{ $t('admin.stats.adminCount') }}</span>
      </div>
    </div>

    <div class="stat-card">
      <div class="stat-icon banned">
        <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm5 11H7v-2h10v2z" />
        </svg>
      </div>
      <div class="stat-info">
        <span class="stat-value">{{ stats?.bannedCount ?? '-' }}</span>
        <span class="stat-label">{{ $t('admin.stats.bannedCount') }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 统计卡片（迁移自 admin.css .stats-grid / .stat-*，值不变） ---- */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 20px;
}

.stat-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 24px;
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.stat-icon.users {
  background: rgba(99, 102, 241, 0.15);
  color: var(--accent);
}
.stat-icon.new {
  background: rgba(34, 197, 94, 0.15);
  color: var(--success);
}
.stat-icon.admin {
  background: rgba(245, 158, 11, 0.15);
  color: var(--warning);
}
.stat-icon.banned {
  background: rgba(239, 68, 68, 0.15);
  color: var(--danger);
}

.stat-info {
  display: flex;
  flex-direction: column;
}

.stat-value {
  font-size: 1.75rem;
  font-weight: 600;
  line-height: 1.2;
}

.stat-label {
  font-size: 0.875rem;
  color: var(--text-secondary);
}

.admin-stats-failed {
  color: var(--danger);
  font-size: 0.875rem;
}

@media (max-width: 768px) {
  .stats-grid {
    grid-template-columns: 1fr;
  }
}
</style>
