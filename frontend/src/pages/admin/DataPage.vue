<script setup lang="ts">
// 数据导入导出（迁移自旧版 modules/admin/assets/js/data.ts）。
// 导出：请求 → OTAC 授权弹窗（5 分钟倒计时）→ blob 下载；取消/关闭时撤销 OTAC。
// 导入：.enc 文件 → 预览（独立 5MB 上传路由）→ 合并/全量覆盖（覆盖需二次确认）。
// preview 上传为 multipart，走原始 fetch 并手动附带 CSRF（client.request 仅支持 JSON）。
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppModal from '@/components/AppModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { toast } from '@/composables/useToast'
import { getCsrfToken, post, del } from '@/api/client'
import type { ExportRequestResponse, ImportPreviewResponse, ImportExecuteResponse } from '@/api/admin'
import './admin-shared.css'

const { t } = useI18n()

// ---- 导出 ----
const exportBusy = ref(false)
const exportRequestId = ref('')
const otacOpen = ref(false)
const otac = ref('')
const otacRemaining = ref(0)
let otacTimer: ReturnType<typeof setInterval> | null = null

const otacTimerText = computed(() => {
  const mins = Math.floor(otacRemaining.value / 60)
  const secs = otacRemaining.value % 60
  return `${mins}:${String(secs).padStart(2, '0')}`
})

function stopOtacTimer(): void {
  if (otacTimer) {
    clearInterval(otacTimer)
    otacTimer = null
  }
}

async function revokeOtac(): Promise<void> {
  // 撤销失败不提示（旧版行为一致）
  await del('/admin/api/data/one-time-access-code').catch(() => {})
}

function closeOtac(revoke: boolean): void {
  stopOtacTimer()
  otacOpen.value = false
  otac.value = ''
  if (revoke) void revokeOtac()
}

async function startExport(): Promise<void> {
  if (exportBusy.value) return
  exportBusy.value = true
  try {
    const data = await post<ExportRequestResponse>('/admin/api/data/export/request')
    exportRequestId.value = data.requestId
    otac.value = ''
    otacRemaining.value = data.expiresIn
    otacOpen.value = true
    stopOtacTimer()
    otacTimer = setInterval(() => {
      otacRemaining.value -= 1
      if (otacRemaining.value <= 0) {
        closeOtac(false)
        toast.error(t('admin.data.otac.expired'))
      }
    }, 1000)
  } catch {
    toast.error(t('admin.data.import.networkError'))
  } finally {
    exportBusy.value = false
  }
}

async function downloadExport(): Promise<void> {
  const code = otac.value.trim()
  if (code.length < 16) return
  try {
    const res = await fetch(
      `/admin/api/data/export/${encodeURIComponent(exportRequestId.value)}/download?otac=${encodeURIComponent(code)}`,
      { credentials: 'same-origin' },
    )
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as { errorCode?: string; message?: string }
      const errorCode = body.errorCode || 'UNKNOWN'
      if (errorCode === 'OTAC_MAX_TRIES') {
        toast.error(t('admin.data.otac.maxTries'))
        closeOtac(false)
      } else if (errorCode.startsWith('OTAC')) {
        toast.error(t('admin.data.otac.wrong'))
        otac.value = ''
      } else {
        toast.error(t('admin.data.export.failed', { reason: body.message || errorCode }))
        closeOtac(false)
      }
      return
    }

    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    const disposition = res.headers.get('content-disposition') || ''
    const match = disposition.match(/filename="([^"]+)"/)
    a.download = match ? match[1] : t('admin.data.export.defaultFilename')
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    toast.success(t('admin.data.export.success'))
    closeOtac(false)
  } catch {
    toast.error(t('admin.data.export.downloadFailed'))
  }
}

// ---- 导入 ----
const previewOpen = ref(false)
const previewBusy = ref(false)
const previewFileToken = ref('')
const previewUsers = ref(0)
const previewLogs = ref(0)
const previewExportedAt = ref('')
const strategy = ref<'merge' | 'overwrite'>('merge')
const overwriteConfirmOpen = ref(false)

function pickImportFile(): void {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = '.enc'
  input.addEventListener('change', () => {
    const file = input.files?.[0]
    if (!file) return
    if (!file.name.endsWith('.enc')) {
      toast.error(t('admin.data.import.invalidFile'))
      return
    }
    void handleImportPreview(file)
  })
  input.click()
}

async function handleImportPreview(file: File): Promise<void> {
  const formData = new FormData()
  formData.append('file', file)
  try {
    const res = await fetch('/admin/api/data/import/preview', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': getCsrfToken() },
      body: formData,
    })
    const body = (await res.json()) as { success: boolean; data?: ImportPreviewResponse }
    if (!res.ok || !body.success || !body.data) {
      toast.error(t('admin.data.import.badFormat'))
      return
    }
    previewFileToken.value = body.data.fileToken
    previewUsers.value = body.data.usersCount
    previewLogs.value = body.data.logsCount
    previewExportedAt.value = body.data.exportedAt
    strategy.value = 'merge'
    previewOpen.value = true
  } catch {
    toast.error(t('admin.data.import.networkError'))
  }
}

async function executeImport(): Promise<void> {
  if (previewBusy.value) return
  previewBusy.value = true
  try {
    let d: ImportExecuteResponse
    try {
      d = await post<ImportExecuteResponse>('/admin/api/data/import/execute', {
        fileToken: previewFileToken.value,
        strategy: strategy.value,
      })
    } catch {
      toast.error(t('admin.data.import.failed'))
      previewOpen.value = false
      return
    }
    const anomalies: string[] = []
    if (d.usersFailed > 0) anomalies.push(t('admin.data.import.anomaly.usersFailed', { count: d.usersFailed }))
    if (d.logsFailed > 0) anomalies.push(t('admin.data.import.anomaly.logsFailed', { count: d.logsFailed }))
    if (d.usersPasswordSkipped > 0) anomalies.push(t('admin.data.import.anomaly.passwordSkipped', { count: d.usersPasswordSkipped }))
    if (d.usersRoleDowngraded > 0) anomalies.push(t('admin.data.import.anomaly.roleDowngraded', { count: d.usersRoleDowngraded }))

    if (anomalies.length > 0) {
      toast.show(t('admin.data.import.successWithAnomalies', { users: d.usersImported, logs: d.logsImported, anomalies: anomalies.join('，') }), 'error')
    } else {
      toast.success(t('admin.data.import.success', { users: d.usersImported, logs: d.logsImported }))
    }
    previewOpen.value = false
  } catch {
    toast.error(t('admin.data.import.networkError'))
  } finally {
    previewBusy.value = false
  }
}

function requestImport(): void {
  // 全量覆盖先经二次确认，再执行导入
  overwriteConfirmOpen.value = true
}

onBeforeUnmount(() => {
  stopOtacTimer()
})
</script>

<template>
  <div>
    <div class="adm-cards">
      <div class="adm-card">
        <div class="adm-card-header">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
            <path d="M5 20h14v-2H5v2zm0-10h4v6h6v-6h4l-7-7-7 7z" />
          </svg>
        </div>
        <p class="adm-card-desc">{{ $t('admin.data.export.desc') }}</p>
        <button type="button" class="adm-create-btn" :disabled="exportBusy" @click="startExport">
          {{ $t('admin.data.export.button') }}
        </button>
      </div>
      <div class="adm-card">
        <div class="adm-card-header">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
            <path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z" />
          </svg>
        </div>
        <p class="adm-card-desc">{{ $t('admin.data.import.desc') }}</p>
        <button type="button" class="adm-btn adm-btn--secondary" @click="pickImportFile">
          {{ $t('admin.data.import.button') }}
        </button>
      </div>
    </div>

    <!-- 导出授权 -->
    <AppModal v-model:open="otacOpen" :title="$t('admin.data.otac.title')" width="420px" :z-index="210" @close="closeOtac(true)">
      <div class="adm-secret-warning">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
          <path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z" />
        </svg>
        <span>{{ $t('admin.data.otac.warning') }}</span>
      </div>
      <div class="adm-form-group">
        <label>{{ $t('admin.data.otac.label') }}</label>
        <input
          v-model="otac"
          type="text"
          class="adm-form-input"
          :placeholder="$t('admin.data.otac.placeholder')"
          maxlength="16"
          autocomplete="off"
        />
        <span class="adm-form-hint">{{ $t('admin.data.otac.remaining', { time: otacTimerText }) }}</span>
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="closeOtac(true)">
            {{ $t('admin.common.cancel') }}
          </button>
          <button type="button" class="adm-btn adm-btn--primary" :disabled="otac.trim().length < 16" @click="downloadExport">
            {{ $t('admin.data.otac.download') }}
          </button>
        </div>
      </template>
    </AppModal>

    <!-- 导入预览 -->
    <AppModal v-model:open="previewOpen" :title="$t('admin.data.preview.title')" width="440px">
      <div class="adm-detail">
        <div class="adm-detail-row">
          <span class="adm-detail-label">{{ $t('admin.data.preview.users') }}</span>
          <span class="adm-detail-value">{{ previewUsers }}</span>
        </div>
        <div class="adm-detail-row">
          <span class="adm-detail-label">{{ $t('admin.data.preview.logs') }}</span>
          <span class="adm-detail-value">{{ previewLogs }}</span>
        </div>
        <div class="adm-detail-row">
          <span class="adm-detail-label">{{ $t('admin.data.preview.exportedAt') }}</span>
          <span class="adm-detail-value">{{ previewExportedAt }}</span>
        </div>
      </div>
      <div class="adm-form-group" style="margin-top: 16px">
        <label class="adm-radio-group">
          <input v-model="strategy" type="radio" value="merge" />
          <span>
            <strong>{{ $t('admin.data.preview.strategy.merge') }}</strong>
            <small>{{ $t('admin.data.preview.strategy.mergeDesc') }}</small>
          </span>
        </label>
        <label class="adm-radio-group">
          <input v-model="strategy" type="radio" value="overwrite" />
          <span>
            <strong>{{ $t('admin.data.preview.strategy.overwrite') }}</strong>
            <small>{{ $t('admin.data.preview.strategy.overwriteDesc') }}</small>
          </span>
        </label>
      </div>
      <template #footer>
        <div class="adm-modal-actions">
          <button type="button" class="adm-btn adm-btn--secondary" @click="previewOpen = false">
            {{ $t('admin.common.cancel') }}
          </button>
          <button type="button" class="adm-btn adm-btn--danger" :disabled="previewBusy" @click="requestImport">
            {{ $t('admin.data.preview.confirm') }}
          </button>
        </div>
      </template>
    </AppModal>

    <!-- 全量覆盖二次确认 -->
    <ConfirmDialog
      v-model:open="overwriteConfirmOpen"
      :title="$t('admin.data.preview.strategy.overwrite')"
      :content="$t('admin.data.preview.overwriteConfirm')"
      :cancel-text="$t('admin.common.cancel')"
      :confirm-text="$t('admin.common.confirm')"
      danger
      @confirm="executeImport"
    />
  </div>
</template>
