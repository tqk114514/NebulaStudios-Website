<script setup lang="ts">
import { useI18n, useSeoMeta } from '#imports'
import { useSignIn } from '~/composables/useSignIn'

const { t } = useI18n()
const {
  backToCredentials,
  code,
  codeError,
  email,
  emailError,
  password,
  passwordError,
  step,
  submitCode,
  submitCredentials,
} = useSignIn()

// 标签页标题与页面主标题同源，不另立一份文案
useSeoMeta({ title: () => t('login.title') })
</script>

<template>
  <main>
    <AuthPanel
      v-if="step === 'credentials'"
      num="01"
      :title="t('login.title')"
      :subtitle="t('login.subtitle')"
    >
      <form class="stack" novalidate @submit.prevent="submitCredentials">
        <BaseField
          v-model="email"
          :label="t('login.email')"
          name="email"
          type="email"
          autocomplete="username email"
          :error="emailError"
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

        <BaseButton type="submit">{{ t('login.submit') }}</BaseButton>
      </form>

      <p class="divider">{{ t('login.providers') }}</p>

      <div class="stack">
        <!-- 标识位需替换为官方下载素材：Google 四色 G 只能原色置于白底，Microsoft logo 不得改动 -->
        <BaseButton variant="secondary" href="/api/auth/google">
          {{ t('login.signinGoogle') }}
        </BaseButton>
        <BaseButton variant="secondary" href="/api/auth/microsoft">
          {{ t('login.signinMicrosoft') }}
        </BaseButton>
      </div>

      <div class="foot">
        <NuxtLink class="quiet" to="/account/register">{{ t('login.register') }}</NuxtLink>
      </div>
    </AuthPanel>

    <AuthPanel
      v-else
      num="02"
      :title="t('totp.title')"
      :subtitle="t('totp.hint')">
      <form class="stack" novalidate @submit.prevent="submitCode">
        <BaseField
          v-model="code"
          :label="t('totp.code')"
          name="code"
          inputmode="numeric"
          autocomplete="one-time-code"
          :maxlength="6"
          :error="codeError"
          required
        />
        <BaseButton type="submit">{{ t('totp.submit') }}</BaseButton>
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
  justify-content: space-between;
  gap: var(--space-4);
  margin-top: var(--space-6);
}
</style>
