<script setup lang="ts">
import { useI18n, useSeoMeta } from '#imports'
import { usePasswordReset } from '~/composables/usePasswordReset'
import { useTurnstile } from '~/composables/useTurnstile'

const { t } = useI18n()
const {
  backToEmail,
  code,
  codeError,
  completed,
  email,
  emailError,
  formError,
  password,
  passwordError,
  pending,
  step,
  submitEmail,
  submitReset,
} = usePasswordReset()
const { enabled, reset, siteKey, token, version } = useTurnstile()

async function onSend() {
  await submitEmail(token.value)
  reset()
}

useSeoMeta({ title: () => t('forgot.title') })
</script>

<template>
  <main>
    <AuthPanel
      v-if="completed"
      :title="t('forgot.doneTitle')"
      :subtitle="t('forgot.doneHint')">
      <BaseButton to="/account/login">{{ t('forgot.backToLogin') }}</BaseButton>
    </AuthPanel>

    <AuthPanel
      v-else-if="step === 'email'"
      :title="t('forgot.title')"
      :subtitle="t('forgot.subtitle')">
      <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
      <form class="stack" novalidate @submit.prevent="onSend">
        <BaseField
          v-model="email"
          :label="t('forgot.email')"
          name="email"
          type="email"
          autocomplete="email"
          :spellcheck="false"
          :error="emailError"
          required
        />
        <BaseTurnstile
          v-if="enabled"
          :key="version"
          v-model="token"
          :site-key="siteKey"
        />
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('forgot.sendCode') }}
        </BaseButton>
      </form>
      <div class="foot">
        <NuxtLink class="quiet" to="/account/login">{{ t('forgot.backToLogin') }}</NuxtLink>
      </div>
    </AuthPanel>

    <AuthPanel
      v-else
      :title="t('forgot.title')"
      :subtitle="t('forgot.subtitle')">
      <p class="notice" role="status">{{ t('forgot.codeSent') }}</p>
      <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
      <form class="stack" novalidate @submit.prevent="submitReset">
        <BaseField
          v-model="code"
          :label="t('forgot.code')"
          name="code"
          autocomplete="one-time-code"
          :spellcheck="false"
          autocorrect="off"
          autocapitalize="none"
          :maxlength="6"
          :error="codeError"
          required
        />
        <BaseField
          v-model="password"
          :label="t('forgot.newPassword')"
          name="new-password"
          type="password"
          autocomplete="new-password"
          :error="passwordError"
          required
        />
        <p class="hint">{{ t('password.hint') }}</p>
        <BaseButton type="submit" :disabled="pending">
          {{ pending ? t('api.working') : t('forgot.submit') }}
        </BaseButton>
      </form>
      <div class="foot">
        <button class="quiet" type="button" @click="backToEmail">
          {{ t('forgot.backToEmail') }}
        </button>
        <span class="quiet sep" aria-hidden="true">|</span>
        <NuxtLink class="quiet" to="/account/login">{{ t('forgot.backToLogin') }}</NuxtLink>
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
.hint {
  margin-top: calc(var(--space-2) * -1);
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--fg-faint);
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
.foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-8);
  margin-top: var(--space-6);
}
</style>
