<script setup lang="ts">
// 操作日志（仅超级管理员可见，路由 + 后端双重控制）。
// details 为统一信封结构：{ summary: {...}, changes?: { field: {old, new} } }。
// 变更类动作逐字段渲染 old -> new。
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Pagination from './Pagination.vue'
import { fetchAdminLogs, type AdminLogEntry } from '@/api/admin'
import { ApiClientError } from '@/api/client'
import { formatDate } from './admin-utils'
import './admin-shared.css'

const { t, te } = useI18n()

const logs = ref<AdminLogEntry[]>([])
const page = ref(1)
const totalPages = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)

function actionText(action: string): string {
  const key = `admin.logs.action.${action}`
  return te(key, 'zh-CN') ? t(key) : action
}

function roleText(role: number): string {
  return t(role === 2 ? 'admin.users.role.superAdmin' : role === 1 ? 'admin.users.role.admin' : 'admin.users.role.user')
}

// ---- summary：动作对象的人类可读描述 ----
function summaryText(log: AdminLogEntry): string {
  const d = log.details as Record<string, any> | undefined
  if (!d || typeof d !== 'object') return '-'

  const s = d.summary
  if (!s || typeof s !== 'object') return '-'

  const name = s.target_username ?? s.name ?? s.domain ?? ''
  if (log.action === 'delete_user') {
    return `${name}（${s.target_email ?? '-'}）`
  }
  if (log.action === 'ban_user') {
    const duration = s.unban_at ? formatDate(String(s.unban_at)) : t('admin.users.detail.permanentBan')
    return `${name} · ${t('admin.logs.details.banInfo', { reason: String(s.reason ?? ''), duration })}`
  }
  if (log.action === 'data_export' || log.action === 'data_import') {
    const users = Number(s.users_count ?? s.users_imported ?? 0)
    const logsCount = Number(s.logs_count ?? s.logs_imported ?? 0)
    return t('admin.logs.details.dataStats', { users, logs: logsCount })
  }
  if (s.client_id) return `${name}（${s.client_id}）`
  return String(name || '-')
}

// ---- changes：字段级变更 old -> new ----
interface FieldChangeVM {
  label: string
  old: string
  new: string
}

function fieldValue(field: string, v: unknown): string {
  if (v === null || v === undefined || v === '') return t('admin.logs.value.empty')
  if (typeof v === 'boolean') return v ? t('admin.logs.value.enabled') : t('admin.logs.value.disabled')
  if (field === 'role') return roleText(Number(v))
  return String(v)
}

function changeVMs(log: AdminLogEntry): FieldChangeVM[] {
  const d = log.details as Record<string, any> | undefined
  const changes = d?.changes
  if (!changes || typeof changes !== 'object') return []
  return Object.entries(changes as Record<string, { old: unknown; new: unknown }>).map(([field, c]) => {
    const key = `admin.logs.change.${field}`
    return {
      label: te(key, 'zh-CN') ? t(key) : field,
      old: fieldValue(field, c.old),
      new: fieldValue(field, c.new),
    }
  })
}

async function loadLogs(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  forbidden.value = false
  try {
    const data = await fetchAdminLogs(page.value)
    logs.value = data.logs
    totalPages.value = data.totalPages
  } catch (e) {
    if (e instanceof ApiClientError && e.errorCode === 'FORBIDDEN') {
      forbidden.value = true
    } else {
      loadFailed.value = true
    }
  } finally {
    loading.value = false
  }
}

function onPageChange(p: number): void {
  page.value = p
  loadLogs()
}

onMounted(loadLogs)
</script>

<template>
  <div>
    <div class="adm-card adm-card--table">
      <table class="adm-table">
        <thead>
          <tr>
            <th>{{ $t('admin.logs.col.admin') }}</th>
            <th>{{ $t('admin.logs.col.action') }}</th>
            <th>{{ $t('admin.logs.col.details') }}</th>
            <th class="adm-num">{{ $t('admin.logs.col.time') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-if="loading">
            <tr v-for="i in 5" :key="i" class="adm-tr--skel" aria-hidden="true">
              <td v-for="j in 4" :key="j"><span class="adm-skel"></span></td>
            </tr>
          </template>
          <tr v-else-if="forbidden">
            <td colspan="4" class="adm-state is-warning">{{ $t('admin.common.forbidden') }}</td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="4" class="adm-state is-danger">{{ $t('admin.common.loadFailed') }}</td>
          </tr>
          <tr v-else-if="logs.length === 0">
            <td colspan="4" class="adm-state">{{ $t('admin.logs.noData') }}</td>
          </tr>
          <template v-else>
            <tr v-for="log in logs" :key="log.id">
              <td class="adm-strong">{{ log.admin_username }}</td>
              <td>{{ actionText(log.action) }}</td>
              <td class="adm-log-details">
                <div class="adm-log-summary">{{ summaryText(log) }}</div>
                <div v-for="c in changeVMs(log)" :key="c.label" class="adm-log-change">
                  <span class="adm-log-field">{{ c.label }}</span>
                  <span class="adm-log-old">{{ c.old }}</span>
                  <span class="adm-log-arrow">→</span>
                  <span class="adm-log-new">{{ c.new }}</span>
                </div>
              </td>
              <td class="adm-num">{{ formatDate(log.created_at) }}</td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination :current="page" :total="totalPages" @change="onPageChange" />
  </div>
</template>

<style scoped>
.adm-log-details {
  max-width: 420px;
}

.adm-log-summary {
  color: var(--adm-fg, inherit);
  word-break: break-all;
}

.adm-log-change {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
  margin-top: 4px;
  font-size: 12px;
  color: var(--adm-dim, #8a857e);
}

.adm-log-field {
  color: var(--adm-mid, #a8a29a);
}

.adm-log-old {
  text-decoration: line-through;
  word-break: break-all;
}

.adm-log-new {
  color: var(--adm-fg, inherit);
  word-break: break-all;
}
</style>
