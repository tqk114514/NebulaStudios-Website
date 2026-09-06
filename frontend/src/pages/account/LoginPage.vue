<script setup lang="ts">
// 登录页（对齐原站 modules/account/pages/login.html 结构 + assets/js/login.ts 逻辑）。
// 样式自持：骨架/序号/标题来自 AuthCard，输入来自 FormField，按钮来自 AppButton，
// 页面专属样式（OAuth 分隔线、OAuth 按钮组、底部链接、弹窗文案）在本页 scoped 中。
//
// 流程：加载验证码配置 → 表单校验 → 若启用则取 Turnstile token → POST /login → 政策同意 → 跳转。

import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'
import AuthCard from '@/components/AuthCard.vue'
import FormField from '@/components/FormField.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'
import CaptchaWidget from '@/components/CaptchaWidget.vue'
import { RouterLink } from 'vue-router'
import { errorKey } from '@/api/errorCodes'
import { login, loginTotp } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { usePolicyConsent } from '@/composables/usePolicyConsent'
import { loadCaptchaConfig, getCaptchaToken, isCaptchaEnabled } from '@/composables/useCaptcha'
import { CDN_URL } from '@/config/cdn'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { check: checkConsent } = usePolicyConsent()

const email = ref('')
const password = ref('')
const loading = ref(false)
const showAlert = ref(false)
const alertMessage = ref('')
const captchaReady = ref(false)

// TOTP 二步验证状态：密码通过后切换出验证码输入形态
const totpStep = ref(false)
const pendingToken = ref('')
const totpCode = ref('')

const returnUrl = (route.query.return as string) || ''

/** 校验登录表单：与旧站 validateLoginForm 一致，仅要求非空 */
function validate(): string | null {
  if (!email.value.trim() || !password.value) return 'account.login.fillAllFields'
  return null
}

function alert(key: string) {
  alertMessage.value = key
  showAlert.value = true
}

/** 处理 return 参数（仅同源），否则跳 dashboard */
function redirectAfterLogin() {
  if (returnUrl) {
    try {
      const url = new URL(decodeURIComponent(returnUrl), window.location.origin)
      if (url.origin === window.location.origin) {
        window.location.href = decodeURIComponent(returnUrl)
        return
      }
    } catch {
      // 解析失败，回退默认跳转
    }
  }
  router.replace('/account/dashboard')
}

function reactToError(e: unknown) {
  alert(errorKey(e))
}

/** 登录成功后的公共收尾：刷新会话态 → 政策同意 → 跳转 */
async function finishLogin() {
  // 强制重新探测：同一次页面会话内可能已有过"未登录"的探测缓存
  // （如登出、会话过期被守卫踢回），登录成功后该结论已失效
  await auth.bootstrap({ force: true })
  // 政策同意：拒绝则已登出并跳转
  const consented = await checkConsent()
  if (!consented) return
  redirectAfterLogin()
}

async function handleSubmit() {
  const err = validate()
  if (err) {
    alert(err)
    return
  }

  // 启用验证码时必须先拿到 token
  if (captchaReady.value && isCaptchaEnabled() && !getCaptchaToken()) {
    alert('error.humanVerifyFailed')
    return
  }

  loading.value = true
  try {
    const res = await login({
      email: email.value.trim(),
      password: password.value,
      captchaToken: getCaptchaToken(),
    })

    // 已启用两步验证：切换到 TOTP 输入形态（中转 token 在验证成功前保持有效）
    if ('totp_required' in res && res.totp_required) {
      pendingToken.value = res.pending_token
      totpCode.value = ''
      totpStep.value = true
      return
    }

    await finishLogin()
  } catch (e) {
    reactToError(e)
  } finally {
    loading.value = false
  }
}

async function handleTotpSubmit() {
  const code = totpCode.value.trim()
  if (!code) {
    alert('account.totp.enterCode')
    return
  }

  loading.value = true
  try {
    await loginTotp({ pendingToken: pendingToken.value, code })
    await finishLogin()
  } catch (e) {
    reactToError(e)
  } finally {
    loading.value = false
  }
}

// 初始化：加载验证码配置
onMounted(async () => {
  await loadCaptchaConfig()
  captchaReady.value = true

  // 处理 OAuth 回调错误参数
  const oauthErr = route.query.error as string | undefined
  if (oauthErr) {
    alert(oauthErr === 'no_linked_account' ? 'account.login.noLinkedAccount' : 'account.login.oauthError')
    // 清除 URL 中的错误参数
    window.history.replaceState({}, '', route.path + (returnUrl ? `?return=${encodeURIComponent(returnUrl)}` : ''))
  }
})
</script>

<template>
  <AuthCard
    num="01"
    :title="$t(totpStep ? 'account.totp.loginTitle' : 'account.login.title')"
    :subtitle="$t(totpStep ? 'account.totp.loginSubtitle' : 'account.login.subtitle')"
  >
    <form v-if="!totpStep" novalidate @submit.prevent="handleSubmit">
      <FormField :label="$t('account.login.emailPlaceholder')">
        <input
          v-model="email"
          type="text"
          name="email"
          autocomplete="username"
          :placeholder="$t('account.login.emailPlaceholder')"
          aria-label="email"
          required
        />
      </FormField>

      <FormField :label="$t('account.login.passwordPlaceholder')">
        <input
          v-model="password"
          type="password"
          name="password"
          autocomplete="current-password"
          :placeholder="$t('account.login.passwordPlaceholder')"
          aria-label="password"
          required
        />
      </FormField>

      <AppButton type="submit" :disabled="loading">
        {{ loading ? $t('account.login.loggingIn') : $t('account.login.submitButton') }}
      </AppButton>

      <!-- 人机验证（系统启用时显示；未启用时不占用空间） -->
      <CaptchaWidget />
    </form>

    <!-- TOTP 二步验证形态 -->
    <form v-else novalidate @submit.prevent="handleTotpSubmit">
      <FormField :label="$t('account.totp.loginCodeLabel')">
        <input
          v-model="totpCode"
          type="text"
          name="totp"
          autocomplete="one-time-code"
          inputmode="numeric"
          maxlength="8"
          :placeholder="$t('account.totp.loginCodePlaceholder')"
          aria-label="totp-code"
          autofocus
          required
        />
      </FormField>

      <AppButton type="submit" :disabled="loading">
        {{ loading ? $t('account.login.loggingIn') : $t('account.totp.loginSubmit') }}
      </AppButton>

      <button type="button" class="totp-back" :disabled="loading" @click="totpStep = false">
        {{ $t('account.totp.backToLogin') }}
      </button>
    </form>

    <div class="oauth-divider">
      <span>{{ $t('account.login.orContinueWith') }}</span>
    </div>

    <div class="oauth-buttons">
      <a :href="`/api/auth/microsoft${returnUrl ? '?return=' + encodeURIComponent(returnUrl) : ''}`" class="oauth-btn">
        <img :src="CDN_URL + '/images/logo/microsoft/Symbol.svg'" alt="Microsoft" width="20" height="20" />
        <span>{{ $t('account.login.microsoftLogin') }}</span>
      </a>
      <a :href="`/api/auth/google${returnUrl ? '?return=' + encodeURIComponent(returnUrl) : ''}`" class="oauth-btn">
        <img :src="CDN_URL + '/images/logo/google/Symbol.svg'" alt="Google" width="20" height="20" />
        <span>{{ $t('account.login.googleLogin') }}</span>
      </a>
    </div>

    <div class="footer-links">
      <RouterLink :to="`/account/forgot${returnUrl ? '?return=' + encodeURIComponent(returnUrl) : ''}`">
        {{ $t('account.login.forgotPassword') }}
      </RouterLink>
      <span>|</span>
      <RouterLink :to="`/account/register${returnUrl ? '?return=' + encodeURIComponent(returnUrl) : ''}`">
        {{ $t('account.login.createAccount') }}
      </RouterLink>
    </div>
  </AuthCard>

  <PolicyFooter />

  <!-- 提示弹窗 -->
  <AppModal v-model:open="showAlert" :title="$t('modal.alert')">
    <p class="modal-message">{{ $t(alertMessage) }}</p>
    <template #footer>
      <AppButton :arrow="false" @click="showAlert = false">{{ $t('modal.close') }}</AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
/* ---- OAuth 分隔线（迁移自 login.css .oauth-divider，值不变） ---- */
.oauth-divider {
  display: flex;
  align-items: center;
  margin: 32px 0 24px;
  color: var(--dim);
  font-size: var(--text-sm);
  letter-spacing: 0.15em;
  text-transform: uppercase;
}

.oauth-divider::before,
.oauth-divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--line);
}

.oauth-divider span {
  padding: 0 16px;
}

/* ---- OAuth 按钮组（迁移自 login.css .oauth-buttons） ---- */
.oauth-buttons {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 锚点版 secondary 按钮（值来自 general.css .button-secondary） */
.oauth-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  width: 100%;
  height: 48px;
  padding: 0 20px;
  background: transparent;
  border: 1px solid var(--dim);
  color: var(--fg);
  font-family: var(--font-mono);
  font-weight: 400;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  cursor: pointer;
  transition: border-color 0.2s, background 0.2s;
}

.oauth-btn:hover {
  border-color: var(--mid);
  background: var(--dim);
}

/* ---- 卡片内底部链接（迁移自 common.css .footer-links + login.css .card .footer-links，
        flex 等间距替代旧的绝对定位，视觉等价） ---- */
.footer-links {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 32px;
  margin-top: 24px;
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
}

.footer-links a {
  color: var(--mid);
  transition: color 0.2s;
}

.footer-links a:hover {
  color: var(--fg);
}

.footer-links span {
  color: var(--dim);
}

/* ---- TOTP 二步返回登录链接 ---- */
.totp-back {
  width: 100%;
  margin-top: 4px;
  padding: 8px 0;
  background: transparent;
  border: none;
  color: var(--mid);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.14em;
  cursor: pointer;
  transition: color 0.2s;
}

.totp-back:hover {
  color: var(--fg);
}

/* ---- 提示弹窗文案（迁移自 common.css .modal-message） ---- */
.modal-message {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
</style>
