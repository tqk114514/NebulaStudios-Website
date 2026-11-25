<script setup lang="ts">
import { useI18n, useSeoMeta } from '#imports'
import { useSignUp } from '~/composables/useSignUp'

const { t } = useI18n()
const {
  email,
  formError,
  password,
  passwordError,
  submit,
  username,
  usernameError,
} = useSignUp()

// 标签页标题与页面主标题同源，不另立一份文案
useSeoMeta({ title: () => t('register.title') })
</script>

<template>
  <main>
    <AuthPanel :title="t('register.title')" :subtitle="t('register.subtitle')">
      <form class="stack" novalidate @submit.prevent="submit">
        <BaseField
          v-model="email"
          :label="t('register.email')"
          name="email"
          type="email"
          autocomplete="email"
          :spellcheck="false"
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

        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>

        <BaseButton type="submit">{{ t('register.submit') }}</BaseButton>
      </form>

      <div class="foot">
        <NuxtLink class="quiet" to="/account/login">{{ t('register.backToLogin') }}</NuxtLink>
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

.form-error {
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--error);
}

.foot {
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: var(--space-6);
}

.quiet {
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
}

a.quiet:hover {
  color: var(--fg);
}
</style>
