<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from '#imports'

const { t } = useI18n()

const email = ref('')
const username = ref('')
const password = ref('')
const confirm = ref('')
const submitted = ref(false)

const usernameError = computed(() =>
  submitted.value && username.value.length > 0 && username.value.length < 3 ? t('register.usernameTaken') : '',
)
const passwordError = computed(() =>
  submitted.value && confirm.value.length > 0 && confirm.value !== password.value
    ? t('register.passwordMismatch')
    : '',
)

function submit() {
  submitted.value = true
}
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
          :error="passwordError"
          required
        />

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
