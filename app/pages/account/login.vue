<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n, useRoute, useRouter, useRuntimeConfig, useSeoMeta } from '#imports'
import { parseSocialCallback, socialErrorKey } from '~/composables/useSignIn/social'
import { useSignIn } from '~/composables/useSignIn'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const apiBase = useRuntimeConfig().public.apiBase
const {
  backToCredentials,
  code,
  codeError,
  formError,
  identifier,
  identifierError,
  password,
  passwordError,
  pending,
  signedInUser,
  startTotpChallenge,
  step,
  submitCode,
  submitCredentials,
} = useSignIn()

// 回调参数只在进入页面时读一次：随后从地址栏移除，避免挑战令牌留在历史记录里
const socialSignedIn = ref(false)
const socialErrorSlug = ref('')
const callback = parseSocialCallback(route.query)
if (callback?.kind === 'ok') socialSignedIn.value = true
else if (callback?.kind === 'error') socialErrorSlug.value = callback.slug
else if (callback?.kind === 'totp') startTotpChallenge(callback.challengeToken)

const socialNotice = computed(() => (socialSignedIn.value ? t('login.socialSignedIn') : ''))
const socialError = computed(() =>
  socialErrorSlug.value ? t(socialErrorKey(socialErrorSlug.value)) : '',
)

onMounted(() => {
  if (!('social' in route.query || 'social_error' in route.query || 'challenge_token' in route.query)) {
    return
  }
  // 只移除回调参数，保留后续接线要用的 redirect 等查询项
  const query = { ...route.query }
  delete query.social
  delete query.social_error
  delete query.challenge_token
  void router.replace({ query })
})
// 标签页标题与页面主标题同源，不另立一份文案
useSeoMeta({ title: () => t('login.title') })
</script>

<template>
  <main>
    <AuthPanel v-if="signedInUser" :title="t('login.signedInTitle')" :subtitle="t('login.signedInHint', { username: signedInUser.username })">
      <BaseButton to="/">{{ t('login.backHome') }}</BaseButton>
    </AuthPanel>
    <AuthPanel
      v-else-if="step === 'credentials'"
      num="01"
      :title="t('login.title')"
      :subtitle="t('login.subtitle')"
    >
      <p v-if="socialNotice" class="notice" role="status">{{ socialNotice }}</p>
      <p v-if="socialError" class="form-error" role="alert">{{ socialError }}</p>
      <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
      <form class="stack" novalidate @submit.prevent="submitCredentials">
        <BaseField
          v-model="identifier"
          :label="t('login.identifier')"
          name="identifier"
          type="text"
          autocomplete="username"
          :spellcheck="false"
          :error="identifierError"
          required
        />
        <BaseField
          v-model="password"
          :label="t('login.password')"
          name="password"
          type="password"
          autocomplete="current-password"
          :error="passwordError"
          required
        />
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('login.submit') }}
        </BaseButton>
      </form>
      <p class="divider">{{ t('login.providers') }}</p>
      <div class="stack">
        <!-- 标识位需替换为官方下载素材：Google 四色 G 只能原色置于白底，Microsoft logo 不得改动 -->
        <BaseButton variant="secondary" :href="`${apiBase}/api/auth/google`">
          {{ t('login.signinGoogle') }}
        </BaseButton>
        <BaseButton variant="secondary" :href="`${apiBase}/api/auth/microsoft`">
          {{ t('login.signinMicrosoft') }}
        </BaseButton>
      </div>

      <div class="foot">
        <NuxtLink class="quiet" to="/account/forgot">{{ t('login.forgotPassword') }}</NuxtLink>
        <span class="quiet sep" aria-hidden="true">|</span>
        <NuxtLink class="quiet" to="/account/register">{{ t('login.register') }}</NuxtLink>
      </div>
    </AuthPanel>
    <AuthPanel
      v-else
      num="02"
      :title="t('totp.title')"
      :subtitle="t('totp.hint')">
      <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>

      <form class="stack" novalidate @submit.prevent="submitCode">
        <BaseField
          v-model="code"
          :label="t('totp.code')"
          name="code"
          autocomplete="one-time-code"
          :spellcheck="false"
          autocorrect="off"
          autocapitalize="characters"
          :maxlength="10"
          :error="codeError"
          required
        />
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('totp.submit') }}
        </BaseButton>
      </form>

      <div class="foot">
        <button class="quiet" type="button" @click="backToCredentials">
          {{ t('totp.back') }}
        </button>
      </div>
    </AuthPanel>
  </main>
</template>

<style scoped>
.stack {
  display: grid;
  gap: var(--space-4);
}
.notice,
.form-error {
  margin-bottom: var(--space-4);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
}
.notice {
  color: var(--success);
}
.form-error {
  color: var(--error);
}
.quiet {
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
  background: none;
  border: 0;
  padding: 0;
  cursor: pointer;
}
.quiet:hover {
  color: var(--fg);
}
.sep {
  color: var(--fg-faint);
}
.divider {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-block: var(--space-6);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-faint);
}
.divider::before,
.divider::after {
  content: '';
  flex: 1;
  border-top: 1px solid var(--line);
}
.foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-8);
  margin-top: var(--space-6);
}
</style>
