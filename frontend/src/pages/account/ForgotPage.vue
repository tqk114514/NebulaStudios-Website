<script setup lang="ts">
// 忘记密码页（对齐原站 modules/account/pages/forgot.html + forgot.ts）。
// 样式自持：骨架来自 AuthCard，输入来自 FormField，按钮来自 AppButton，
// 页面专属样式（密码强度、错误链接、底部链接）在本页 scoped 中。
// 两步：1) 发重置验证码（校验邮箱+白名单+captcha）→ 2) 验证码+新密码+确认。
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'
import AuthCard from '@/components/AuthCard.vue'
import FormField from '@/components/FormField.vue'
import PolicyFooter from '@/components/PolicyFooter.vue'
import CaptchaWidget from '@/components/CaptchaWidget.vue'
import SupportedEmailsModal from '@/components/SupportedEmailsModal.vue'
import { post, ApiClientError } from '@/api/client'
import { errorKey } from '@/api/errorCodes'
import { loadEmailWhitelist, validateEmail } from '@/composables/useEmailWhitelist'
import { loadCaptchaConfig, getCaptchaToken, isCaptchaEnabled, resetCaptchaToken } from '@/composables/useCaptcha'
import { useCountdown } from '@/composables/useCountdown'

const route = useRoute()
const router = useRouter()
const returnUrl = (route.query.return as string) || ''

// 登录页目标（保留 return 参数）
const loginRoute = computed(() =>
  returnUrl ? { path: '/account/login', query: { return: returnUrl } } : { path: '/account/login' },
)

const step = ref<'email' | 'reset'>('email')
const email = ref('')
const code = ref('')
const password = ref('')
const passwordConfirm = ref('')

const emailError = ref('')

// 邮箱合法（格式完整 + 白名单内）才可发送重置验证码（对齐旧前端）
const emailSendable = computed(() => validateEmail(email.value.trim()).valid)
const sendingCode = ref(false)
const resetting = ref(false)
const countdown = useCountdown(60)
const showAlert = ref(false)
const alertMessage = ref('')
const showSupportedEmails = ref(false)

// 密码强度
const passReq = reactive({ length: false, number: false, special: false, ccase: false })
const SPECIAL_RE = /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?`~]/
function updatePassReq() {
  const p = password.value
  passReq.length = p.length >= 16 && p.length <= 64
  passReq.number = /\d/.test(p)
  passReq.special = SPECIAL_RE.test(p)
  passReq.ccase = /[A-Z]/.test(p) && /[a-z]/.test(p)
}

function alert(key: string) {
  alertMessage.value = key
  showAlert.value = true
}

function goToLogin() {
  router.push(loginRoute.value)
}

// 邮箱输入：与旧前端一致——格式未完整（缺 @ / .）时不即时提示，
// 仅在格式完整但域名不在白名单时显示"邮箱不受支持"（提交时再走完整校验拦截）
function onEmailInput() {
  const e = email.value.trim()
  if (e === '') {
    emailError.value = ''
    return
  }
  const v = validateEmail(e)
  emailError.value = v.errorKey === 'account.register.emailNotSupported' ? v.errorKey : ''
}

// 发送重置验证码
async function handleSendCode() {
  const e = email.value.trim().toLowerCase()
  const v = validateEmail(e)
  if (!v.valid) {
    alert(v.errorKey)
    return
  }
  if (countdown.running.value) return
  if (isCaptchaEnabled() && !getCaptchaToken()) {
    alert('account.login.humanVerifyFailed')
    return
  }
  sendingCode.value = true
  try {
    await post('/api/auth/send-reset-code', {
      email: e,
      captchaToken: getCaptchaToken(),
      language: document.documentElement.lang || 'zh-CN',
    })
    resetCaptchaToken()
    countdown.start('forgot', e)
    alert('account.forgotPassword.codeSent')
    step.value = 'reset'
  } catch (er) {
    // 限流命中：以服务端返回的限制结束时间戳启动倒计时（后端按邮箱 60s 限流）
    if (er instanceof ApiClientError && er.errorCode === 'RATE_LIMIT' && er.retryAt) {
      countdown.start('forgot', e, Math.ceil(er.retryAt - Date.now() / 1000))
    }
    alert(errorKey(er))
  } finally {
    sendingCode.value = false
  }
}

// 重置密码
async function handleReset() {
  if (!code.value.trim()) {
    alert('account.forgotPassword.codeRequired')
    return
  }
  const pv = password.value
  if (!passReq.length || !passReq.number || !passReq.special || !passReq.ccase) {
    alert('account.register.passwordInvalid')
    return
  }
  if (pv !== passwordConfirm.value) {
    alert('account.register.passwordMismatch')
    return
  }
  resetting.value = true
  try {
    await post<{ message: string }>('/api/auth/reset-password', {
      email: email.value.trim().toLowerCase(),
      code: code.value.trim(),
      password: pv,
    })
    alert('account.forgotPassword.resetSuccess')
    setTimeout(goToLogin, 1500)
  } catch (er) {
    alert(errorKey(er))
    resetting.value = false
  }
}

onMounted(async () => {
  await loadEmailWhitelist()
  await loadCaptchaConfig()
  countdown.restoreOnMount('forgot', email.value)
})
</script>

<template>
  <AuthCard num="03" :title="$t('account.forgotPassword.title')" :subtitle="$t('account.forgotPassword.subtitle')">
    <!-- 步骤 1：输入邮箱 -->
    <form v-if="step === 'email'" id="email-step" novalidate @submit.prevent="handleSendCode">
      <!-- 邮箱错误内嵌"查看支持的邮箱"链接，故错误区域在插槽内自渲染 -->
      <FormField :label="$t('account.forgotPassword.emailPlaceholder')" :class="{ 'field-invalid': !!emailError }">
        <input
          v-model="email"
          type="email"
          name="email"
          autocomplete="email"
          :placeholder="$t('account.forgotPassword.emailPlaceholder')"
          required
          @input="onEmailInput"
        />
        <p v-if="emailError" class="form-error">
          {{ $t(emailError) }}
          <button type="button" class="error-link" @click="showSupportedEmails = true">
            {{ $t('account.register.viewSupportedEmails') }}
          </button>
        </p>
      </FormField>

      <FormField>
        <AppButton type="submit" variant="secondary" :disabled="sendingCode || countdown.running.value || !emailSendable">
          <template v-if="countdown.running.value">{{ countdown.remaining }}s</template>
          <template v-else>{{ $t('account.forgotPassword.sendCode') }}</template>
        </AppButton>
        <CaptchaWidget />
      </FormField>
    </form>

    <!-- 步骤 2：验证码 + 新密码 -->
    <form v-else id="reset-step" novalidate @submit.prevent="handleReset">
      <FormField :label="$t('account.forgotPassword.codePlaceholder')">
        <input
          v-model="code"
          type="text"
          name="code"
          autocomplete="one-time-code"
          maxlength="6"
          :placeholder="$t('account.forgotPassword.codePlaceholder')"
          required
          @input="code = code.replace(/[^0-9a-zA-Z]/g, '')"
        />
      </FormField>

      <FormField :label="$t('account.forgotPassword.newPasswordPlaceholder')">
        <input
          v-model="password"
          type="password"
          name="password"
          autocomplete="new-password"
          :placeholder="$t('account.forgotPassword.newPasswordPlaceholder')"
          required
          @input="updatePassReq"
        />
        <div class="password-requirements">
          <span class="req-item" :class="{ 'is-valid': passReq.length }">{{ $t('account.register.reqLength') }}</span>
          <span class="req-item" :class="{ 'is-valid': passReq.number }">{{ $t('account.register.reqNumber') }}</span>
          <span class="req-item" :class="{ 'is-valid': passReq.special }">{{ $t('account.register.reqSpecial') }}</span>
          <span class="req-item" :class="{ 'is-valid': passReq.ccase }">{{ $t('account.register.reqCase') }}</span>
        </div>
      </FormField>

      <FormField :label="$t('account.forgotPassword.confirmPasswordPlaceholder')">
        <input
          v-model="passwordConfirm"
          type="password"
          name="passwordConfirm"
          autocomplete="new-password"
          :placeholder="$t('account.forgotPassword.confirmPasswordPlaceholder')"
          required
        />
      </FormField>

      <AppButton type="submit" :disabled="resetting">
        {{ resetting ? $t('account.forgotPassword.resetting') : $t('account.forgotPassword.resetPassword') }}
      </AppButton>
    </form>

    <div class="footer-links">
      <RouterLink :to="loginRoute">{{ $t('account.forgotPassword.backToLogin') }}</RouterLink>
    </div>
  </AuthCard>

  <PolicyFooter />

  <SupportedEmailsModal v-model:open="showSupportedEmails" />

  <AppModal v-model:open="showAlert" :title="$t('modal.alert')">
    <p class="modal-message">{{ $t(alertMessage) }}</p>
    <template #footer>
      <AppButton :arrow="false" @click="showAlert = false">{{ $t('modal.close') }}</AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
/* ---- 密码强度检测（迁移自 common.css .password-requirements / .req-item，值不变） ---- */
.password-requirements {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 12px;
}

.req-item {
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
  color: var(--mid);
  transition: color 0.2s;
}

.req-item.is-valid {
  color: var(--success);
}

/* ---- 卡片内底部链接（迁移自 common.css .footer-links，值不变） ---- */
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

/* ---- 邮箱错误（带链接，迁移自 common.css .form-error / .error-link，值不变） ---- */
.field-invalid :deep(input) {
  border-color: var(--error);
}

.form-error {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--error);
  margin-top: 8px;
}

.error-link {
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  letter-spacing: inherit;
  color: var(--error-bright);
  border-bottom: 1px solid transparent;
  margin-left: 8px;
  cursor: pointer;
  transition: border-color 0.2s;
}

.error-link:hover {
  border-color: var(--error-bright);
}

/* ---- 提示弹窗文案（迁移自 common.css .modal-message） ---- */
.modal-message {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
</style>
