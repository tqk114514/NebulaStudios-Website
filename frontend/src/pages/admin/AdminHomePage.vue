<script setup lang="ts">
// 管理后台仪表盘（迁移自 modules/admin/assets/js/stats.ts + index.html 仪表盘 section）。
// 四项统计：总用户数 / 今日新增 / 管理员数 / 封禁用户，来自 GET /admin/api/stats。
// 视觉：排版驱动的统计横带——纯数字（tabular-nums）+ 小型大写标签，垂直发丝线分隔，
// 无图标无彩色底；封禁数大于零时数字转危险色作为健康警示。
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
  <div v-if="failed" class="adm-banner">{{ $t('admin.stats.loadFailed') }}</div>

  <div v-else class="adm-stats">
    <div class="adm-stat">
      <span class="adm-stat-value">{{ stats?.totalUsers ?? '-' }}</span>
      <span class="adm-stat-label">{{ $t('admin.stats.totalUsers') }}</span>
    </div>
    <div class="adm-stat">
      <span class="adm-stat-value adm-stat-value--pulse">{{ stats?.todayNewUsers ?? '-' }}</span>
      <span class="adm-stat-label">{{ $t('admin.stats.todayNewUsers') }}</span>
    </div>
    <div class="adm-stat">
      <span class="adm-stat-value">{{ stats?.adminCount ?? '-' }}</span>
      <span class="adm-stat-label">{{ $t('admin.stats.adminCount') }}</span>
    </div>
    <div class="adm-stat">
      <span
        class="adm-stat-value"
        :class="{ 'adm-stat-value--alert': (stats?.bannedCount ?? 0) > 0 }"
      >{{ stats?.bannedCount ?? '-' }}</span>
      <span class="adm-stat-label">{{ $t('admin.stats.bannedCount') }}</span>
    </div>
  </div>
</template>

<style scoped>
/* ---- 统计横带：整条白卡由发丝线切分为四格（令牌来自 body.adm-page-open） ---- */
.adm-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  background: var(--adm-surface);
  border: 1px solid var(--adm-line);
  border-radius: var(--adm-radius);
  overflow: hidden;
}

.adm-stat {
  padding: 20px 24px;
}

.adm-stat + .adm-stat {
  border-left: 1px solid var(--adm-line);
}

.adm-stat-value {
  display: block;
  font-size: 30px;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
  color: var(--adm-ink);
}

/* 今日新增：数字后的增长点（唯一一处“正向”色彩语义） */
.adm-stat-value--pulse::after {
  content: '';
  display: inline-block;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--adm-success);
  margin-left: 8px;
  vertical-align: 0.12em;
}

/* 封禁数大于零：数字转危险色 */
.adm-stat-value--alert {
  color: var(--adm-danger);
}

.adm-stat-label {
  display: block;
  margin-top: 6px;
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--adm-ink-3);
}

/* ---- 加载失败横幅 ---- */
.adm-banner {
  padding: 14px 16px;
  background: var(--adm-warning-bg);
  border: 1px solid #ead9ac;
  border-radius: var(--adm-radius);
  color: var(--adm-warning);
  font-size: 13px;
}

/* ---- 响应式：两列网格，边线随列位切换 ---- */
@media (max-width: 768px) {
  .adm-stats {
    grid-template-columns: repeat(2, 1fr);
  }

  .adm-stat:nth-child(2n) {
    border-left: 1px solid var(--adm-line);
  }

  .adm-stat:nth-child(2n + 1) {
    border-left: none;
  }

  .adm-stat:nth-child(n + 3) {
    border-top: 1px solid var(--adm-line);
  }
}
</style>
