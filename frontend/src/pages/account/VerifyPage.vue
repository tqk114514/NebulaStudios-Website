<script setup lang="ts">
// 验证码页（对齐原站 verify.html + verify.ts，SPA 化）。
// 样式自持：骨架来自 AuthCard（标题仅在成功态传入），返回按钮来自 AppButton，
// 页面专属样式（验证码格子、过期提示、错误态、加载 spinner）在本页 scoped 中。
// token 通过 URL query 传递（?token=xxx，vue-router route.query 原生支持）→
// POST /api/auth/verify-email → 显示 6 位验证码（可点击复制）或错误态。
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AuthCard from '@/components/AuthCard.vue'
import AppButton from '@/components/AppButton.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'
import { post } from '@/api/client'

const route = useRoute()
const router = useRouter()

const VERIFY_KEYS: Record<string, string> = {
  INVALID_TOKEN: 'account.verify.errorInvalidToken',
  TOKEN_EXPIRED: 'account.verify.errorTokenExpired',
  TOKEN_USED: 'account.verify.errorTokenUsed',
  NO_TOKEN: 'account.verify.errorNoToken',
  NETWORK_ERROR: 'account.verify.errorNetwork',
  SERVER_ERROR: 'account.verify.errorDefault',
  VERIFY_FAILED: 'account.verify.errorDefault',
}

interface VerifyBody {
  code: string
  email: string
}

const state = ref<'loading' | 'success' | 'error'>('loading')
const code = ref('')
const errorKey = ref('')

const codeChars = computed(() => Array.from({ length: 6 }, (_, i) => code.value[i] ?? '-'))

/** 从路由 query 读取 token（邮件链接 ?token=xxx）。 */
function getQueryToken(): string {
  const t = route.query.token
  return typeof t === 'string' ? t : ''
}

async function copyCode() {
  if (!code.value) return
  try {
    await navigator.clipboard.writeText(code.value)
  } catch {
    // 降级：execCommand
    const ta = document.createElement('textarea')
    ta.value = code.value
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    try {
      document.execCommand('copy')
    } catch {
      // 忽略复制失败
    }
    document.body.removeChild(ta)
  }
}

async function loadVerificationCode() {
  const token = getQueryToken()
  // 取到后清除 query，避免刷新重复提交
  if (token) {
    router.replace({ path: route.path })
  }
  if (!token) {
    errorKey.value = 'account.verify.errorNoToken'
    state.value = 'error'
    return
  }
  try {
    const res = await post<VerifyBody>('/api/auth/verify-email', { token })
    code.value = res.code
    if (res.email) {
      sessionStorage.setItem('verify_email', res.email)
    }
    state.value = 'success'
  } catch (e) {
    const errCode = (e as { errorCode?: string }).errorCode || 'VERIFY_FAILED'
    errorKey.value = VERIFY_KEYS[errCode] ?? 'account.verify.errorDefault'
    state.value = 'error'
  }
}

onMounted(() => {
  loadVerificationCode()
})

function closeThisPage() {
  window.close()
}
</script>

<template>
  <AuthCard
    num="04"
    :title="state === 'success' ? $t('account.verify.title') : ''"
    :subtitle="state === 'success' ? $t('account.verify.subtitle') : ''"
  >
    <!-- Loading -->
    <div v-if="state === 'loading'" class="loading-state">
      <div class="loader-spinner"></div>
    </div>

    <!-- Success State -->
    <div v-else-if="state === 'success'" class="verify-state">
      <div class="code-display">
        <div
          class="code-boxes"
          role="button"
          tabindex="0"
          aria-label="verification code"
          @click="copyCode"
          @keydown.enter.prevent="copyCode"
          @keydown.space.prevent="copyCode"
        >
          <span v-for="(ch, i) in codeChars" :key="i" class="code-box">{{ ch }}</span>
        </div>
        <div class="code-hint" role="button" tabindex="0" @click="copyCode">{{ $t('account.verify.copyHint') }}</div>
      </div>

      <p class="expire-text">{{ $t('account.verify.expireNotice') }}</p>

      <AppButton variant="secondary" @click="closeThisPage">
        {{ $t('account.verify.backButton') }}
      </AppButton>
    </div>

    <!-- Error State -->
    <div v-else class="verify-state">
      <div class="error-container">
        <div class="error-title">{{ $t('account.verify.errorTitle') }}</div>
        <p class="error-text">{{ $t(errorKey) }}</p>
      </div>
      <AppButton variant="secondary" @click="closeThisPage">
        {{ $t('account.verify.backButton') }}
      </AppButton>
    </div>
  </AuthCard>

  <PolicyFooter />
</template>

<style scoped>
/* ---- 加载状态（迁移自 verify.css #loading-state + .loader-spinner，值不变） ---- */
.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 24px;
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

/* ---- 验证码显示（迁移自 verify.css .code-display 系列，值不变） ---- */
.code-display {
  margin: 32px 0;
  text-align: center;
}

.code-boxes {
  display: flex;
  justify-content: center;
  gap: 8px;
  margin-bottom: 16px;
  cursor: pointer;
}

.code-box {
  width: 48px;
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--line);
  border: 1px solid var(--dim);
  font-family: var(--font-mono);
  font-size: var(--text-lg);
  font-weight: 300;
  color: var(--fg);
  transition: border-color 0.2s, background 0.2s;
}

.code-box:hover {
  border-color: var(--mid);
  background: var(--dim);
}

.code-hint {
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
  color: var(--mid);
  cursor: pointer;
  transition: color 0.2s;
  display: inline-block;
}

.code-hint:focus-visible {
  color: var(--fg);
  text-decoration: underline;
  outline: none;
}

.code-hint:hover {
  color: var(--fg);
}

/* ---- 过期提示（迁移自 verify.css .expire-text，值不变） ---- */
.expire-text {
  margin: 24px 0;
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
  line-height: 1.8;
  text-align: center;
}

/* ---- 错误状态（迁移自 verify.css .error-container 系列，值不变） ---- */
.error-container {
  text-align: center;
  margin-bottom: 32px;
}

.error-title {
  font-family: var(--font-display);
  font-weight: 700;
  font-size: clamp(24px, 3vw, 32px);
  letter-spacing: -0.02em;
  color: var(--error);
  margin-bottom: 16px;
}

.error-text {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
</style>
