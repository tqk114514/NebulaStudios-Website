<script setup lang="ts">
import { useI18n } from '#imports'
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
  remember,
  step,
  submitCode,
  submitCredentials,
} = useSignIn()
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

        <label class="check">
          <input v-model="remember" type="checkbox" name="remember">
          {{ t('login.remember') }}
        </label>

        <BaseButton type="submit">{{ t('login.submit') }}</BaseButton>
      </form>

      <p class="divider">{{ t('login.providers') }}</p>

      <div class="stack">
        <!-- 标识位需替换为官方下载素材：Google 四色 G 只能原色置于白底，Microsoft logo 不得改动 -->
        <a class="provider" href="/api/auth/google">{{ t('login.signinGoogle') }}</a>
        <a class="provider" href="/api/auth/microsoft">{{ t('login.signinMicrosoft') }}</a>
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

.check {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg-muted);
}

.check input {
  width: var(--target-min);
  height: var(--target-min);
  accent-color: var(--fg);
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

.provider {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: var(--target-comfort);
  padding: 0 var(--space-5);
  background: var(--surface);
  border: 1px solid var(--line-strong);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: var(--track-mono);
  color: var(--fg);
  transition: background-color var(--dur-ui) var(--ease-standard);
}

.provider:hover {
  background: var(--surface-raised);
}

.foot {
  display: flex;
  justify-content: space-between;
  gap: var(--space-4);
  margin-top: var(--space-6);
}
</style>
