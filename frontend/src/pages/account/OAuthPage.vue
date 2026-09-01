<script setup lang="ts">
// OAuth 授权确认页（对齐原站 modules/account/pages/oauth.html + oauth.ts）。
// 样式自持：骨架来自 AuthCard，允许/拒绝按钮来自 AppButton，
// 页面专属样式（应用信息、用户信息、权限列表、提示文本、加载 spinner）在本页 scoped 中。
// 通过 URL query 携带授权参数，拉取 /oauth/authorize/info 展示应用与权限，
// 用户在允许/拒绝后 POST /oauth/authorize，成功则跳转 redirect_url。
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AuthCard from '@/components/AuthCard.vue'
import AppButton from '@/components/AppButton.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'

interface AuthorizeInfo {
  clientName: string
  clientDescription?: string
  scopes: string[]
  username: string
  userAvatar?: string
}

interface AuthorizeResult {
  redirect_url?: string
  errorCode?: string
}

const route = useRoute()
const auth = useAuthStore()

const loading = ref(true)
const info = ref<AuthorizeInfo | null>(null)
const errorKey = ref('')
const submitting = ref(false)

const SCOPE_NAMES: Record<string, string> = { openid: 'openid', profile: 'profile', email: 'email' }

function scopeName(scope: string): string {
  return SCOPE_NAMES[scope] ?? scope
}

async function loadInfo() {
  const q = route.query
  if (!q.client_id || !q.redirect_uri || !q.scope) {
    errorKey.value = 'account.oauth.error.invalidRequest'
    loading.value = false
    return
  }
  try {
    const params = new URLSearchParams()
    for (const k of ['client_id', 'redirect_uri', 'scope', 'state', 'code_challenge', 'code_challenge_method']) {
      const v = q[k]
      if (v) params.append(k, v as string)
    }
    const res = await fetch(`/oauth/authorize/info?${params.toString()}`, { credentials: 'same-origin' })
    const data = await res.json()
    if (data.success && data.data) {
      info.value = data.data
      await auth.bootstrap()
    } else {
      errorKey.value = mapError(data.errorCode ?? '')
    }
  } catch {
    errorKey.value = 'account.oauth.error.serverError'
  } finally {
    loading.value = false
  }
}

async function submitDecision(decision: 'approve' | 'deny') {
  submitting.value = true
  try {
    const form = new URLSearchParams()
    for (const k of ['client_id', 'redirect_uri', 'scope', 'state', 'code_challenge', 'code_challenge_method']) {
      const v = route.query[k]
      if (v) form.append(k, v as string)
    }
    form.append('decision', decision)

    const res = await fetch('/oauth/authorize', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: form.toString(),
    })
    const data = (await res.json()) as AuthorizeResult
    if (res.ok && data.redirect_url) {
      window.location.href = data.redirect_url
      return
    }
    errorKey.value = mapError(data.errorCode ?? 'accessDenied')
    submitting.value = false
  } catch {
    errorKey.value = 'account.oauth.error.serverError'
    submitting.value = false
  }
}

function mapError(code: string): string {
  const map: Record<string, string> = {
    invalid_request: 'account.oauth.error.invalidRequest',
    invalid_client: 'account.oauth.error.invalidClient',
    invalid_scope: 'account.oauth.error.invalidScope',
    access_denied: 'account.oauth.error.accessDenied',
    unauthorized: 'account.oauth.error.unauthorized',
    server_error: 'account.oauth.error.serverError',
    unsupported_response_type: 'account.oauth.error.unsupportedResponseType',
  }
  return map[code] ?? 'account.oauth.error.unknown'
}

onMounted(() => {
  loadInfo()
})
</script>

<template>
  <AuthCard
    num="05"
    :title="loading ? '' : $t(info ? 'account.oauth.authorize.title' : 'account.oauth.error.title')"
    :subtitle="!loading && info ? $t('account.oauth.authorize.subtitle') : ''"
  >
    <!-- Loading -->
    <div v-if="loading" class="oauth-loading">
      <div class="loader-spinner"></div>
    </div>

    <template v-else-if="info">
      <!-- 应用信息 -->
      <div class="oauth-app-info">
        <div class="oauth-app-icon">
          <svg viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z" />
          </svg>
        </div>
        <div class="oauth-app-name">{{ info.clientName }}</div>
        <div v-if="info.clientDescription" class="oauth-app-desc">{{ info.clientDescription }}</div>
      </div>

      <!-- 当前用户 -->
      <div class="oauth-user-info">
        <span>{{ $t('account.oauth.authorize.loginAs') }}</span>
        <div class="oauth-user-avatar">{{ info.username.charAt(0).toUpperCase() }}</div>
        <span class="oauth-user-name">{{ info.username }}</span>
      </div>

      <!-- 权限列表 -->
      <div class="oauth-scopes">
        <p class="oauth-scopes-title">{{ $t('account.oauth.authorize.permissions') }}</p>
        <ul class="oauth-scope-list">
          <li v-for="s in info.scopes" :key="s" class="oauth-scope-item">
            <div class="oauth-scope-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M5 12l4 4L19 6" stroke-linecap="square" />
              </svg>
            </div>
            <div class="oauth-scope-text">
              <div class="oauth-scope-name">{{ $t(`account.oauth.scope.${scopeName(s)}.name`) }}</div>
              <div class="oauth-scope-desc">{{ $t(`account.oauth.scope.${scopeName(s)}.desc`) }}</div>
            </div>
          </li>
        </ul>
      </div>

      <!-- 按钮组 -->
      <div class="button-group">
        <AppButton :disabled="submitting" @click="submitDecision('approve')">
          {{ $t('account.oauth.authorize.allow') }}
        </AppButton>
        <AppButton variant="secondary" :disabled="submitting" @click="submitDecision('deny')">
          {{ $t('account.oauth.authorize.deny') }}
        </AppButton>
      </div>

      <p class="oauth-notice">{{ $t('account.oauth.authorize.notice') }}</p>
    </template>

    <!-- 错误态 -->
    <template v-else>
      <p class="oauth-error-text">{{ $t(errorKey) }}</p>
    </template>
  </AuthCard>

  <PolicyFooter />
</template>

<style scoped>
/* ---- 加载状态（迁移自 common.css .loader-spinner，值不变） ---- */
.oauth-loading {
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

/* ---- 应用信息（迁移自 oauth.css .oauth-app-* 系列，值不变） ---- */
.oauth-app-info {
  text-align: center;
  margin: 40px 0;
}

.oauth-app-icon {
  width: 80px;
  height: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--line);
  border: 1px solid var(--dim);
  color: var(--fg);
  margin: 0 auto 16px;
}

.oauth-app-icon svg {
  width: 48px;
  height: 48px;
}

.oauth-app-name {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-lg);
  color: var(--fg);
  margin-bottom: 8px;
}

.oauth-app-desc {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
  line-height: 1.8;
}

/* ---- 当前用户（迁移自 oauth.css .oauth-user-* 系列，值不变） ---- */
.oauth-user-info {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-bottom: 32px;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
}

.oauth-user-avatar {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--line);
  border: 1px solid var(--dim);
  font-family: var(--font-display);
  font-size: 20px;
  font-weight: 700;
  color: var(--fg);
}

.oauth-user-name {
  color: var(--fg);
  font-weight: 400;
}

/* ---- 权限列表（迁移自 oauth.css .oauth-scope-* 系列，值不变） ---- */
.oauth-scopes {
  margin-bottom: 32px;
}

.oauth-scopes-title {
  font-size: var(--text-sm);
  letter-spacing: 0.12em;
  color: var(--mid);
  text-transform: uppercase;
  margin-bottom: 16px;
}

.oauth-scope-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.oauth-scope-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
}

.oauth-scope-item:first-child {
  padding-top: 0;
}

.oauth-scope-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.oauth-scope-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--mid);
  flex-shrink: 0;
}

.oauth-scope-icon svg {
  width: 24px;
  height: 24px;
}

.oauth-scope-text {
  flex: 1;
}

.oauth-scope-name {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--fg);
  margin-bottom: 4px;
}

.oauth-scope-desc {
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.6;
}

/* ---- 按钮组（迁移自 general.css .button-group，值不变） ---- */
.button-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* ---- 提示文本（迁移自 oauth.css .oauth-notice，值不变） ---- */
.oauth-notice {
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--mid);
  text-align: center;
  line-height: 1.6;
  margin-top: 24px;
}

/* ---- 错误态文案（迁移自 verify.css .error-text，值不变） ---- */
.oauth-error-text {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
</style>
