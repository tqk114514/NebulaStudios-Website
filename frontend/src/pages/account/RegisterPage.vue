<script setup lang="ts">
// 注册页（对齐原站 modules/account/pages/register.html + register.ts）。
// 样式自持：骨架来自 AuthCard，输入来自 FormField，按钮来自 AppButton，
// 页面专属样式（密码强度、政策协议、错误链接、底部链接）在本页 scoped 中。
// 流程：白名单校验 → captcha → 发送验证码 → 倒计时 → 提交注册。
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

const form = reactive({
  username: '',
  email: '',
  verificationCode: '',
  password: '',
  passwordConfirm: '',
})

const usernameError = ref('')
const emailError = ref('')

// 邮箱合法（格式完整 + 白名单内）才可发送验证码（对齐旧前端 sendBtn.disabled = !validation.valid）
const emailSendable = computed(() => validateEmail(form.email.trim()).valid)
const submitting = ref(false)
const sendingCode = ref(false)
const showAlert = ref(false)
const alertMessage = ref('')
const showSupportedEmails = ref(false)
const emailLoaded = ref(false)

const countdown = useCountdown(60)

// ---- 密码强度 ----
const passReq = reactive({ length: false, number: false, special: false, ccase: false })

const SPECIAL_RE = /[!@#$%^&*(),.?":{}|<>]/
function updatePassReq() {
  const p = form.password
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

// ---- 发送验证码（人机验证启用时需先通过验证拿到 token）----
async function handleSendCode() {
  const email = form.email.trim()
  const v = validateEmail(email)
  if (!v.valid) {
    alert(v.errorKey)
    return
  }
  if (sendingCode.value || countdown.running.value) return
  if (isCaptchaEnabled() && !getCaptchaToken()) {
    alert('account.login.humanVerifyFailed')
    return
  }

  sendingCode.value = true
  try {
    await post('/api/auth/send-code', {
      email,
      captchaToken: getCaptchaToken(),
      language: 'zh-CN',
    })
    countdown.start('register', email)
    resetCaptchaToken() // token 一次性，发送后清除避免复用
    alert('account.register.codeSent')
  } catch (e) {
    // 限流命中：以服务端返回的限制结束时间戳启动倒计时（后端按邮箱 60s 限流，本地计时可能不准）
    if (e instanceof ApiClientError && e.errorCode === 'RATE_LIMIT' && e.retryAt) {
      countdown.start('register', email, Math.ceil(e.retryAt - Date.now() / 1000))
    }
    alert(errorKey(e))
  } finally {
    sendingCode.value = false
  }
}

// 邮箱输入：与旧前端一致——格式未完整（缺 @ / .）时不即时提示，
// 仅在格式完整但域名不在白名单时显示"邮箱不受支持"（提交时再走完整校验拦截）
function onEmailInput() {
  const email = form.email.trim()
  if (email === '') {
    emailError.value = ''
    return
  }
  const v = validateEmail(email)
  emailError.value = v.errorKey === 'account.register.emailNotSupported' ? v.errorKey : ''
}

// 用户名：长度校验
function onUsernameInput() {
  const u = form.username.trim()
  if (u.length > 15) {
    usernameError.value = 'account.register.usernameTooLong'
  } else {
    usernameError.value = ''
  }
}

// 验证码输入过滤：仅字母数字
function onCodeInput() {
  form.verificationCode = form.verificationCode.replace(/[^0-9a-zA-Z]/g, '').slice(0, 6)
}

// ---- 提交注册 ----
function validateForm(): string | null {
  if (!form.username.trim() || !form.email.trim() || !form.verificationCode.trim() ||
      !form.password || !form.passwordConfirm) {
    return 'account.register.fillAllFields'
  }
  if (form.password !== form.passwordConfirm) return 'account.register.passwordMismatch'
  if (!passReq.length || !passReq.number || !passReq.special || !passReq.ccase) return 'account.register.passwordInvalid'
  const ev = validateEmail(form.email.trim())
  if (!ev.valid) return ev.errorKey
  return null
}

async function handleSubmit() {
  const err = validateForm()
  if (err) {
    alert(err)
    return
  }
  submitting.value = true
  try {
    await post<{ message: string }>('/api/auth/register', {
      username: form.username.trim(),
      email: form.email.trim(),
      verificationCode: form.verificationCode.trim(),
      password: form.password,
    })
    alert('account.register.success')
    setTimeout(goToLogin, 2000)
  } catch (e) {
    alert(errorKey(e))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  emailLoaded.value = await loadEmailWhitelist()
  await loadCaptchaConfig()
  countdown.restoreOnMount('register', form.email)
})
</script>

<template>
  <AuthCard num="02" :title="$t('account.register.title')" :subtitle="$t('account.register.subtitle')">
    <form novalidate @submit.prevent="handleSubmit">
      <FormField :label="$t('account.register.usernamePlaceholder')" :error="usernameError ? $t(usernameError) : ''">
        <input
          v-model="form.username"
          type="text"
          name="username"
          autocomplete="username"
          :placeholder="$t('account.register.usernamePlaceholder')"
          required
          @input="onUsernameInput"
        />
      </FormField>

      <!-- 邮箱错误内嵌"查看支持的邮箱"链接，故错误区域在插槽内自渲染 -->
      <FormField :label="$t('account.register.emailPlaceholder')" :class="{ 'field-invalid': !!emailError }">
        <input
          v-model="form.email"
          type="email"
          name="email"
          autocomplete="email"
          :placeholder="$t('account.register.emailPlaceholder')"
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

      <FormField class="verification-input-group">
        <input
          v-model="form.verificationCode"
          type="text"
          name="verificationCode"
          autocomplete="one-time-code"
          maxlength="6"
          :placeholder="$t('account.register.verificationCodePlaceholder')"
          required
          @input="onCodeInput"
        />
      </FormField>

      <FormField>
        <AppButton
          variant="secondary"
          :disabled="sendingCode || countdown.running.value || !emailSendable"
          @click="handleSendCode"
        >
          <template v-if="countdown.running.value">{{ countdown.remaining }}s</template>
          <template v-else>{{ $t('account.register.sendCodeButton') }}</template>
        </AppButton>
        <CaptchaWidget />
      </FormField>

      <FormField :label="$t('account.register.passwordPlaceholder')">
        <input
          v-model="form.password"
          type="password"
          name="password"
          autocomplete="new-password"
          :placeholder="$t('account.register.passwordPlaceholder')"
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

      <FormField :label="$t('account.register.confirmPasswordPlaceholder')">
        <input
          v-model="form.passwordConfirm"
          type="password"
          name="passwordConfirm"
          autocomplete="new-password"
          :placeholder="$t('account.register.confirmPasswordPlaceholder')"
          required
        />
      </FormField>

      <AppButton type="submit" :disabled="submitting">
        {{ submitting ? $t('account.register.registering') : $t('account.register.submitButton') }}
      </AppButton>

      <p v-if="emailLoaded" class="policy-agree">
        {{ $t('policy.agreeByRegister') }}
        <a href="/policy#privacy" target="_blank">{{ $t('policy.privacyPolicy') }}</a>
        {{ $t('policy.and') }}
        <a href="/policy#terms" target="_blank">{{ $t('policy.termsOfService') }}</a>
      </p>
    </form>

    <div class="footer-links">
      <RouterLink :to="loginRoute">{{ $t('account.register.backToLogin') }}</RouterLink>
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

/* ---- 政策协议声明（迁移自 register.css .policy-agree，值不变） ---- */
.policy-agree {
  margin-top: 20px;
  font-size: var(--text-base);
  letter-spacing: 0.1em;
  color: var(--mid);
  line-height: 1.8;
  text-align: center;
}

.policy-agree a {
  color: var(--fg);
  border-bottom: 1px solid transparent;
  transition: border-color 0.2s;
}

.policy-agree a:hover {
  border-color: var(--fg);
}

.policy-agree a:focus-visible {
  border-color: var(--fg);
  outline: none;
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
