<script setup lang="ts">
import { useI18n, useSeoMeta } from '#imports'
import { usePasswordReset } from '~/composables/usePasswordReset'

const { t } = useI18n()
const {
  backToEmail,
  code,
  codeError,
  email,
  emailError,
  password,
  passwordError,
  step,
  submitEmail,
  submitReset,
} = usePasswordReset()

useSeoMeta({ title: () => t('forgot.title') })
</script>

<template>
  <main>
    <AuthPanel :title="t('forgot.title')" :subtitle="t('forgot.subtitle')">
      <form
        v-if="step === 'email'"
        class="stack"
        novalidate
        @submit.prevent="submitEmail">
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
        <BaseButton type="submit">{{ t('forgot.sendCode') }}</BaseButton>
      </form>

      <form
        v-else
        class="stack"
        novalidate
        @submit.prevent="submitReset">
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
        <BaseButton type="submit">{{ t('forgot.submit') }}</BaseButton>
        <button class="quiet" type="button" @click="backToEmail">
          {{ t('forgot.backToEmail') }}
        </button>
      </form>

      <div class="foot">
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

.foot {
  display: flex;
  justify-content: center;
  margin-top: var(--space-6);
}
</style>
