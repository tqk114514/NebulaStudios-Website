<script setup lang="ts">
// 操作日志（迁移自旧版 modules/admin/assets/js/logs.ts）。
// 仅超级管理员可见（路由 + 后端双重控制）。
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

function formatDetails(log: AdminLogEntry): string {
  const d = log.details
  if (!d) return '-'

  if (log.action === 'set_role') {
    return t('admin.logs.details.roleChange', {
      name: String(d.target_username ?? ''),
      old: roleText(Number(d.old_role)),
      new: roleText(Number(d.new_role)),
    })
  }
  if (log.action === 'delete_user') {
    return t('admin.logs.details.deletedUser', {
      name: String(d.target_username ?? ''),
      email: String(d.target_email ?? ''),
    })
  }
  if (log.action === 'ban_user') {
    return t('admin.logs.details.banReason', {
      name: String(d.target_username ?? ''),
      reason: String(d.reason ?? ''),
    })
  }
  if (log.action === 'unban_user') {
    return String(d.target_username ?? '')
  }
  if (log.action.startsWith('oauth_client_')) {
    const name = String(d.client_name ?? '')
    const id = String(d.client_id ?? '')
    return id ? `${name} (${id})` : name
  }
  if (log.action.startsWith('email_whitelist_')) {
    return String(d.domain ?? '')
  }
  if (log.action === 'data_export' || log.action === 'data_import') {
    const users = Number(d.users_count ?? d.usersCount ?? d.users_imported ?? d.usersImported ?? 0)
    const logsCount = Number(d.logs_count ?? d.logsCount ?? d.logs_imported ?? d.logsImported ?? 0)
    return t('admin.logs.details.dataStats', { users, logs: logsCount })
  }
  return JSON.stringify(d)
}

function roleText(role: number): string {
  return t(role === 2 ? 'admin.users.role.superAdmin' : role === 1 ? 'admin.users.role.admin' : 'admin.users.role.user')
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
    <div class="adm-table-wrap">
      <table class="adm-table">
        <thead>
          <tr>
            <th>{{ $t('admin.logs.col.admin') }}</th>
            <th>{{ $t('admin.logs.col.action') }}</th>
            <th>{{ $t('admin.logs.col.details') }}</th>
            <th>{{ $t('admin.logs.col.time') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="4" class="adm-loading-cell">...</td>
          </tr>
          <tr v-else-if="forbidden">
            <td colspan="4" class="adm-loading-cell">{{ $t('admin.common.forbidden') }}</td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="4" class="adm-loading-cell">{{ $t('admin.common.loadFailed') }}</td>
          </tr>
          <tr v-else-if="logs.length === 0">
            <td colspan="4" class="adm-loading-cell">{{ $t('admin.logs.noData') }}</td>
          </tr>
          <template v-else>
            <tr v-for="log in logs" :key="log.id">
              <td>{{ log.admin_username }}</td>
              <td>{{ actionText(log.action) }}</td>
              <td>{{ formatDetails(log) }}</td>
              <td>{{ formatDate(log.created_at) }}</td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination :current="page" :total="totalPages" @change="onPageChange" />
  </div>
</template>
