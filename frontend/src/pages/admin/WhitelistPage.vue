<script setup lang="ts">
// 邮箱白名单管理（迁移自旧版 modules/admin/assets/js/email-whitelist.ts）。
// 列表 + 分页 + 添加/编辑表单 + 详情 + 启用/禁用 + 删除。
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/AppModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import Pagination from './Pagination.vue'
import { toast } from '@/composables/useToast'
import {
  fetchWhitelist,
  fetchWhitelistEntry,
  createWhitelistEntry,
  updateWhitelistEntry,
  deleteWhitelistEntry,
  type EmailWhitelistEntry,
} from '@/api/admin'
import { ApiClientError } from '@/api/client'
import { formatDate } from './admin-utils'
import './admin-shared.css'

const { t } = useI18n()

const entries = ref<EmailWhitelistEntry[]>([])
const page = ref(1)
const totalPages = ref(1)
const loading = ref(false)
const loadFailed = ref(false)
const forbidden = ref(false)

const detailOpen = ref(false)
const detailLoading = ref(false)
const detailEntry = ref<EmailWhitelistEntry | null>(null)

const formOpen = ref(false)
const editingId = ref<number | null>(null)
const formDomain = ref('')
const formSignupUrl = ref('')
const formLogoUrl = ref('')
const formSubmitting = ref(false)

const confirmOpen = ref(false)
const confirmTitle = ref('')
const confirmText = ref('')
const confirmDanger = ref(false)
let confirmAction: (() => Promise<void>) | null = null

function statusBadge(enabled: boolean): string {
  return enabled ? 'adm-tag adm-tag--success' : 'adm-tag'
}

async function loadWhitelist(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  forbidden.value = false
  try {
    const data = await fetchWhitelist(page.value)
    entries.value = data.whitelist
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
  loadWhitelist()
}

async function openDetail(id: number): Promise<void> {
  detailOpen.value = true
  detailLoading.value = true
  detailEntry.value = null
  try {
    const data = await fetchWhitelistEntry(id)
    detailEntry.value = data.item
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
  if (entries.value.length === 1 && page.value > 1) page.value -= 1
  await loadWhitelist()
}

function openCreate(): void {
  editingId.value = null
  formDomain.value = ''
  formSignupUrl.value = ''
  formLogoUrl.value = ''
  formOpen.value = true
}

function openEdit(entry: EmailWhitelistEntry): void {
  editingId.value = entry.id
  formDomain.value = entry.domain
  formSignupUrl.value = entry.signup_url
  formLogoUrl.value = entry.logo_url || ''
  detailOpen.value = false
  formOpen.value = true
}

async function submitForm(): Promise<void> {
  const domain = formDomain.value.trim()
  const signupUrl = formSignupUrl.value.trim()
  const logoUrl = formLogoUrl.value.trim()

  if (!domain) {
    toast.error(t('admin.whitelist.form.error.domain'))
    return
  }
  if (!signupUrl) {
    toast.error(t('admin.whitelist.form.error.signupUrl'))
    return
  }

  formSubmitting.value = true
  try {
    if (editingId.value !== null) {
      // 编辑沿用旧列表中的启用状态（后端 PUT 需携带完整字段）
      const current = entries.value.find((e) => e.id === editingId.value)
      await updateWhitelistEntry(editingId.value, {
        domain,
        signup_url: signupUrl,
        logo_url: logoUrl,
        is_enabled: current?.is_enabled ?? true,
      })
      toast.success(t('admin.whitelist.toast.updated'))
      formOpen.value = false
      await loadWhitelist()
    } else {
      await createWhitelistEntry(domain, signupUrl, logoUrl)
      toast.success(t('admin.whitelist.toast.added'))
      formOpen.value = false
      page.value = 1
      await loadWhitelist()
    }
  } catch {
    toast.error(editingId.value !== null ? t('admin.whitelist.toast.updateFailed') : t('admin.whitelist.toast.createFailed'))
  } finally {
    formSubmitting.value = false
  }
}

async function toggleEntry(entry: EmailWhitelistEntry): Promise<void> {
  const enabling = !entry.is_enabled
  try {
    await updateWhitelistEntry(entry.id, {
      domain: entry.domain,
      signup_url: entry.signup_url,
      logo_url: entry.logo_url,
      is_enabled: enabling,
    })
    toast.success(t(enabling ? 'admin.whitelist.toast.enabled' : 'admin.whitelist.toast.disabled'))
    await loadWhitelist()
  } catch {
    toast.error(t('admin.common.operateFailed'))
  }
}

function confirmToggle(entry: EmailWhitelistEntry): void {
  const enabling = !entry.is_enabled
  askConfirm(
    t(enabling ? 'admin.whitelist.confirm.enableTitle' : 'admin.whitelist.confirm.disableTitle'),
    t('admin.whitelist.confirm.toggleText', { action: t(enabling ? 'admin.whitelist.confirm.enable' : 'admin.whitelist.confirm.disable'), domain: entry.domain }),
    () => toggleEntry(entry),
  )
}

function confirmDelete(entry: EmailWhitelistEntry): void {
  askConfirm(
    t('admin.whitelist.confirm.deleteTitle'),
    t('admin.whitelist.confirm.deleteText', { domain: entry.domain }),
    async () => {
      try {
        await deleteWhitelistEntry(entry.id)
        toast.success(t('admin.whitelist.toast.deleted'))
        await refreshList()
      } catch {
        toast.error(t('admin.whitelist.toast.deleteFailed'))
      }
    },
    true,
  )
}

onMounted(loadWhitelist)
</script>

<template>
  <div>
    <div class="adm-toolbar">
      <span></span>
      <button type="button" class="adm-btn adm-btn--primary" @click="openCreate">{{ $t('admin.whitelist.create') }}</button>
    </div>

    <div class="adm-card adm-card--table">
      <table class="adm-table">
        <thead>
          <tr>
            <th>{{ $t('admin.whitelist.col.domain') }}</th>
            <th>{{ $t('admin.whitelist.col.logo') }}</th>
            <th>{{ $t('admin.whitelist.col.signupUrl') }}</th>
            <th>{{ $t('admin.whitelist.col.status') }}</th>
            <th class="adm-num">{{ $t('admin.whitelist.col.createdAt') }}</th>
            <th class="adm-end">{{ $t('admin.whitelist.col.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-if="loading">
            <tr v-for="i in 5" :key="i" class="adm-tr--skel" aria-hidden="true">
              <td v-for="j in 6" :key="j"><span class="adm-skel"></span></td>
            </tr>
          </template>
          <tr v-else-if="forbidden">
            <td colspan="6" class="adm-state is-warning">{{ $t('admin.common.forbidden') }}</td>
          </tr>
          <tr v-else-if="loadFailed">
            <td colspan="6" class="adm-state is-danger">{{ $t('admin.common.loadFailed') }}</td>
          </tr>
          <tr v-else-if="entries.length === 0">
            <td colspan="6" class="adm-state">{{ $t('admin.common.noData') }}</td>
          </tr>
          <template v-else>
            <tr v-for="e in entries" :key="e.id">
              <td class="adm-strong">{{ e.domain }}</td>
              <td>
                <img v-if="e.logo_url" :src="e.logo_url" class="adm-thumb" alt="" width="24" height="24" />
                <span v-else class="adm-thumb-empty">{{ $t('admin.whitelist.noLogo') }}</span>
              </td>
              <td class="adm-clip" :title="e.signup_url">{{ e.signup_url }}</td>
              <td><span :class="statusBadge(e.is_enabled)">{{ $t(e.is_enabled ? 'admin.common.enabled' : 'admin.common.disabled') }}</span></td>
              <td class="adm-num">{{ formatDate(e.created_at) }}</td>
              <td class="adm-end">
                <button type="button" class="adm-row-btn" @click="openDetail(e.id)">
                  {{ $t('admin.common.view') }}
                </button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination :current="page" :total="totalPages" @change="onPageChange" />

    <!-- 白名单详情 -->
    <AppModal v-model:open="detailOpen" :title="$t('admin.whitelist.detail.title')" width="520px">
      <template v-if="detailLoading || !detailEntry">
        <div class="adm-detail">
          <div v-for="i in 6" :key="i" class="adm-detail-row adm-detail-row--skel">
            <span class="adm-skel"></span>
            <span class="adm-skel"></span>
          </div>
        </div>
      </template>
      <template v-else>
        <div class="adm-detail">
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.col.domain') }}</span>
            <span class="adm-detail-value">{{ detailEntry.domain }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.detail.logoUrl') }}</span>
            <span class="adm-detail-value adm-mono">{{ detailEntry.logo_url || $t('admin.whitelist.detail.notSet') }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.detail.logoPreview') }}</span>
            <span class="adm-detail-value">
              <img v-if="detailEntry.logo_url" :src="detailEntry.logo_url" class="adm-thumb" alt="" style="width: 48px; height: 48px" />
              <span v-else class="adm-thumb-empty">{{ $t('admin.whitelist.detail.notSet') }}</span>
            </span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.detail.signupUrl') }}</span>
            <span class="adm-detail-value adm-mono">{{ detailEntry.signup_url }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.col.status') }}</span>
            <span class="adm-detail-value">
              <span :class="statusBadge(detailEntry.is_enabled)">
                {{ $t(detailEntry.is_enabled ? 'admin.common.enabled' : 'admin.common.disabled') }}
              </span>
            </span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.whitelist.col.createdAt') }}</span>
            <span class="adm-detail-value">{{ formatDate(detailEntry.created_at) }}</span>
          </div>
          <div class="adm-detail-row">
            <span class="adm-detail-label">{{ $t('admin.oauth.detail.updatedAt') }}</span>
            <span class="adm-detail-value">{{ formatDate(detailEntry.updated_at) }}</span>
          </div>
        </div>
      </template>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="detailOpen = false">
            {{ $t('admin.common.close') }}
          </button>
          <template v-if="detailEntry">
            <button type="button" class="adm-btn adm-btn--secondary" @click="confirmToggle(detailEntry)">
              {{ $t(detailEntry.is_enabled ? 'admin.whitelist.confirm.disable' : 'admin.whitelist.confirm.enable') }}
            </button>
            <button type="button" class="adm-btn adm-btn--primary" @click="openEdit(detailEntry)">
              {{ $t('admin.whitelist.action.edit') }}
            </button>
            <button type="button" class="adm-btn adm-btn--danger" @click="confirmDelete(detailEntry)">
              {{ $t('admin.whitelist.action.delete') }}
            </button>
          </template>
        </div>
      </template>
    </AppModal>

    <!-- 添加/编辑表单 -->
    <AppModal v-model:open="formOpen" :title="$t(editingId !== null ? 'admin.whitelist.form.editTitle' : 'admin.whitelist.form.addTitle')" width="480px">
      <div class="adm-field">
        <label>{{ $t('admin.whitelist.form.domain') }} <span class="adm-required">*</span></label>
        <input v-model="formDomain" type="text" class="adm-input" :placeholder="$t('admin.whitelist.form.domainPlaceholder')" />
      </div>
      <div class="adm-field">
        <label>{{ $t('admin.whitelist.form.signupUrl') }} <span class="adm-required">*</span></label>
        <input v-model="formSignupUrl" type="text" class="adm-input" :placeholder="$t('admin.whitelist.form.signupUrlPlaceholder')" />
      </div>
      <div class="adm-field">
        <label>{{ $t('admin.whitelist.form.logoUrl') }}</label>
        <input v-model="formLogoUrl" type="text" class="adm-input" :placeholder="$t('admin.whitelist.form.logoUrlPlaceholder')" />
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="formOpen = false">
            {{ $t('admin.common.cancel') }}
          </button>
          <button type="button" class="adm-btn adm-btn--primary" :disabled="formSubmitting" @click="submitForm">
            {{ $t(editingId !== null ? 'admin.whitelist.form.submitSave' : 'admin.whitelist.form.submitAdd') }}
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
