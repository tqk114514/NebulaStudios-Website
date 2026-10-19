<script setup lang="ts">
import { useI18n } from '#imports'
import { useSignUp } from '~/composables/useSignUp'

const { t } = useI18n()
const {
  confirm,
  confirmError,
  email,
  formError,
  password,
  submit,
  username,
  usernameError,
} = useSignUp()
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
          required
        />
        <BaseField
          v-model="username"
          :label="t('register.username')"
          name="username"
          autocomplete="username"
          :error="usernameError"
          required
        />
        <BaseField
          v-model="password"
          :label="t('register.password')"
          name="new-password"
          type="password"
          autocomplete="new-password"
          required
        />
        <BaseField
          v-model="confirm"
          :label="t('register.confirmPassword')"
          name="confirm-password"
          type="password"
          autocomplete="new-password"
          :error="confirmError"
          required
        />

        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>

        <BaseButton type="submit">{{ t('register.submit') }}</BaseButton>
      </form>

      <div class="foot">
        <span class="quiet">{{ t('register.haveAccount') }}</span>
        <NuxtLink class="quiet" to="/account/login">{{ t('register.signIn') }}</NuxtLink>
      </div>
    </AuthPanel>
  </main>
</template>

<style scoped>
.stack {
  display: grid;
  gap: var(--space-4);
}

.form-error {
  font-size: var(--text-xs);
  letter-spacing: var(--track-label);
  color: var(--error);
}

.foot {
  display: flex;
  justify-content: space-between;
  gap: var(--space-4);
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
