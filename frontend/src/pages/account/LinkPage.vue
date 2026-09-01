<script setup lang="ts">
// 账户绑定确认页（迁移自 modules/account/pages/link.html + link.ts + link.css）。
// 后端同邮箱登录流程（Google/Microsoft 共用 handleLoginAction）签发一次性 link_token
// 并重定向到 /account/link；本页拉取 /api/auth/pending-link 展示双方账户，
// 确认后 POST /api/auth/confirm-link 并硬跳 Dashboard。
// Provider 由服务端 pending 状态解析（随一次性 token 存储），前端不感知、URL 不携带。
// 加载失败不沿用旧版"弹窗 + 2 秒强跳登录"，改为就地错误态 + 手动返回按钮
// （对齐 VerifyPage/OAuthPage 的迁移规范）。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiClientError, get, post } from '@/api/client'
import AuthCard from '@/components/AuthCard.vue'
import AppButton from '@/components/AppButton.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'

// pending-link 响应：第三方侧字段名按实际 Provider 返回（microsoftName / googleName 二选一）
interface PendingLinkRaw {
  microsoftName?: string
  microsoftAvatar?: string
  googleName?: string
  googleAvatar?: string
  username: string
  userAvatar?: string
}

const router = useRouter()

const loading = ref(true)
const info = ref<PendingLinkRaw | null>(null)
const errorKey = ref('')
const submitting = ref(false)

const providerName = computed(() => info.value?.microsoftName ?? info.value?.googleName ?? '-')
const providerAvatar = computed(() => info.value?.microsoftAvatar ?? info.value?.googleAvatar)
const username = computed(() => info.value?.username ?? '-')
const userAvatar = computed(() => info.value?.userAvatar)

// 拉取待绑定信息失败的错误码映射（展示后由用户手动返回登录页）
const LOAD_ERROR_MAP: Record<string, string> = {
  INVALID_TOKEN: 'account.linkConfirm.invalidLink',
  TOKEN_EXPIRED: 'account.linkConfirm.linkExpired',
  USER_NOT_FOUND: 'error.sessionError',
}

// 确认绑定失败 → 留在本页展示原因（按钮恢复可用）
const CONFIRM_ERROR_MAP: Record<string, string> = {
  INVALID_TOKEN: 'account.linkConfirm.invalidLink',
  TOKEN_EXPIRED: 'account.linkConfirm.linkExpired',
  MICROSOFT_ALREADY_LINKED: 'account.dashboard.microsoftAlreadyLinked',
  GOOGLE_ALREADY_LINKED: 'account.dashboard.googleAlreadyLinked',
  USER_NOT_FOUND: 'error.sessionError',
  USER_BANNED: 'account.linkConfirm.userBanned',
}

function initialOf(name: string): string {
  return name && name !== '-' ? name.charAt(0).toUpperCase() : '-'
}

// 加载失败：就地展示错误，由用户手动返回登录页（SPA 化适配，去掉旧版 2 秒强跳）
function failToLogin(key: string): void {
  errorKey.value = key
  loading.value = false
}

async function confirmLink(): Promise<void> {
  if (submitting.value) return
  submitting.value = true
  errorKey.value = ''
  try {
    await post('/api/auth/confirm-link')
    // 服务端已下发新会话 Cookie，硬跳转确保 Dashboard 以新登录态加载
    window.location.href = '/account/dashboard'
  } catch (e) {
    const code = e instanceof ApiClientError ? e.errorCode : ''
    errorKey.value = CONFIRM_ERROR_MAP[code] ?? 'account.linkConfirm.linkFailed'
    submitting.value = false
  }
}

function cancel(): void {
  router.push({ name: 'login' })
}

onMounted(async () => {
  try {
    const data = await get<PendingLinkRaw>('/api/auth/pending-link')
    if (!data?.username) {
      failToLogin('account.linkConfirm.linkFailed')
      return
    }
    info.value = data
  } catch (e) {
    const code = e instanceof ApiClientError ? e.errorCode : ''
    failToLogin(LOAD_ERROR_MAP[code] ?? 'account.linkConfirm.linkFailed')
    return
  }
  loading.value = false
})
</script>

<template>
  <AuthCard
    num="06"
    :title="$t('account.linkConfirm.title')"
    :subtitle="info ? $t('account.linkConfirm.subtitle') : ''"
  >
    <!-- Loading -->
    <div v-if="loading" class="link-loading">
      <div class="loader-spinner"></div>
    </div>

    <!-- 加载失败：就地错误态，手动返回登录页 -->
    <div v-else-if="!info" class="link-error-state">
      <p class="link-error-text">{{ $t(errorKey) }}</p>
      <AppButton variant="secondary" @click="cancel">
        {{ $t('account.linkConfirm.backToLogin') }}
      </AppButton>
    </div>

    <template v-else>
      <!-- 账户绑定可视化 -->
      <div class="link-visual">
        <!-- 已有账户 -->
        <div class="link-avatar-item">
          <div class="link-avatar">
            <img v-if="userAvatar" :src="userAvatar" :alt="username" />
            <template v-else>{{ initialOf(username) }}</template>
          </div>
          <span class="link-avatar-label">{{ username }}</span>
        </div>

        <!-- 链接图标 -->
        <div class="link-icon">
          <svg viewBox="0 0 24 24" fill="currentColor"><path d="M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z"/></svg>
        </div>

        <!-- 第三方账户 -->
        <div class="link-avatar-item">
          <div class="link-avatar">
            <img v-if="providerAvatar" :src="providerAvatar" :alt="providerName" />
            <template v-else>{{ initialOf(providerName) }}</template>
          </div>
          <span class="link-avatar-label">{{ providerName }}</span>
        </div>
      </div>

      <p class="link-warning">{{ $t('account.linkConfirm.warning') }}</p>

      <!-- 确认失败原因 -->
      <p v-if="errorKey" class="link-error-text">{{ $t(errorKey) }}</p>

      <div class="button-group">
        <AppButton :disabled="submitting" @click="confirmLink">
          {{ $t('account.linkConfirm.confirmLink') }}
        </AppButton>
        <AppButton variant="secondary" :disabled="submitting" @click="cancel">
          {{ $t('account.linkConfirm.cancel') }}
        </AppButton>
      </div>
    </template>
  </AuthCard>

  <PolicyFooter />
</template>

<style scoped>
/* ---- 加载状态（迁移自 common.css .loader-spinner，值不变） ---- */
.link-loading {
  display: flex;
  justify-content: center;
  align-items: center;
  margin: 40px 0;
}

.loader-spinner {
  width: 32px;
  height: 32px;
  border: 2px solid var(--line);
  border-top-color: var(--fg);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* ---- 账户绑定可视化（迁移自 link.css .link-* 系列，值不变） ---- */
.link-visual {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 24px;
  margin: 40px 0;
}

.link-avatar-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  flex: 1;
}

.link-avatar {
  width: 80px;
  height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: var(--line);
  border: 1px solid var(--dim);
  font-family: var(--font-display);
  font-size: 32px;
  font-weight: 700;
  color: var(--fg);
}

.link-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.link-avatar-label {
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  color: var(--mid);
  text-align: center;
}

.link-icon {
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg);
  opacity: 0.6;
  flex-shrink: 0;
}

/* ---- 警告文本（迁移自 link.css .link-warning，值不变） ---- */
.link-warning {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
  line-height: 1.8;
  text-align: center;
  margin: 24px 0;
}

/* ---- 按钮组（迁移自 general.css .button-group，值不变） ---- */
.button-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* ---- 错误态（迁移自 verify.css .error-text，值不变；布局对齐 VerifyPage 错误态） ---- */
.link-error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  margin: 40px 0;
}

.link-error-text {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
  text-align: center;
  margin-bottom: 24px;
}
</style>
