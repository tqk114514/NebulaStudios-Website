<script setup lang="ts">
import { useI18n, useSeoMeta } from '#imports'
import { useSignUp } from '~/composables/useSignUp'
import { useTurnstile } from '~/composables/useTurnstile'

const { t } = useI18n()
const {
  backToRegister,
  code,
  codeError,
  email,
  emailError,
  formErrorText,
  notice,
  password,
  passwordError,
  pending,
  resend,
  step,
  submit,
  username,
  usernameError,
  verified,
  verifyCode,
} = useSignUp()
const { enabled, reset, siteKey, token, version } = useTurnstile()

async function onSubmit() {
  await submit(token.value)
  reset()
}

async function onResend() {
  await resend(token.value)
  reset()
}

// 标签页标题与页面主标题同源，不另立一份文案
useSeoMeta({ title: () => t('register.title') })
</script>

<template>
  <main>
    <AuthPanel
      v-if="verified"
      :title="t('register.verifiedTitle')"
      :subtitle="t('register.verifiedHint')">
      <BaseButton to="/account/login">{{ t('register.backToLogin') }}</BaseButton>
    </AuthPanel>

    <AuthPanel
      v-else-if="step === 'register'"
      :title="t('register.title')"
      :subtitle="t('register.subtitle')">
      <form class="stack" novalidate @submit.prevent="onSubmit">
        <BaseField
          v-model="email"
          :label="t('register.email')"
          name="email"
          type="email"
          autocomplete="email"
          :spellcheck="false"
          :error="emailError"
          required
        />
        <BaseField
          v-model="username"
          :label="t('register.username')"
          name="username"
          autocomplete="username"
          :spellcheck="false"
          autocorrect="off"
          autocapitalize="none"
          :error="usernameError"
          required
        />
        <BaseField
          v-model="password"
          :label="t('register.password')"
          name="new-password"
          type="password"
          autocomplete="new-password"
          :error="passwordError"
          required
        />
        <p class="hint">{{ t('password.hint') }}</p>
        <p v-if="formErrorText" class="form-error" role="alert">{{ formErrorText }}</p>
        <BaseTurnstile
          v-if="enabled"
          :key="version"
          v-model="token"
          :site-key="siteKey"
        />
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('register.submit') }}
        </BaseButton>
      </form>
      <div class="foot">
        <NuxtLink class="quiet" to="/account/login">{{ t('register.backToLogin') }}</NuxtLink>
      </div>
    </AuthPanel>

    <AuthPanel
      v-else
      :title="t('register.verifyTitle')"
      :subtitle="t('register.verifyHint', { email })">
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <p v-if="formErrorText" class="form-error" role="alert">{{ formErrorText }}</p>
      <form class="stack" novalidate @submit.prevent="verifyCode">
        <BaseField
          v-model="code"
          :label="t('register.code')"
          name="code"
          autocomplete="one-time-code"
          :spellcheck="false"
          autocorrect="off"
          autocapitalize="none"
          :maxlength="6"
          :error="codeError"
          required
        />
        <BaseTurnstile
          v-if="enabled"
          :key="version"
          v-model="token"
          :site-key="siteKey"
        />
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('register.verifySubmit') }}
        </BaseButton>
      </form>
      <div class="foot">
        <button
          class="quiet"
          type="button"
          :disabled="pending"
          @click="onResend">
          {{ t('register.resend') }}
        </button>
        <span class="quiet sep" aria-hidden="true">|</span>
        <button class="quiet" type="button" @click="backToRegister">
          {{ t('register.changeEmail') }}
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
.hint {
  margin-top: calc(var(--space-2) * -1);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--fg-faint);
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
.foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-8);
  margin-top: var(--space-6);
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
a.quiet:hover,
.quiet:hover {
  color: var(--fg);
}
.sep {
  color: var(--fg-faint);
}
</style>
