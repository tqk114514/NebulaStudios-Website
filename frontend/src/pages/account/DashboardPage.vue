<script setup lang="ts">
// 个人中心 Dashboard（迁移自 modules/account/pages/dashboard.html + assets/js/dashboard.ts）。
// 样式自持：弹窗来自 AppModal，按钮来自 AppButton，表单字段来自 FormField，
// 本页全部专属样式收编在本页 scoped 中（命名 dash-*，值照旧）。
// 覆盖：用户信息展示、头像管理、微软/Google 绑定解绑、修改密码、删除账户、登出、数据导出、
//      用户操作日志分页、OAuth 已授权应用、政策同意检查。
import { ref, reactive, computed, onMounted, watch, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'
import FormField from '@/components/FormField.vue'
import CaptchaWidget from '@/components/CaptchaWidget.vue'
import { i18n } from '@/i18n'
import { fetchMe, logout as logoutApi } from '@/api/auth'
import { errorKey } from '@/api/errorCodes'
import type { Me } from '@/api/auth'
import {
  updateAvatar,
  unlinkMicrosoft,
  unlinkGoogle,
  changePassword,
  sendDeleteCode as requestDeleteCode,
  deleteAccount,
  requestDataExport,
  fetchUserLogs,
  fetchOAuthGrants,
  revokeOAuthGrant,
  type UserLogItem,
  type OAuthGrant,
} from '@/api/dashboard'
import { loadCaptchaConfig, getCaptchaToken, isCaptchaEnabled, resetCaptchaToken } from '@/composables/useCaptcha'
import { useCountdown } from '@/composables/useCountdown'
import { usePolicyConsent } from '@/composables/usePolicyConsent'
import { holdPageLoader, releasePageLoader } from '@/composables/usePageLoader'
import { CDN_URL } from '@/config/cdn'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { check: checkConsent } = usePolicyConsent()

/** i18n t 助手（允许字符串 key + 命名参数），供 script 内格式化时间/文案 */
const gt = (key: string, params?: Record<string, unknown>): string =>
  (i18n.global.t as (k: string, p?: Record<string, unknown>) => string)(key, params)

// ==================== 用户信息 ====================
const user = ref<Me | null>(null)

const avatarDisplayUrl = computed(() => {
  const u = user.value
  if (!u) return ''
  if (u.avatar_url === 'microsoft') return u.microsoft_avatar_url ?? ''
  if (u.avatar_url === 'google') return u.google_avatar_url ?? ''
  return u.avatar_url || ''
})

function isBanned(u: Me | null): boolean {
  if (!u?.is_banned) return false
  if (u.unban_at && new Date(u.unban_at) < new Date()) return false
  return true
}
const banned = computed(() => isBanned(user.value))

function formatDateTime(dateStr: string | null | undefined): string {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString(i18n.global.locale.value as string, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const msSyncDisabled = computed(() => user.value?.microsoft_avatar_sync === false)

function microsoftName(): string {
  const u = user.value
  if (!u) return ''
  if (u.microsoft_id && u.microsoft_name) return u.microsoft_name
  if (u.microsoft_id) return gt('account.dashboard.linked')
  return gt('account.dashboard.notLinked')
}
function googleName(): string {
  const u = user.value
  if (!u) return ''
  if (u.google_id && u.google_name) return u.google_name
  if (u.google_id) return gt('account.dashboard.linked')
  return gt('account.dashboard.notLinked')
}

async function refreshUser() {
  try {
    user.value = await fetchMe()
  } catch {
    // 刷新失败不打断（保底用旧信息）
  }
}

// ==================== 弹窗状态 ====================
const showAlert = ref(false)
const alertMessage = ref('')
function setAlert(key: string) {
  alertMessage.value = key
  showAlert.value = true
}

const showConfirm = ref(false)
const confirmTitle = ref('')
const confirmMessage = ref('')
const confirmParams = ref<Record<string, unknown> | null>(null)
const confirmDanger = ref(false)
let confirmCallback: ((v: boolean) => void) | null = null
watch(showConfirm, (v) => {
  if (!v && confirmCallback) {
    const cb = confirmCallback
    confirmCallback = null
    cb(false)
  }
})
function confirm(
  messageKey: string,
  titleKey: string,
  options?: { danger?: boolean; params?: Record<string, unknown> },
): Promise<boolean> {
  return new Promise((resolve) => {
    confirmMessage.value = messageKey
    confirmTitle.value = titleKey
    confirmParams.value = options?.params ?? null
    confirmDanger.value = options?.danger ?? false
    confirmCallback = resolve
    showConfirm.value = true
  })
}
function resolveConfirm(v: boolean) {
  showConfirm.value = false
  confirmCallback?.(v)
  confirmCallback = null
}

// ==================== 验证器（迁移自 lib/validators.ts）====================
function validatePassword(pw: string): string | null {
  if (pw.length < 16 || pw.length > 64) return 'account.register.passwordLength'
  if (!/\d/.test(pw)) return 'account.register.passwordNumber'
  if (!/[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/.test(pw)) return 'account.register.passwordSpecial'
  if (!/[a-z]/.test(pw) || !/[A-Z]/.test(pw)) return 'account.register.passwordCase'
  return null
}

function validateAvatarUrl(url: string): string | null {
  if (!url || !url.trim()) return 'account.dashboard.invalidUrl'
  const v = url.trim()
  // 不放行 data: URL：自定义头像仅存第三方图床 URL（隐私政策 2.2.1），base64 内联等价于直接存图
  if (v.length > 2048) return 'account.dashboard.invalidUrl'
  let parsed: URL
  try {
    parsed = new URL(v)
  } catch {
    return 'account.dashboard.invalidUrl'
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) return 'account.dashboard.invalidUrl'
  const host = parsed.hostname.toLowerCase()
  const blocked = [
    /^localhost$/i,
    /^127\.\d+\.\d+\.\d+$/,
    /^10\.\d+\.\d+\.\d+$/,
    /^172\.(1[6-9]|2\d|3[01])\.\d+\.\d+$/,
    /^192\.168\.\d+\.\d+$/,
    /^0\.0\.0\.0$/,
  ]
  if (blocked.some((r) => r.test(host))) return 'account.dashboard.invalidUrl'
  const specialDomains = ['graph.microsoft.com']
  const isSpecial = specialDomains.some((d) => host === d || host.endsWith('.' + d))
  if (!isSpecial) {
    const exts = ['.jpg', '.jpeg', '.png', '.gif', '.webp', '.bmp', '.ico']
    if (!exts.some((e) => parsed.pathname.toLowerCase().endsWith(e))) return 'account.dashboard.invalidImageUrl'
  }
  return null
}

function loadImage(src: string): Promise<boolean> {
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => resolve(true)
    img.onerror = () => resolve(false)
    img.src = src
  })
}

// ==================== 头像管理 ====================
const avatarOpen = ref(false)
const avatarInput = ref('')
const avatarReadonly = ref(false)
const avatarError = ref('')
const avatarPreviewSrc = ref('')
const avatarSendValue = ref<string | null>(null)

function resetAvatarWidget() {
  avatarInput.value = ''
  avatarReadonly.value = false
  avatarError.value = ''
  avatarPreviewSrc.value = ''
  avatarSendValue.value = null
}

function openAvatarModal() {
  resetAvatarWidget()
  avatarOpen.value = true
}

function applyValidAvatar(value: string, preview: string) {
  avatarSendValue.value = value
  avatarPreviewSrc.value = preview
  avatarError.value = ''
}

async function loadIntoPreview(value: string, preview: string) {
  const ok = await loadImage(preview)
  if (ok) applyValidAvatar(value, preview)
  else avatarError.value = 'account.dashboard.avatarLoadFailed'
}

async function onAvatarInput() {
  avatarError.value = ''
  avatarPreviewSrc.value = ''
  avatarSendValue.value = null
  const v = avatarInput.value.trim()
  if (!v) return
  const err = validateAvatarUrl(v)
  if (err) {
    avatarError.value = err
    return
  }
  await loadIntoPreview(v, v)
}

function selectMicrosoftAvatar() {
  const u = user.value
  const url = u?.microsoft_avatar_url
  if (!url) return
  avatarInput.value = gt('account.dashboard.useMicrosoftAvatar')
  avatarReadonly.value = true
  void loadIntoPreview('microsoft', url)
}

// 占位态（使用微软/Google头像）的输入框再次聚焦时清空，回到可输入状态
// （迁移自旧版 handleFocus：清输入、解除只读、清预览、清待发送值）
function onAvatarInputFocus() {
  if (avatarReadonly.value) resetAvatarWidget()
}
function selectGoogleAvatar() {
  const u = user.value
  const url = u?.google_avatar_url
  if (!url) return
  avatarInput.value = gt('account.dashboard.useGoogleAvatar')
  avatarReadonly.value = true
  void loadIntoPreview('google', url)
}

async function removeAvatar() {
  const u = user.value
  if (!u) return
  // 微软头像同步被关闭时，删除按钮变为"恢复同步"
  if (msSyncDisabled.value) {
    const ok = await confirm('account.dashboard.restoreAvatarSyncConfirm', 'account.dashboard.restoreAvatarSync')
    if (!ok) return
    try {
      await updateAvatar('microsoft')
    } catch {
      setAlert('account.dashboard.avatarUpdateFailed')
      return
    }
    setAlert('account.dashboard.restoreAvatarSyncPending')
    sessionStorage.setItem('avatar-dialog-pending', '1')
    window.location.href =
      '/api/auth/microsoft?action=login&return=' + encodeURIComponent('/account/dashboard')
    return
  }

  const ok = await confirm('account.dashboard.removeAvatarConfirm', 'account.dashboard.removeAvatar', { danger: true })
  if (!ok) return
  try {
    await updateAvatar('')
    avatarOpen.value = false
    await refreshUser()
    setAlert('account.dashboard.avatarUpdateSuccess')
  } catch {
    setAlert('account.dashboard.avatarUpdateFailed')
  }
}

async function submitAvatar() {
  const value = avatarSendValue.value
  if (!value) return
  try {
    const res = await updateAvatar(value)
    avatarOpen.value = false
    await refreshUser()
    if (res?.avatar_url !== undefined) {
      // 值微调：其实 refreshUser 已刷新，这里不再覆盖展示逻辑
    }
    setAlert('account.dashboard.avatarUpdateSuccess')
  } catch (e) {
    avatarError.value = mapAvatarError(e)
  }
}

function mapAvatarError(e: unknown): string {
  if (typeof e === 'object' && e && 'errorCode' in e) {
    const code = (e as { errorCode: string }).errorCode
    if (code === 'INVALID_IMAGE_URL') return 'account.dashboard.invalidImageUrl'
    if (code === 'INVALID_URL' || code === 'URL_TOO_LONG') return 'account.dashboard.invalidUrl'
  }
  return 'account.dashboard.avatarUpdateFailed'
}

// ==================== 微软 / Google 绑定解绑 ====================
async function handleMicrosoftToggle() {
  const u = user.value
  if (!u) return
  if (u.microsoft_id) {
    const ok = await confirm('account.dashboard.confirmUnlink', 'account.dashboard.unlinkThirdParty', { danger: true })
    if (!ok) return
    try {
      await unlinkMicrosoft()
      await refreshUser()
      setAlert('account.dashboard.unlinkSuccess')
    } catch {
      setAlert('account.dashboard.unlinkFailed')
    }
  } else {
    const ok = await confirm('account.dashboard.confirmLink', 'account.dashboard.linkThirdParty')
    if (!ok) return
    window.location.href =
      '/api/auth/microsoft?action=link&return=' + encodeURIComponent('/account/dashboard')
  }
}

async function handleGoogleToggle() {
  const u = user.value
  if (!u) return
  if (u.google_id) {
    const ok = await confirm('account.dashboard.confirmUnlinkGoogle', 'account.dashboard.unlinkThirdParty', { danger: true })
    if (!ok) return
    try {
      await unlinkGoogle()
      await refreshUser()
      setAlert('account.dashboard.unlinkSuccessGoogle')
    } catch {
      setAlert('account.dashboard.unlinkFailed')
    }
  } else {
    const ok = await confirm('account.dashboard.confirmLinkGoogle', 'account.dashboard.linkThirdParty')
    if (!ok) return
    window.location.href =
      '/api/auth/google?action=link&return=' + encodeURIComponent('/account/dashboard')
  }
}

// ==================== 登出 / 数据导出 ====================
async function handleLogout() {
  const ok = await confirm('account.dashboard.confirmLogout', 'account.dashboard.logout')
  if (!ok) return
  try {
    await logoutApi()
  } catch {
    // 忽略登出失败，强制回登录页
  }
  // 清空前端会话态，否则 guestOnly 守卫仍视作已登录把跳转弹回 dashboard
  auth.clear()
  router.replace('/account/login')
}

async function handleDataExport() {
  const ok = await confirm('account.dashboard.dataExportConfirm', 'account.dashboard.dataExport')
  if (!ok) return
  try {
    const res = await requestDataExport()
    const url = `/api/user/export/${encodeURIComponent(res.token)}`
    const link = document.createElement('a')
    link.href = url
    link.download = 'user-data.txt'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    setAlert('account.dashboard.dataExportSuccess')
  } catch (e) {
    if (typeof e === 'object' && e && 'errorCode' in e && (e as { errorCode: string }).errorCode === 'RATE_LIMIT') {
      setAlert('account.dashboard.dataExportRateLimit')
    } else {
      setAlert('account.dashboard.dataExportFailed')
    }
  }
}

// ==================== 修改密码 ====================
const passwordOpen = ref(false)
const passForm = reactive({ currentPassword: '', newPassword: '', confirmPassword: '' })
const passwordError = ref('')
const passwordSubmitting = ref(false)
const passwordCaptchaKey = ref(0)
const passReq = reactive({ length: false, number: false, special: false, ccase: false })

const SPECIAL_RE = /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/
function updatePassReq() {
  const p = passForm.newPassword
  passReq.length = p.length >= 16 && p.length <= 64
  passReq.number = /\d/.test(p)
  passReq.special = SPECIAL_RE.test(p)
  passReq.ccase = /[a-z]/.test(p) && /[A-Z]/.test(p)
}
const passCanSubmit = computed(
  () =>
    passForm.currentPassword.length > 0 &&
    passForm.newPassword.length > 0 &&
    passForm.confirmPassword.length > 0 &&
    passReq.length &&
    passReq.number &&
    passReq.special &&
    passReq.ccase,
)

function openPasswordModal() {
  passForm.currentPassword = ''
  passForm.newPassword = ''
  passForm.confirmPassword = ''
  passwordError.value = ''
  passwordCaptchaKey.value++
  updatePassReq()
  passwordOpen.value = true
}

async function submitPassword() {
  const pwdErr = validatePassword(passForm.newPassword)
  if (pwdErr) {
    passwordError.value = pwdErr
    return
  }
  if (passForm.newPassword !== passForm.confirmPassword) {
    passwordError.value = 'account.register.passwordMismatch'
    return
  }
  if (isCaptchaEnabled() && !getCaptchaToken()) {
    passwordError.value = 'account.register.humanVerifyFailed'
    return
  }
  passwordSubmitting.value = true
  passwordError.value = ''
  try {
    await changePassword({
      currentPassword: passForm.currentPassword,
      newPassword: passForm.newPassword,
      captchaToken: getCaptchaToken(),
    })
    passwordOpen.value = false
    setAlert('account.dashboard.changePasswordSuccess')
    // 修改成功后强制重新登录
    setTimeout(() => {
      try {
        void logoutApi()
      } catch {
        // 忽略
      }
      router.replace('/account/login')
    }, 1500)
  } catch (e) {
    passwordError.value = mapPasswordError(e)
  } finally {
    passwordSubmitting.value = false
    resetCaptchaToken()
    passwordCaptchaKey.value++
  }
}

function mapPasswordError(e: unknown): string {
  if (typeof e === 'object' && e && 'errorCode' in e) {
    const code = (e as { errorCode: string }).errorCode
    if (code === 'WRONG_PASSWORD') return 'account.dashboard.wrongPassword'
    if (code === 'SAME_PASSWORD') return 'account.dashboard.samePassword'
    if (code === 'CAPTCHA_FAILED') return 'account.register.humanVerifyFailed'
  }
  return errorKey(e) === 'account.login.failed' ? 'account.dashboard.changePasswordFailed' : errorKey(e)
}

// ==================== 删除账户 ====================
const deleteOpen = ref(false)
const deleteCode = ref('')
const deletePassword = ref('')
const deleteError = ref('')
const deleteSubmitting = ref(false)
const deleteSending = ref(false)
const deleteCaptchaKey = ref(0)
const deleteCountdown = useCountdown(60)

function openDeleteModal() {
  deleteCode.value = ''
  deletePassword.value = ''
  deleteError.value = ''
  deleteSubmitting.value = false
  deleteSending.value = false
  deleteCountdown.restoreOnMount('delete_account', user.value?.email ?? '')
  deleteCaptchaKey.value++
  deleteOpen.value = true
}

async function sendDeleteCode() {
  if (deleteSending.value || deleteCountdown.running.value) return
  if (isCaptchaEnabled() && !getCaptchaToken()) {
    deleteError.value = 'account.register.humanVerifyFailed'
    return
  }
  deleteSending.value = true
  deleteError.value = ''
  try {
    await requestDeleteCode({
      captchaToken: getCaptchaToken(),
      language: (i18n.global.locale.value as string) || 'zh-CN',
    })
    deleteCountdown.start('delete_account', user.value?.email ?? '')
    setAlert('account.dashboard.codeSent')
  } catch (e) {
    deleteError.value = mapSendCodeError(e)
  } finally {
    deleteSending.value = false
    resetCaptchaToken()
    deleteCaptchaKey.value++
  }
}

function mapSendCodeError(e: unknown): string {
  if (typeof e === 'object' && e && 'errorCode' in e) {
    const code = (e as { errorCode: string }).errorCode
    if (code === 'CAPTCHA_FAILED') return 'account.register.humanVerifyFailed'
    if (code === 'RATE_LIMIT') return 'error.rateLimitExceeded'
  }
  return 'account.dashboard.sendCodeFailed'
}

const deleteCanSubmit = computed(
  () =>
    deleteCode.value.trim().length > 0 &&
    deletePassword.value.length > 0 &&
    !deleteSubmitting.value,
)

async function submitDelete() {
  if (isCaptchaEnabled() && !getCaptchaToken()) {
    deleteError.value = 'account.register.humanVerifyFailed'
    return
  }
  deleteSubmitting.value = true
  deleteError.value = ''
  try {
    await deleteAccount({
      code: deleteCode.value.trim(),
      password: deletePassword.value,
    })
    deleteOpen.value = false
    setAlert('account.dashboard.deleteSuccess')
    setTimeout(() => {
      router.replace('/account/login')
    }, 1500)
  } catch (e) {
    deleteError.value = mapDeleteError(e)
  } finally {
    deleteSubmitting.value = false
    resetCaptchaToken()
    deleteCaptchaKey.value++
  }
}

function mapDeleteError(e: unknown): string {
  if (typeof e === 'object' && e && 'errorCode' in e) {
    const code = (e as { errorCode: string }).errorCode
    if (code === 'INVALID_CODE') return 'account.dashboard.invalidCode'
    if (code === 'CODE_EXPIRED') return 'account.dashboard.codeExpired'
    if (code === 'WRONG_PASSWORD') return 'account.dashboard.wrongPassword'
  }
  return 'account.dashboard.deleteFailed'
}

onBeforeUnmount(() => {
  deleteCountdown.stop()
})

// ==================== 操作日志 ====================
const logsOpen = ref(false)
const logs = ref<UserLogItem[]>([])
const logsTotal = ref(0)
const logsPage = ref(1)
const logsLoading = ref(false)
const logsEmpty = ref(false)
const logsHasMore = ref(false)
const PAGE_SIZE = 5

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  if (diff < 60 * 1000) return gt('account.dashboard.timeJustNow')
  if (diff < 60 * 60 * 1000) return gt('account.dashboard.timeMinutesAgo', { n: Math.floor(diff / 60000) })
  if (diff < 24 * 60 * 60 * 1000) return gt('account.dashboard.timeHoursAgo', { n: Math.floor(diff / 3600000) })
  if (diff < 7 * 24 * 60 * 60 * 1000) return gt('account.dashboard.timeDaysAgo', { n: Math.floor(diff / 86400000) })
  return new Date(iso).toLocaleString(i18n.global.locale.value as string, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

// 日志 action 是固定内部枚举（register/change_username/...），新增 action 会同步补 i18n key，故直接翻译。
function logActionText(action: string): string {
  return gt('account.dashboard.logAction.' + action)
}

// 日志图标（迁移自旧 dashboard.ts 的 getLogActionIcon；静态受信内容，v-html 渲染安全）
const MS_LOGO = `<img src="${CDN_URL}/images/logo/microsoft/Symbol.svg" alt="Microsoft" width="20" height="20">`
const GOOGLE_LOGO = `<img src="${CDN_URL}/images/logo/google/Symbol.svg" alt="Google" width="20" height="20">`
const LOG_ICONS: Record<string, string> = {
  register:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M15 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm-9-2V7H4v3H1v2h3v3h2v-3h3v-2H6zm9 4c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>',
  change_password:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/></svg>',
  change_username:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>',
  change_avatar:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M21 19V5c0-1.1-.9-2-2-2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2zM8.5 13.5l2.5 3.01L14.5 12l4.5 6H5l3.5-4.5z"/></svg>',
  link_microsoft: MS_LOGO,
  unlink_microsoft: MS_LOGO,
  link_google: GOOGLE_LOGO,
  unlink_google: GOOGLE_LOGO,
  enable_avatar_sync:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"/></svg>',
  disable_avatar_sync:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"/></svg>',
  delete_account:
    '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>',
}
const LOG_ICON_DEFAULT =
  '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z"/></svg>'
function logIcon(action: string): string {
  return LOG_ICONS[action] ?? LOG_ICON_DEFAULT
}

// 封禁理由由服务端硬限制为固定枚举（violation/abuse/malicious/spam，见 admin/user.go 的 allowedReasons），
// 且新增枚举会同步补充 i18n key，故直接翻译，无需兜底。
function banReasonText(reason: string): string {
  if (!reason) return '-'
  return gt('account.dashboard.banReason.' + reason)
}
function logDetailsText(log: UserLogItem): string {
  const d = log.details
  if (!d) return ''
  if (log.action === 'change_username') {
    if (d.old_username && d.new_username) return `${d.old_username} → ${d.new_username}`
    return ''
  }
  if (log.action === 'link_microsoft' || log.action === 'unlink_microsoft') return d.microsoft_name || ''
  if (log.action === 'link_google' || log.action === 'unlink_google') return d.google_name || ''
  return ''
}

async function loadLogs(page: number) {
  logsLoading.value = true
  logsPage.value = page
  try {
    const res = await fetchUserLogs(page, PAGE_SIZE)
    logsTotal.value = res.total ?? 0
    const incoming = res.logs ?? []
    if (page === 1) logs.value = incoming
    else logs.value = [...logs.value, ...incoming]
    logsEmpty.value = page === 1 && logs.value.length === 0
    logsHasMore.value = logs.value.length < logsTotal.value
  } catch {
    logsEmpty.value = logs.value.length === 0
    logsHasMore.value = false
  } finally {
    logsLoading.value = false
  }
}

function openLogsModal() {
  logs.value = []
  logsTotal.value = 0
  logsPage.value = 1
  logsEmpty.value = false
  logsHasMore.value = false
  logsOpen.value = true
  void loadLogs(1)
}

// ==================== 已授权应用 ====================
const grantsOpen = ref(false)
const grants = ref<OAuthGrant[]>([])
const grantsLoading = ref(false)
const grantsEmpty = ref(false)

// OAuth scope 由服务端 validScopes 白名单限制为 openid/profile/email，均有关键 key，故直接翻译。
function scopeText(scope: string): string {
  return scope
    .split(' ')
    .filter((s) => s)
    .map((s) => gt(`account.oauth.scope.${s}.name`))
    .join(', ')
}
function grantTime(iso: string): string {
  return relativeTime(iso)
}

async function loadGrants() {
  grantsLoading.value = true
  grantsEmpty.value = false
  try {
    const res = await fetchOAuthGrants()
    const list = res.grants ?? []
    grants.value = list
    grantsEmpty.value = list.length === 0
  } catch {
    grants.value = []
    grantsEmpty.value = true
  } finally {
    grantsLoading.value = false
  }
}

function openGrantsModal() {
  grants.value = []
  grantsLoading.value = false
  grantsEmpty.value = false
  grantsOpen.value = true
  void loadGrants()
}

async function revokeGrant(grant: OAuthGrant) {
  const ok = await confirm(
    'account.dashboard.oauthRevokeConfirm',
    'account.dashboard.oauthRevoke',
    { danger: true, params: { name: grant.client_name } },
  )
  if (!ok) return
  try {
    await revokeOAuthGrant(grant.client_id)
    grants.value = grants.value.filter((g) => g.client_id !== grant.client_id)
    grantsEmpty.value = grants.value.length === 0
    setAlert('account.dashboard.oauthRevokeSuccess')
  } catch {
    setAlert('account.dashboard.oauthRevokeFailed')
  }
}

// ==================== OAuth 绑定结果提示 ====================
const BIND_SUCCESS: Record<string, string> = {
  microsoft_linked: 'account.dashboard.linkSuccess',
  google_linked: 'account.dashboard.linkSuccessGoogle',
}
const BIND_ERROR: Record<string, string> = {
  microsoft_already_linked: 'account.dashboard.microsoftAlreadyLinked',
  google_already_linked: 'account.dashboard.googleAlreadyLinked',
  session_expired: 'error.sessionExpired',
  user_banned: 'account.error.userBanned',
}

// ==================== 初始化 ====================
onMounted(async () => {
  // 数据加载期间持有全局加载遮罩（页面内不再渲染"加载中"文字）
  holdPageLoader()
  try {
    await loadCaptchaConfig()
    try {
      user.value = await fetchMe()
    } catch {
      router.replace('/account/login')
      return
    }

    const consented = await checkConsent()
    if (!consented) {
      router.replace('/account/login')
      return
    }

    // OAuth 绑定回跳提示 & 清除 URL 参数
    const q = route.query
    const succ = typeof q.success === 'string' ? q.success : ''
    const err = typeof q.error === 'string' ? q.error : ''
    const msgKey = (succ && BIND_SUCCESS[succ]) || (err && BIND_ERROR[err]) || ''
    if (msgKey) {
      setTimeout(() => setAlert(msgKey), 100)
      window.history.replaceState({}, '', route.path)
    }

    // 恢复头像同步回跳后自动拉起头像弹窗
    if (sessionStorage.getItem('avatar-dialog-pending') === '1') {
      sessionStorage.removeItem('avatar-dialog-pending')
      openAvatarModal()
    }
  } finally {
    releasePageLoader()
  }
})
</script>

<template>
  <div class="dash-main" :class="{ 'is-banned': banned }">
    <template v-if="user">
      <!-- 封禁信息卡片 -->
      <section v-if="banned" class="dash-banned-info">
        <h3 class="dash-banned-info-title">
          <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/></svg>
          <span>{{ $t('account.dashboard.bannedInfo') }}</span>
        </h3>
        <div class="dash-banned-info-item">
          <span class="dash-banned-info-label">{{ $t('account.dashboard.bannedReason') }}</span>
          <span class="dash-banned-info-value">{{ user.ban_reason ? banReasonText(user.ban_reason) : '-' }}</span>
        </div>
        <div class="dash-banned-info-item">
          <span class="dash-banned-info-label">{{ $t('account.dashboard.bannedAt') }}</span>
          <span class="dash-banned-info-value">{{ formatDateTime(user.banned_at) }}</span>
        </div>
        <div class="dash-banned-info-item">
          <span class="dash-banned-info-label">{{ $t('account.dashboard.unbanAt') }}</span>
          <span class="dash-banned-info-value">{{ user.unban_at ? formatDateTime(user.unban_at) : $t('account.dashboard.permanentBan') }}</span>
        </div>
      </section>

      <!-- 封禁盖章 -->
      <div v-if="banned" class="dash-banned-stamp">
        <div class="dash-banned-stamp-inner">
          <span class="dash-banned-stamp-text">BANNED</span>
        </div>
      </div>

      <!-- 头像与欢迎 -->
      <section class="dash-profile">
        <div class="dash-avatar-large">
          <img v-if="avatarDisplayUrl" :src="avatarDisplayUrl" :alt="user.username" />
          <span v-else>{{ user.username.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="dash-profile-welcome">
          <h1>{{ user.username }}</h1>
          <p class="dash-profile-email">{{ user.email }}</p>
        </div>
      </section>

      <!-- 账户信息 -->
      <section class="dash-section">
        <h2 class="dash-section-title">{{ $t('account.dashboard.accountInfo') }}</h2>
        <div class="dash-list">
          <div class="dash-item">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.username') }}</span>
              <span class="dash-item-value">{{ user.username }}</span>
            </div>
          </div>
          <div class="dash-item">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.email') }}</span>
              <span class="dash-item-value">{{ user.email }}</span>
            </div>
          </div>
          <button type="button" class="dash-item clickable" @click="handleMicrosoftToggle">
            <div class="dash-item-icon">
              <img :src="CDN_URL + '/images/logo/microsoft/Symbol.svg'" alt="Microsoft" width="24" height="24" />
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.microsoftAccount') }}</span>
              <span class="dash-item-value" :class="{ 'is-linked': user.microsoft_id }">{{ microsoftName() }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable" @click="handleGoogleToggle">
            <div class="dash-item-icon">
              <img :src="CDN_URL + '/images/logo/google/Symbol.svg'" alt="Google" width="24" height="24" />
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.googleAccount') }}</span>
              <span class="dash-item-value" :class="{ 'is-linked': user.google_id }">{{ googleName() }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable" @click="openAvatarModal">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M21 19V5c0-1.1-.9-2-2-2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2zM8.5 13.5l2.5 3.01L14.5 12l4.5 6H5l3.5-4.5z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.changeAvatar') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.changeAvatarHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
        </div>
      </section>

      <!-- 已授权应用 -->
      <section class="dash-section">
        <h2 class="dash-section-title">{{ $t('account.dashboard.oauthGrants') }}</h2>
        <div class="dash-list">
          <button type="button" class="dash-item clickable" @click="openGrantsModal">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.oauthGrantsLabel') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.oauthGrantsHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
        </div>
      </section>

      <!-- 安全设置 -->
      <section class="dash-section">
        <h2 class="dash-section-title">{{ $t('account.dashboard.security') }}</h2>
        <div class="dash-list">
          <button type="button" class="dash-item clickable" @click="openPasswordModal">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.changePassword') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.changePasswordHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable" @click="openLogsModal">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 3H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-5 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.userLogs') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.userLogsHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable" @click="handleDataExport">
            <div class="dash-item-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.dataExport') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.dataExportHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable logout-item" @click="handleLogout">
            <div class="dash-item-icon logout-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.logout') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.logoutHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
          <button type="button" class="dash-item clickable delete-account-item" @click="openDeleteModal">
            <div class="dash-item-icon delete-icon">
              <svg viewBox="0 0 24 24" fill="currentColor"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>
            </div>
            <div class="dash-item-content">
              <span class="dash-item-label">{{ $t('account.dashboard.deleteAccount') }}</span>
              <span class="dash-item-hint">{{ $t('account.dashboard.deleteAccountHint') }}</span>
            </div>
            <div class="dash-item-arrow"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg></div>
          </button>
        </div>
      </section>
    </template>

    <!-- 页尾：政策链接 + 版权（原 .policy-links + .dashboard-footer） -->
    <footer class="dash-footer">
      <nav class="dash-footer-links" aria-label="policy">
        <a href="/policy#privacy" target="_blank">{{ $t('policy.privacyPolicy') }}</a>
        <span>|</span>
        <a href="/policy#terms" target="_blank">{{ $t('policy.termsOfService') }}</a>
      </nav>
      <p class="dash-footer-copyright" translate="no">{{ $t('footer.copyright') }}</p>
    </footer>
  </div>

  <!-- 提示弹窗（二级，层级高于业务弹窗） -->
  <AppModal v-model:open="showAlert" :title="$t('modal.alert')" :width="'360px'" :z-index="210">
    <p class="dash-modal-message">{{ $t(alertMessage) }}</p>
    <template #footer>
      <AppButton :arrow="false" @click="showAlert = false">{{ $t('modal.close') }}</AppButton>
    </template>
  </AppModal>

  <!-- 确认弹窗（二级，层级高于业务弹窗） -->
  <AppModal v-model:open="showConfirm" :title="$t(confirmTitle)" :width="'380px'" :z-index="210">
    <p class="dash-modal-message">{{ $t(confirmMessage, confirmParams ?? {}) }}</p>
    <template #footer>
      <AppButton variant="secondary" :arrow="false" @click="resolveConfirm(false)">{{ $t('modal.cancel') }}</AppButton>
      <AppButton :arrow="false" @click="resolveConfirm(true)">{{ $t('modal.confirm') }}</AppButton>
    </template>
  </AppModal>

  <!-- 更改头像弹窗 -->
  <AppModal v-model:open="avatarOpen" :title="$t('account.dashboard.changeAvatar')" :width="'420px'">
    <div class="dash-avatar-row">
      <div class="dash-avatar-item">
        <div class="dash-avatar-preview current">
          <img v-if="avatarDisplayUrl" :src="avatarDisplayUrl" :alt="user?.username || ''" />
          <span v-else>{{ user?.username?.charAt(0).toUpperCase() || 'U' }}</span>
        </div>
        <span class="dash-avatar-label">{{ $t('account.dashboard.currentAvatar') }}</span>
      </div>
      <div class="dash-avatar-arrow">
        <svg viewBox="0 0 24 24" fill="currentColor"><path d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/></svg>
      </div>
      <div class="dash-avatar-item">
        <div class="dash-avatar-preview new">
          <img v-if="avatarPreviewSrc" :src="avatarPreviewSrc" alt="new" />
          <span v-else>?</span>
        </div>
        <span class="dash-avatar-label">{{ $t('account.dashboard.newAvatar') }}</span>
      </div>
    </div>

    <FormField :label="$t('account.dashboard.avatarUrlPlaceholder')" :error="avatarError ? $t(avatarError) : ''">
      <input
        v-model="avatarInput"
        type="text"
        :readonly="avatarReadonly"
        :placeholder="$t('account.dashboard.avatarUrlPlaceholder')"
        autocomplete="url"
        @focus="onAvatarInputFocus"
        @change="onAvatarInput"
      />
    </FormField>

    <div class="dash-button-group">
      <AppButton v-if="user?.microsoft_avatar_url" variant="secondary" :arrow="false" :disabled="msSyncDisabled" @click="selectMicrosoftAvatar">
        {{ $t('account.dashboard.useMicrosoftAvatar') }}
      </AppButton>
      <AppButton variant="secondary" :arrow="false" @click="removeAvatar">
        {{ msSyncDisabled ? $t('account.dashboard.restoreAvatarSync') : $t('account.dashboard.removeAvatar') }}
      </AppButton>
      <AppButton v-if="user?.google_avatar_url" variant="secondary" :arrow="false" @click="selectGoogleAvatar">
        {{ $t('account.dashboard.useGoogleAvatar') }}
      </AppButton>
    </div>

    <template #footer>
      <AppButton variant="secondary" :arrow="false" @click="avatarOpen = false">{{ $t('modal.cancel') }}</AppButton>
      <AppButton :arrow="false" :disabled="!avatarSendValue" @click="submitAvatar">
        {{ $t('modal.confirm') }}
      </AppButton>
    </template>
  </AppModal>

  <!-- 修改密码弹窗 -->
  <AppModal v-model:open="passwordOpen" :title="$t('account.dashboard.changePassword')" :width="'420px'">
    <FormField :label="$t('account.dashboard.currentPasswordPlaceholder')">
      <input v-model="passForm.currentPassword" type="password" autocomplete="current-password"
        :placeholder="$t('account.dashboard.currentPasswordPlaceholder')" />
    </FormField>
    <FormField :label="$t('account.dashboard.newPasswordPlaceholder')">
      <input v-model="passForm.newPassword" type="password" autocomplete="new-password"
        :placeholder="$t('account.dashboard.newPasswordPlaceholder')" @input="updatePassReq" />
      <div class="dash-password-requirements">
        <span class="dash-req-item" :class="{ 'is-valid': passReq.length }">• {{ $t('account.register.reqLength') }}</span>
        <span class="dash-req-item" :class="{ 'is-valid': passReq.number }">• {{ $t('account.register.reqNumber') }}</span>
        <span class="dash-req-item" :class="{ 'is-valid': passReq.special }">• {{ $t('account.register.reqSpecial') }}</span>
        <span class="dash-req-item" :class="{ 'is-valid': passReq.ccase }">• {{ $t('account.register.reqCase') }}</span>
      </div>
    </FormField>
    <FormField :label="$t('account.dashboard.confirmNewPasswordPlaceholder')">
      <input v-model="passForm.confirmPassword" type="password" autocomplete="new-password"
        :placeholder="$t('account.dashboard.confirmNewPasswordPlaceholder')" />
    </FormField>
    <CaptchaWidget :key="passwordCaptchaKey" />
    <p v-if="passwordError" class="dash-form-error">{{ $t(passwordError) }}</p>
    <template #footer>
      <AppButton variant="secondary" :arrow="false" @click="passwordOpen = false">{{ $t('modal.cancel') }}</AppButton>
      <AppButton :arrow="false" :disabled="!passCanSubmit || passwordSubmitting" @click="submitPassword">
        {{ passwordSubmitting ? $t('account.register.registering') : $t('modal.confirm') }}
      </AppButton>
    </template>
  </AppModal>

  <!-- 删除账户弹窗 -->
  <AppModal v-model:open="deleteOpen" :title="$t('account.dashboard.deleteAccount')" :width="'420px'" content-class="danger">
    <p class="dash-delete-warning">{{ $t('account.dashboard.deleteAccountWarning') }}</p>
    <FormField :label="$t('account.dashboard.verificationCodePlaceholder')">
      <input v-model="deleteCode" type="text" maxlength="6" autocomplete="one-time-code"
        :placeholder="$t('account.dashboard.verificationCodePlaceholder')" />
    </FormField>
    <FormField>
      <AppButton variant="secondary" :arrow="false" :disabled="deleteSending || deleteCountdown.running.value" @click="sendDeleteCode">
        <template v-if="deleteCountdown.running.value">{{ deleteCountdown.remaining }}s</template>
        <template v-else>{{ $t('account.dashboard.sendCode') }}</template>
      </AppButton>
    </FormField>
    <CaptchaWidget :key="deleteCaptchaKey" />
    <FormField :label="$t('account.dashboard.passwordPlaceholder')">
      <input v-model="deletePassword" type="password" autocomplete="current-password"
        :placeholder="$t('account.dashboard.passwordPlaceholder')" />
    </FormField>
    <p v-if="deleteError" class="dash-form-error">{{ $t(deleteError) }}</p>
    <template #footer>
      <AppButton variant="secondary" :arrow="false" @click="deleteOpen = false">{{ $t('modal.cancel') }}</AppButton>
      <AppButton :arrow="false" :disabled="!deleteCanSubmit" @click="submitDelete">
        {{ deleteSubmitting ? $t('account.register.registering') : $t('account.dashboard.confirmDelete') }}
      </AppButton>
    </template>
  </AppModal>

  <!-- 操作日志弹窗 -->
  <AppModal v-model:open="logsOpen" :title="$t('account.dashboard.userLogs')" :width="'520px'">
    <div v-if="logsLoading && logs.length === 0" class="dash-modal-loading">
      <div class="dash-spinner"></div>
    </div>
    <div v-else-if="logsEmpty" class="dash-logs-empty">{{ $t('account.dashboard.userLogsEmpty') }}</div>
    <div v-else class="dash-logs-list">
      <div v-for="log in logs" :key="log.id" class="dash-log-item">
        <div class="dash-log-icon" v-html="logIcon(log.action)"></div>
        <div class="dash-log-content">
          <span class="dash-log-action">{{ logActionText(log.action) }}</span>
          <span v-if="logDetailsText(log)" class="dash-log-meta">{{ logDetailsText(log) }}</span>
        </div>
        <span class="dash-log-time">
          <span class="dash-log-time-relative">{{ relativeTime(log.created_at) }}</span>
          <span class="dash-log-time-absolute">{{ formatDateTime(log.created_at) }}</span>
        </span>
      </div>
      <div v-if="logsLoading" class="dash-modal-loading">
        <div class="dash-spinner"></div>
      </div>
      <AppButton v-if="logsHasMore && !logsLoading" variant="secondary" class="dash-logs-load-more" @click="loadLogs(logsPage + 1)">
        {{ $t('account.dashboard.loadMore') }}
      </AppButton>
    </div>
    <template #footer>
      <AppButton :arrow="false" @click="logsOpen = false">{{ $t('modal.close') }}</AppButton>
    </template>
  </AppModal>

  <!-- 已授权应用弹窗 -->
  <AppModal v-model:open="grantsOpen" :title="$t('account.dashboard.oauthGrantsLabel')" :width="'520px'">
    <div v-if="grantsLoading" class="dash-modal-loading">
      <div class="dash-spinner"></div>
    </div>
    <div v-else-if="grantsEmpty" class="dash-grants-empty">{{ $t('account.dashboard.oauthGrantsEmpty') }}</div>
    <div v-else class="dash-grants-list">
      <div v-for="grant in grants" :key="grant.client_id" class="dash-grant-item">
        <div class="dash-grant-icon">
          <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/></svg>
        </div>
        <div class="dash-grant-content">
          <span class="dash-grant-name">{{ grant.client_name }}</span>
          <span class="dash-grant-scopes">{{ scopeText(grant.scope) }}</span>
        </div>
        <span class="dash-grant-time">
          <span class="dash-grant-time-relative">{{ $t('account.dashboard.oauthAuthorizedAt', { time: grantTime(grant.created_at) }) }}</span>
          <span class="dash-grant-time-absolute">{{ $t('account.dashboard.oauthAuthorizedAt', { time: formatDateTime(grant.created_at) }) }}</span>
        </span>
        <AppButton variant="secondary" size="sm" :block="false" class="dash-grant-revoke" @click="revokeGrant(grant)">
          {{ $t('account.dashboard.oauthRevoke') }}
        </AppButton>
      </div>
    </div>
    <template #footer>
      <AppButton :arrow="false" @click="grantsOpen = false">{{ $t('modal.close') }}</AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
/* ==================== 页面布局（原 .dashboard-main / .page-loader，值不变） ==================== */
.dash-main {
  position: relative;
  z-index: 2;
  width: 100%;
  max-width: 720px;
  margin: 0 auto;
  padding: 0 24px;
  padding-top: calc(60px + 5vh);
  padding-bottom: 24px;
  animation: dash-enter 0.6s cubic-bezier(0.16, 1, 0.3, 1);
}

.dash-main.is-banned {
  opacity: 0.5;
  pointer-events: none;
}

@keyframes dash-enter {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 弹窗内加载动画（原 .user-logs-loading/.oauth-grants-loading + .loader-spinner） */
.dash-modal-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px 0;
}

.dash-spinner {
  width: 32px;
  height: 32px;
  border: 2px solid var(--line);
  border-top-color: var(--fg);
  border-radius: 50%;
  animation: dash-spin 0.8s linear infinite;
}

@keyframes dash-spin {
  to {
    transform: rotate(360deg);
  }
}

/* ==================== 页尾（原 .policy-links + .dashboard-footer，值不变） ==================== */
.dash-footer-links {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 32px;
  margin-top: 24px;
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
}

.dash-footer-links a {
  color: var(--mid);
  transition: color 0.2s;
}

.dash-footer-links a:hover {
  color: var(--fg);
}

.dash-footer-links span {
  color: var(--dim);
}

.dash-footer-copyright {
  margin-top: 32px;
  margin-bottom: 40px;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  text-align: center;
}

/* ==================== 封禁标识（原 .banned-info / .banned-stamp，值不变） ==================== */
.dash-banned-info {
  margin-bottom: 32px;
  padding: 20px;
  border: 1px solid var(--error);
  background: rgba(170, 48, 48, 0.1);
}

.dash-banned-info-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  color: var(--error);
  text-transform: uppercase;
  margin-bottom: 16px;
}

.dash-banned-info-title svg {
  width: 20px;
  height: 20px;
}

.dash-banned-info-item {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 16px;
  margin-bottom: 8px;
  font-size: var(--text-sm);
  letter-spacing: 0.08em;
}

.dash-banned-info-item:last-child {
  margin-bottom: 0;
}

.dash-banned-info-label {
  color: var(--mid);
  flex-shrink: 0;
}

.dash-banned-info-value {
  color: var(--fg);
  text-align: right;
  font-family: var(--font-mono);
}

.dash-banned-stamp {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 100;
}

.dash-banned-stamp-inner {
  transform: rotate(-15deg);
  border: 4px solid var(--error);
  padding: 16px 32px;
  opacity: 0.8;
}

.dash-banned-stamp-text {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: 32px;
  color: var(--error);
  letter-spacing: 0.2em;
  text-transform: uppercase;
}

/* ==================== 用户头部（原 .profile-header / .avatar-large，值不变） ==================== */
.dash-profile {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 40px;
}

.dash-avatar-large {
  width: 80px;
  height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--line);
  border: 1px solid var(--dim);
  font-family: var(--font-display);
  font-size: 32px;
  font-weight: 700;
  color: var(--fg);
  flex-shrink: 0;
}

.dash-avatar-large img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.dash-profile-welcome {
  flex: 1;
  min-width: 0;
}

.dash-profile-welcome h1 {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: clamp(20px, 2.5vw, 24px);
  letter-spacing: -0.02em;
  color: var(--fg);
  margin: 0 0 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dash-profile-email {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ==================== 分区与信息列表（原 .account-section / .info-* 系列，值不变） ==================== */
.dash-section {
  margin-bottom: 32px;
}

.dash-section-title {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  color: var(--mid);
  text-transform: uppercase;
  margin-bottom: 16px;
}

.dash-list {
  border-top: 1px solid var(--line);
}

.dash-item {
  display: flex;
  align-items: center;
  gap: 16px;
  width: 100%;
  padding: 16px 0;
  background: transparent;
  border: none;
  border-bottom: 1px solid var(--line);
  font: inherit;
  color: inherit;
  text-align: left;
  transition: background 0.2s;
}

.dash-item.clickable {
  cursor: pointer;
}

.dash-item.clickable:hover {
  background: rgba(255, 255, 255, 0.03);
}

.dash-item.logout-item:hover,
.dash-item.delete-account-item:hover {
  background: rgba(170, 48, 48, 0.1);
}

/* 行图标（原 .info-icon：32px 容器 + 24px 图形，登出/删除红色） */
.dash-item-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--mid);
  flex-shrink: 0;
}

.dash-item-icon svg,
.dash-item-icon img {
  width: 24px;
  height: 24px;
}

.dash-item-icon.logout-icon,
.dash-item-icon.delete-icon {
  color: var(--error);
}

.dash-item-content {
  flex: 1;
  min-width: 0;
}

.dash-item-label {
  display: block;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--fg);
  margin-bottom: 2px;
}

.dash-item-value {
  display: block;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  font-family: var(--font-mono);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dash-item-value.is-linked {
  color: var(--success);
}

.dash-item-hint {
  display: block;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
}

.dash-item-arrow {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--dim);
  flex-shrink: 0;
}

.dash-item-arrow svg {
  width: 20px;
  height: 20px;
}

/* ==================== 弹窗通用（原 .modal-message / .form-error / .button-group，值不变） ==================== */
.dash-modal-message {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}

.dash-form-error {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--error);
  margin-top: 8px;
}

.dash-button-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
  /* 原 .avatar-modal-content .modal-body 的 padding-bottom */
  margin-bottom: 24px;
}

/* ==================== 头像弹窗（原 .avatar-preview-* 系列，值不变） ==================== */
.dash-avatar-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-bottom: 24px;
}

.dash-avatar-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.dash-avatar-preview {
  width: 80px;
  height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--line);
  border: 1px solid var(--dim);
  font-family: var(--font-display);
  font-size: 32px;
  font-weight: 700;
  color: var(--fg);
}

.dash-avatar-preview.new {
  border-style: dashed;
  opacity: 0.5;
}

.dash-avatar-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.dash-avatar-label {
  font-size: var(--text-xs);
  letter-spacing: 0.12em;
  color: var(--mid);
  text-transform: uppercase;
}

.dash-avatar-arrow {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--dim);
}

.dash-avatar-arrow svg {
  width: 24px;
  height: 24px;
}

/* ==================== 删除账户弹窗（原 .delete-warning，值不变） ==================== */
.dash-delete-warning {
  font-size: var(--text-sm);
  letter-spacing: 0.08em;
  color: var(--error);
  line-height: 1.8;
  margin-bottom: 24px;
}

/* ==================== 密码强度（原 .password-requirements / .req-item，值不变） ==================== */
.dash-password-requirements {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 12px;
}

.dash-req-item {
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
  color: var(--mid);
  transition: color 0.2s;
}

.dash-req-item.is-valid {
  color: var(--success);
}

/* ==================== 操作日志弹窗（原 .user-log-* 系列，值不变） ==================== */
.dash-logs-list {
  max-height: 400px;
  overflow-y: auto;
  -ms-overflow-style: none;
  scrollbar-width: none;
}

.dash-logs-list::-webkit-scrollbar {
  display: none;
}

.dash-log-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
}

.dash-log-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

/* 日志行图标（原 .user-log-icon） */
.dash-log-icon {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--mid);
  flex-shrink: 0;
  margin-top: 2px;
}

.dash-log-icon :deep(svg),
.dash-log-icon :deep(img) {
  width: 20px;
  height: 20px;
}

.dash-log-content {
  flex: 1;
  min-width: 0;
}

.dash-log-action {
  display: block;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--fg);
  margin-bottom: 2px;
}

.dash-log-meta {
  display: block;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  font-family: var(--font-mono);
}

.dash-log-time {
  display: grid;
  justify-items: end;
  font-family: var(--font-mono);
  min-width: 120px;
}

.dash-log-time > span {
  grid-area: 1 / 1;
}

.dash-log-time-relative {
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  opacity: 1;
  transform: translateY(0);
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.dash-log-time-absolute {
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--fg);
  opacity: 0;
  transform: translateY(10px);
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.dash-log-item:hover .dash-log-time-relative {
  opacity: 0;
  transform: translateY(-10px);
}

.dash-log-item:hover .dash-log-time-absolute {
  opacity: 1;
  transform: translateY(0);
}

.dash-logs-empty {
  text-align: center;
  padding: 32px 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
}

.dash-logs-load-more {
  margin-top: 16px;
}

/* ==================== 已授权应用弹窗（原 .oauth-grant-* 系列，值不变） ==================== */
.dash-grants-list {
  max-height: 400px;
  overflow-y: auto;
  -ms-overflow-style: none;
  scrollbar-width: none;
}

.dash-grants-list::-webkit-scrollbar {
  display: none;
}

.dash-grant-item {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
}

.dash-grant-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

/* 授权项图标（原 .oauth-grant-icon） */
.dash-grant-icon {
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--mid);
  flex-shrink: 0;
}

.dash-grant-icon svg {
  width: 20px;
  height: 20px;
}

.dash-grant-content {
  flex: 1;
  min-width: 0;
}

.dash-grant-name {
  display: block;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--fg);
}

.dash-grant-scopes {
  display: block;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  margin-top: 2px;
}

.dash-grant-time {
  display: grid;
  justify-items: end;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  font-family: var(--font-mono);
  flex-shrink: 0;
  min-width: 100px;
  text-align: right;
}

.dash-grant-time > span {
  grid-area: 1 / 1;
}

.dash-grant-time-relative {
  color: var(--mid);
  opacity: 1;
  transform: translateY(0);
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.dash-grant-time-absolute {
  color: var(--fg);
  opacity: 0;
  transform: translateY(10px);
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.dash-grant-item:hover .dash-grant-time-relative {
  opacity: 0;
  transform: translateY(-10px);
}

.dash-grant-item:hover .dash-grant-time-absolute {
  opacity: 1;
  transform: translateY(0);
}

.dash-grant-revoke {
  flex-shrink: 0;
}

.dash-grants-empty {
  text-align: center;
  padding: 32px 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
}
</style>
