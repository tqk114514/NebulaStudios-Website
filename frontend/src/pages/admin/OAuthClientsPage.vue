<script setup lang="ts">
// OAuth 应用管理（迁移自旧版 modules/admin/assets/js/oauth.ts）。
// 列表 + 搜索 + 创建/编辑表单 + 详情 + 启用/禁用 + 密钥重生成 + 删除。
// 密钥弹窗关闭时立即清空明文，缩短驻留时间（对齐旧实现）。
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/AppModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import Pagination from './Pagination.vue'
import { toast } from '@/composables/useToast'
import {
  fetchOAuthClients,
  fetchOAuthClient,
  createOAuthClient,
  updateOAuthClient,
  toggleOAuthClient,
  deleteOAuthClient,
  regenerateOAuthClientSecret,
  type OAuthClient,
} from '@/api/admin'
import { ApiClientError } from '@/api/client'
import { formatDate } from './admin-utils'
import './admin-shared.css'

const { t } = useI18n()

const clients = ref<OAuthClient[]>([])
const page = ref(1)
const totalPages = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)
const searchInput = ref('')
const activeSearch = ref('')

const detailOpen = ref(false)
const detailLoading = ref(false)
const detailClient = ref<OAuthClient | null>(null)

const formOpen = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formDescription = ref('')
const formRedirect = ref('')
const formSubmitting = ref(false)

const secretOpen = ref(false)
const secretValue = ref('')

const confirmOpen = ref(false)
const confirmTitle = ref('')
const confirmText = ref('')
const confirmDanger = ref(false)
let confirmAction: (() => Promise<void>) | null = null

function statusBadge(enabled: boolean): string {
  return enabled ? 'adm-tag adm-tag--success' : 'adm-tag'
}

async function loadClients(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  forbidden.value = false
  try {
    const data = await fetchOAuthClients(page.value, activeSearch.value)
    clients.value = data.clients
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
  loadClients()
}

function onSearch(): void {
  activeSearch.value = searchInput.value.trim()
  page.value = 1
  loadClients()
}

async function openDetail(id: number): Promise<void> {
  detailOpen.value = true
  detailLoading.value = true
  detailClient.value = null
  try {
    detailClient.value = await fetchOAuthClient(id)
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

async function handleConfirm(): Promise<void> {
  if (confirmAction) await confirmAction()
  confirmAction = null
}

async function refreshList(): Promise<void> {
  if (clients.value.length === 1 && page.value > 1) page.value -= 1
  await loadClients()
}

// ---- 表单 ----

function openCreate(): void {
  editingId.value = null
  formName.value = ''
  formDescription.value = ''
  formRedirect.value = ''
  formOpen.value = true
}

function openEdit(client: OAuthClient): void {
  editingId.value = client.id
  formName.value = client.name
  formDescription.value = client.description || ''
  formRedirect.value = client.redirect_uri
  detailOpen.value = false
  formOpen.value = true
}

/** 与服务端 validateRedirectURIScheme 一致：https（需主机名）或 http 且仅限本地回环，拒绝锚点 */
function validateRedirect(uri: string): boolean {
  try {
    const parsed = new URL(uri)
    const host = parsed.hostname.toLowerCase()
    const schemeOk =
      (parsed.protocol === 'https:' && host !== '') ||
      (parsed.protocol === 'http:' && (host === 'localhost' || host === '127.0.0.1' || host === '::1'))
    return schemeOk && !parsed.hash
  } catch {
    return false
  }
}

async function submitForm(): Promise<void> {
  const name = formName.value.trim()
  const description = formDescription.value.trim()
  const redirectUri = formRedirect.value.trim()

  if (!name) {
    toast.error(t('admin.oauth.form.error.name'))
    return
  }
  if (!redirectUri) {
    toast.error(t('admin.oauth.form.error.redirect'))
    return
  }
  try {
    new URL(redirectUri)
  } catch {
    toast.error(t('admin.oauth.form.error.redirectFormat'))
    return
  }
  if (!validateRedirect(redirectUri)) {
    toast.error(t('admin.oauth.form.error.redirectScheme'))
    return
  }

  formSubmitting.value = true
  try {
    if (editingId.value !== null) {
      await updateOAuthClient(editingId.value, name, description, redirectUri)
      toast.success(t('admin.oauth.toast.updated'))
      formOpen.value = false
      await loadClients()
    } else {
      const result = await createOAuthClient(name, description, redirectUri)
      formOpen.value = false
      await loadClients()
      secretValue.value = result.client_secret
      secretOpen.value = true
      toast.success(t('admin.oauth.toast.created'))
    }
  } catch {
    toast.error(editingId.value !== null ? t('admin.oauth.toast.updateFailed') : t('admin.common.operateFailed'))
  } finally {
    formSubmitting.value = false
  }
}

function confirmToggle(client: OAuthClient): void {
  const enabling = !client.is_enabled
  askConfirm(
    t(enabling ? 'admin.oauth.confirm.enableTitle' : 'admin.oauth.confirm.disableTitle'),
    t('admin.oauth.confirm.toggleText', { action: t(enabling ? 'admin.oauth.confirm.enable' : 'admin.oauth.confirm.disable'), name: client.name }),
    async () => {
      try {
        await toggleOAuthClient(client.id, enabling)
        toast.success(t(enabling ? 'admin.oauth.toast.enabled' : 'admin.oauth.toast.disabled'))
        await loadClients()
      } catch {
        toast.error(t('admin.common.operateFailed'))
      }
    },
  )
}

function confirmRegenerate(client: OAuthClient): void {
  askConfirm(t('admin.oauth.confirm.regenerateTitle'), t('admin.oauth.confirm.regenerateText'), async () => {
    try {
      const result = await regenerateOAuthClientSecret(client.id)
      detailOpen.value = false
      secretValue.value = result.client_secret
      secretOpen.value = true
      toast.success(t('admin.oauth.toast.regenerated'))
    } catch {
      toast.error(t('admin.common.operateFailed'))
    }
  })
}

function confirmDelete(client: OAuthClient): void {
  askConfirm(
    t('admin.oauth.confirm.deleteTitle'),
    t('admin.oauth.confirm.deleteText', { name: client.name }),
    async () => {
      try {
        await deleteOAuthClient(client.id)
        toast.success(t('admin.oauth.toast.deleted'))
        await refreshList()
      } catch {
        toast.error(t('admin.oauth.toast.deleteFailed'))
      }
    },
    true,
  )
}

function closeSecret(): void {
  secretValue.value = ''
  secretOpen.value = false
}

async function copySecret(): Promise<void> {
  try {
    await navigator.clipboard.writeText(secretValue.value)
    toast.success(t('admin.oauth.secret.copied'))
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = secretValue.value
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    toast.success(t('admin.oauth.secret.copied'))
  }
}

onMounted(loadClients)
</script>

<template>
  <div>
    <div class="adm-toolbar">
      <div class="adm-search">
        <input
          v-model="searchInput"
          type="text"
          :placeholder="$t('admin.oauth.searchPlaceholder')"
          @keyup.enter="onSearch"
        />
        <button type="button" class="adm-search-btn" @click="onSearch">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
            <path d="M15.5 14h-.79l-.28-.27a6.5 6.5 0 1 0-.7.7l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z" />
          </svg>
        </button>
      </div>
      <button type="button" class="adm-btn adm-btn--primary" @click="openCreate">{{ $t('admin.oauth.create') }}</button>
    </div>

    <div class="adm-card adm-card--table">
      <table class="adm-table">
        <thead>
          <tr>
            <th>{{ $t('admin.oauth.col.name') }}</th>
            <th>{{ $t('admin.oauth.col.clientId') }}</th>
            <th>{{ $t('admin.oauth.col.status') }}</th>
            <th class="adm-num">{{ $t('admin.oauth.col.createdAt') }}</th>
            <th class="adm-end">{{ $t('admin.oauth.col.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-if="loading">
            <tr v-for="i in 5" :key="i" class="adm-tr--skel" aria-hidden="true">
              <td v-for="j in 5" :key="j"><span class="adm-skel"></span></td>
            </tr>
          </template>
          <tr v-else-if="forbidden">
            <td colspan="5" class="adm-state is-warning">{{ $t('admin.common.forbidden') }}</td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="5" class="adm-state is-danger">{{ $t('admin.common.loadFailed') }}</td>
          </tr>
          <tr v-else-if="clients.length === 0">
            <td colspan="5" class="adm-state">{{ $t('admin.common.noData') }}</td>
          </tr>
          <template v-else>
            <tr v-for="c in clients" :key="c.id">
              <td>
                <div class="adm-cell-main">{{ c.name }}</div>
                <div v-if="c.description" class="adm-cell-sub">{{ c.description }}</div>
              </td>
              <td><code class="adm-code">{{ c.client_id }}</code></td>
              <td><span :class="statusBadge(c.is_enabled)">{{ $t(c.is_enabled ? 'admin.common.enabled' : 'admin.common.disabled') }}</span></td>
              <td class="adm-num">{{ formatDate(c.created_at) }}</td>
              <td class="adm-end">
                <button type="button" class="adm-row-btn" @click="openDetail(c.id)">
                  {{ $t('admin.common.view') }}
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination :current="page" :total="totalPages" @change="onPageChange" />

    <!-- 应用详情 -->
    <AppModal v-model:open="detailOpen" :title="$t('admin.oauth.detail.title')" width="520px">
      <template v-if="detailLoading || !detailClient">
        <div class="adm-detail">
          <div v-for="i in 7" :key="i" class="adm-detail-row adm-detail-row--skel">
            <span class="adm-skel"></span>
            <span class="adm-skel"></span>
          </div>
        </div>
      </template>
      <template v-else>
        <div class="adm-detail">
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.col.name') }}</span>
            <span class="adm-detail-value">{{ detailClient.name }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.detail.description') }}</span>
            <span class="adm-detail-value">{{ detailClient.description || '-' }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.col.clientId') }}</span>
            <span class="adm-detail-value adm-mono">{{ detailClient.client_id }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.detail.redirectUri') }}</span>
            <span class="adm-detail-value adm-mono">{{ detailClient.redirect_uri }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.col.status') }}</span>
            <span class="adm-detail-value">
              <span :class="statusBadge(detailClient.is_enabled)">
                {{ $t(detailClient.is_enabled ? 'admin.common.enabled' : 'admin.common.disabled') }}
              </span>
            </span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.col.createdAt') }}</span>
            <span class="adm-detail-value">{{ formatDate(detailClient.created_at) }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.detail.updatedAt') }}</span>
            <span class="adm-detail-value">{{ formatDate(detailClient.updated_at) }}</span>
          </div>
        </div>
      </template>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="detailOpen = false">
            {{ $t('admin.common.close') }}
          </button>
          <template v-if="detailClient">
            <button type="button" class="adm-btn adm-btn--secondary" @click="confirmToggle(detailClient)">
              {{ $t(detailClient.is_enabled ? 'admin.oauth.confirm.disable' : 'admin.oauth.confirm.enable') }}
            </button>
            <button type="button" class="adm-btn adm-btn--secondary" @click="openEdit(detailClient)">
              {{ $t('admin.oauth.action.edit') }}
            </button>
            <button type="button" class="adm-btn adm-btn--secondary" @click="confirmRegenerate(detailClient)">
              {{ $t('admin.oauth.action.regenerate') }}
            </button>
            <button type="button" class="adm-btn adm-btn--danger" @click="confirmDelete(detailClient)">
              {{ $t('admin.oauth.action.delete') }}
            </button>
          </template>
        </div>
      </template>
    </AppModal>

    <!-- 创建/编辑表单 -->
    <AppModal v-model:open="formOpen" :title="$t(editingId !== null ? 'admin.oauth.form.editTitle' : 'admin.oauth.form.createTitle')" width="480px">
      <div class="adm-field">
        <label>{{ $t('admin.oauth.form.name') }} <span class="adm-required">*</span></label>
        <input v-model="formName" type="text" class="adm-input" :placeholder="$t('admin.oauth.form.namePlaceholder')" />
      </div>
      <div class="adm-field">
        <label>{{ $t('admin.oauth.form.description') }}</label>
        <textarea v-model="formDescription" class="adm-textarea" :placeholder="$t('admin.oauth.form.descriptionPlaceholder')"></textarea>
      </div>
      <div class="adm-field">
        <label>{{ $t('admin.oauth.form.redirectUri') }} <span class="adm-required">*</span></label>
        <input v-model="formRedirect" type="text" class="adm-input" :placeholder="$t('admin.oauth.form.redirectUriPlaceholder')" />
        <span class="adm-hint">{{ $t('admin.oauth.form.redirectHint') }}</span>
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="formOpen = false">
            {{ $t('admin.common.cancel') }}
          </button>
          <button type="button" class="adm-btn adm-btn--primary" :disabled="formSubmitting" @click="submitForm">
            {{ $t(editingId !== null ? 'admin.oauth.form.submitSave' : 'admin.oauth.form.submitCreate') }}
          </button>
        </div>
      </template>
    </AppModal>

    <!-- 密钥展示 -->
    <AppModal v-model:open="secretOpen" :title="$t('admin.oauth.secret.title')" width="480px" :z-index="210" @close="closeSecret">
      <div class="adm-notice">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
          <path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z" />
        </svg>
        <span>{{ $t('admin.oauth.secret.warning') }}</span>
      </div>
      <div class="adm-secret">
        <code>{{ secretValue }}</code>
        <button type="button" class="adm-btn adm-btn--secondary" @click="copySecret">
          {{ $t('admin.oauth.secret.copy') }}
        </button>
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--primary" @click="closeSecret">
            {{ $t('admin.oauth.secret.ok') }}
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
