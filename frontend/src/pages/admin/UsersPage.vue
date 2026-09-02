<script setup lang="ts">
// 用户管理（迁移自旧版 modules/admin/assets/js/users.ts）。
// 列表 + 搜索 + 分页；详情/封禁/角色/删除操作，权限与旧版一致：
// 封禁/解封需 role>=1 且目标为普通用户；角色变更与删除需 role>=2 且目标 role<2。
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import AppModal from '@/components/AppModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import Pagination from './Pagination.vue'
import { toast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import {
  fetchAdminUsers,
  fetchAdminUser,
  setAdminUserRole,
  deleteAdminUser,
  banAdminUser,
  unbanAdminUser,
  type AdminUser,
} from '@/api/admin'
import { ApiClientError } from '@/api/client'
import { formatDate, isUserBanned } from './admin-utils'
import './admin-shared.css'

const { t, te } = useI18n()
const auth = useAuthStore()
const { role: currentRole } = storeToRefs(auth)

// ---- 列表状态 ----
const users = ref<AdminUser[]>([])
const page = ref(1)
const totalPages = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)
const searchInput = ref('')
const activeSearch = ref('')

// ---- 详情弹窗 ----
const detailOpen = ref(false)
const detailLoading = ref(false)
const detailUser = ref<AdminUser | null>(null)

// ---- 封禁弹窗 ----
const banOpen = ref(false)
const banTarget = ref<AdminUser | null>(null)
const banReason = ref('')
const banDuration = ref('7')
const banSubmitting = ref(false)

// ---- 确认框 ----
const confirmOpen = ref(false)
const confirmTitle = ref('')
const confirmText = ref('')
const confirmDanger = ref(false)
let confirmAction: (() => Promise<void>) | null = null

const BAN_REASONS = ['violation', 'abuse', 'malicious', 'spam'] as const
const BAN_DURATIONS = ['1', '3', '7', '30', '90', '365', '0'] as const

function roleBadgeClass(role: number): string {
  return role === 2 ? 'adm-badge role-super-admin' : role === 1 ? 'adm-badge role-admin' : 'adm-badge role-user'
}

function roleText(role: number): string {
  return t(role === 2 ? 'admin.users.role.superAdmin' : role === 1 ? 'admin.users.role.admin' : 'admin.users.role.user')
}

function banReasonText(reason?: string): string {
  if (!reason) return '-'
  const key = `admin.users.ban.reason.${reason}`
  return te(key, 'zh-CN') ? t(key) : reason
}

function isTargetBanned(user: AdminUser): boolean {
  return isUserBanned(user)
}

async function loadUsers(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  forbidden.value = false
  try {
    const data = await fetchAdminUsers(page.value, activeSearch.value)
    users.value = data.users
    totalPages.value = data.totalPages
  } catch (e) {
    if (e instanceof ApiClientError && (e.errorCode === 'FORBIDDEN' || e.errorCode === 'ACCESS_DENIED')) {
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
  loadUsers()
}

function onSearch(): void {
  activeSearch.value = searchInput.value.trim()
  page.value = 1
  loadUsers()
}

async function openDetail(uid: string): Promise<void> {
  detailOpen.value = true
  detailLoading.value = true
  detailUser.value = null
  try {
    detailUser.value = await fetchAdminUser(uid)
  } catch {
    detailOpen.value = false
    toast.error(t('admin.common.loadFailed'))
  } finally {
    detailLoading.value = false
  }
}

function askConfirm(title: string, text: string, action: () => Promise<void>, danger = false): void {
  confirmTitle.value = title
  confirmText.value = text
  confirmDanger.value = danger
  confirmAction = action
  detailOpen.value = false
  confirmOpen.value = true
}

async function refreshList(): Promise<void> {
  if (users.value.length === 1 && page.value > 1) page.value -= 1
  await loadUsers()
}

async function handleConfirm(): Promise<void> {
  if (confirmAction) await confirmAction()
  confirmAction = null
}

function openBanModal(): void {
  if (!detailUser.value) return
  banTarget.value = detailUser.value
  banReason.value = ''
  banDuration.value = '7'
  detailOpen.value = false
  banOpen.value = true
}

async function submitBan(): Promise<void> {
  if (!banTarget.value || !banReason.value || banSubmitting.value) return
  banSubmitting.value = true
  try {
    await banAdminUser(banTarget.value.uid, banReason.value, Number(banDuration.value))
    toast.success(t('admin.users.toast.banned'))
    banOpen.value = false
    banTarget.value = null
    await refreshList()
  } catch {
    toast.error(t('admin.users.toast.banFailed'))
  } finally {
    banSubmitting.value = false
  }
}

function confirmUnban(user: AdminUser): void {
  askConfirm(t('admin.users.confirm.unbanTitle'), t('admin.users.confirm.unbanText', { name: user.username }), async () => {
    try {
      await unbanAdminUser(user.uid)
      toast.success(t('admin.users.toast.unbanned'))
      await refreshList()
    } catch {
      toast.error(t('admin.common.operateFailed'))
    }
  })
}

function confirmPromote(user: AdminUser): void {
  askConfirm(t('admin.users.confirm.promoteTitle'), t('admin.users.confirm.promoteText', { name: user.username }), async () => {
    try {
      await setAdminUserRole(user.uid, 1)
      toast.success(t('admin.users.toast.promoted'))
      await refreshList()
    } catch {
      toast.error(t('admin.common.operateFailed'))
    }
  })
}

function confirmDemote(user: AdminUser): void {
  askConfirm(t('admin.users.confirm.demoteTitle'), t('admin.users.confirm.demoteText', { name: user.username }), async () => {
    try {
      await setAdminUserRole(user.uid, 0)
      toast.success(t('admin.users.toast.demoted'))
      await refreshList()
    } catch {
      toast.error(t('admin.common.operateFailed'))
    }
  })
}

function confirmDelete(user: AdminUser): void {
  askConfirm(
    t('admin.users.confirm.deleteTitle'),
    t('admin.users.confirm.deleteText', { name: user.username }),
    async () => {
      try {
        await deleteAdminUser(user.uid)
        toast.success(t('admin.users.toast.deleted'))
        await refreshList()
      } catch {
        toast.error(t('admin.users.toast.deleteFailed'))
      }
    },
    true,
  )
}

onMounted(loadUsers)
</script>

<template>
  <div>
    <div class="adm-page-header">
      <div class="adm-search">
        <input
          v-model="searchInput"
          type="text"
          :placeholder="$t('admin.users.searchPlaceholder')"
          @keyup.enter="onSearch"
        />
        <button type="button" class="adm-search-btn" @click="onSearch">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
            <path d="M15.5 14h-.79l-.28-.27a6.5 6.5 0 1 0-.7.7l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z" />
          </svg>
        </button>
      </div>
    </div>

    <div class="adm-table-wrap">
      <table class="adm-table">
        <thead>
          <tr>
            <th>{{ $t('admin.users.col.uid') }}</th>
            <th>{{ $t('admin.users.col.username') }}</th>
            <th>{{ $t('admin.users.col.email') }}</th>
            <th>{{ $t('admin.users.col.role') }}</th>
            <th>{{ $t('admin.users.col.createdAt') }}</th>
            <th>{{ $t('admin.users.col.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="6" class="adm-loading-cell">...</td>
          </tr>
          <tr v-else-if="forbidden">
            <td colspan="6" class="adm-loading-cell">{{ $t('admin.common.forbidden') }}</td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="6" class="adm-loading-cell">{{ $t('admin.common.loadFailed') }}</td>
          </tr>
          <tr v-else-if="users.length === 0">
            <td colspan="6" class="adm-loading-cell">{{ $t('admin.users.noData') }}</td>
          </tr>
          <template v-else>
            <tr v-for="u in users" :key="u.uid">
              <td class="adm-mono">{{ u.uid }}</td>
              <td>{{ u.username }}</td>
              <td>{{ u.email }}</td>
              <td><span :class="roleBadgeClass(u.role)">{{ roleText(u.role) }}</span></td>
              <td>{{ formatDate(u.created_at) }}</td>
              <td>
                <button type="button" class="adm-action-btn view" @click="openDetail(u.uid)">
                  {{ $t('admin.common.view') }}
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination :current="page" :total="totalPages" @change="onPageChange" />

    <!-- 用户详情 -->
    <AppModal v-model:open="detailOpen" :title="$t('admin.users.detail.title')" width="480px">
      <template v-if="detailLoading || !detailUser">
        <div class="adm-detail">
          <div v-for="i in 6" :key="i" class="adm-detail-row">
            <span class="adm-detail-label">-</span>
            <span class="adm-detail-value">...</span>
          </div>
        </div>
      </template>
      <template v-else>
        <div class="adm-detail">
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.col.uid') }}</span>
            <span class="adm-detail-value mono">{{ detailUser.uid }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.col.username') }}</span>
            <span class="adm-detail-value">{{ detailUser.username }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.col.email') }}</span>
            <span class="adm-detail-value">{{ detailUser.email }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.col.role') }}</span>
            <span class="adm-detail-value"><span :class="roleBadgeClass(detailUser.role)">{{ roleText(detailUser.role) }}</span></span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.detail.microsoftAccount') }}</span>
            <span class="adm-detail-value">{{ detailUser.microsoft_name || $t('admin.users.detail.notBound') }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.users.col.createdAt') }}</span>
            <span class="adm-detail-value">{{ formatDate(detailUser.created_at) }}</span>
          </div>
          <template v-if="isTargetBanned(detailUser)">
            <div class="adm-detail-row adm-detail-banned">
              <span class="adm-detail-label">{{ $t('admin.users.detail.banStatus') }}</span>
              <span class="adm-detail-value"><span class="adm-badge banned">{{ $t('admin.users.detail.banned') }}</span></span>
            </div>
            <div class="adm-detail-row">
              <span class="adm-detail-label">{{ $t('admin.users.detail.banReason') }}</span>
              <span class="adm-detail-value">{{ banReasonText(detailUser.ban_reason) }}</span>
            </div>
            <div class="adm-detail-row">
              <span class="adm-detail-label">{{ $t('admin.users.detail.banTime') }}</span>
              <span class="adm-detail-value">{{ formatDate(detailUser.banned_at) }}</span>
            </div>
            <div class="adm-detail-row">
              <span class="adm-detail-label">{{ $t('admin.users.detail.unbanTime') }}</span>
              <span class="adm-detail-value" :class="{ 'adm-permanent-ban': !detailUser.unban_at }">
                {{ detailUser.unban_at ? formatDate(detailUser.unban_at) : $t('admin.users.detail.permanentBan') }}
              </span>
            </div>
          </template>
        </div>
      </template>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="detailOpen = false">
            {{ $t('admin.common.close') }}
          </button>
          <template v-if="detailUser">
            <template v-if="currentRole >= 1 && detailUser.role < 1">
              <button v-if="isTargetBanned(detailUser)" type="button" class="adm-btn adm-btn--success" @click="confirmUnban(detailUser)">
                {{ $t('admin.users.action.unban') }}
              </button>
              <button v-else type="button" class="adm-btn adm-btn--warning" @click="openBanModal">
                {{ $t('admin.users.action.ban') }}
              </button>
            </template>
            <template v-if="currentRole >= 2 && detailUser.role < 2">
              <button v-if="detailUser.role === 0 && !isTargetBanned(detailUser)" type="button" class="adm-btn adm-btn--warning" @click="confirmPromote(detailUser)">
                {{ $t('admin.users.action.promote') }}
              </button>
              <button v-if="detailUser.role === 1" type="button" class="adm-btn adm-btn--secondary" @click="confirmDemote(detailUser)">
                {{ $t('admin.users.action.demote') }}
              </button>
              <button type="button" class="adm-btn adm-btn--danger" @click="confirmDelete(detailUser)">
                {{ $t('admin.users.action.delete') }}
              </button>
            </template>
          </template>
        </div>
      </template>
    </AppModal>

    <!-- 封禁弹窗 -->
    <AppModal v-model:open="banOpen" :title="$t('admin.users.ban.title')" width="420px" :z-index="210">
      <div class="adm-form-group">
        <label>{{ $t('admin.users.ban.reason') }}</label>
        <select v-model="banReason" class="adm-form-select">
          <option value="">{{ $t('admin.users.ban.reasonPlaceholder') }}</option>
          <option v-for="r in BAN_REASONS" :key="r" :value="r">{{ $t(`admin.users.ban.reason.${r}`) }}</option>
        </select>
      </div>
      <div class="adm-form-group">
        <label>{{ $t('admin.users.ban.duration') }}</label>
        <select v-model="banDuration" class="adm-form-select">
          <option v-for="d in BAN_DURATIONS" :key="d" :value="d">{{ $t(`admin.users.ban.duration.${d}`) }}</option>
        </select>
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="banOpen = false">
            {{ $t('admin.common.cancel') }}
          </button>
          <button type="button" class="adm-btn adm-btn--danger" :disabled="!banReason || banSubmitting" @click="submitBan">
            {{ $t('admin.users.ban.confirm') }}
          </button>
        </div>
      </template>
    </AppModal>

    <!-- 通用确认 -->
    <ConfirmDialog
      v-model:open="confirmOpen"
      :title="confirmTitle"
      :content="confirmText"
      :cancel-text="$t('admin.common.cancel')"
      :confirm-text="$t('admin.common.confirm')"
      :danger="confirmDanger"
      @confirm="handleConfirm"
    />
  </div>
</template>
