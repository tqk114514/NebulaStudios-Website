<script setup lang="ts">
import { useI18n } from '#imports'

const revealed = defineModel<boolean>({ required: true })
const { t } = useI18n()
</script>

<template>
  <button
    class="password-reveal"
    type="button"
    :aria-pressed="revealed"
    :aria-label="revealed ? t('form.hidePassword') : t('form.showPassword')"
    :title="revealed ? t('form.hidePassword') : t('form.showPassword')"
    @click="revealed = !revealed"
  >
    <svg v-if="revealed" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M3 3l18 18M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 5.2A9.8 9.8 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3.1 4.1M6.6 6.6C3.6 8.5 2 12 2 12s3.5 7 10 7a9.8 9.8 0 0 0 4.4-1" />
    </svg>
    <svg v-else viewBox="0 0 24 24" aria-hidden="true">
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z" />
    </svg>
  </button>
</template>

<style scoped>
.password-reveal {
  position: absolute;
  right: 0;
  display: grid;
  place-items: center;
  width: var(--space-10);
  height: var(--space-10);
  padding: 0;
  border: none;
  background: transparent;
  color: var(--fg-muted);
  cursor: pointer;
}
.password-reveal:hover {
  color: var(--fg);
}
.password-reveal:focus-visible {
  outline: 2px solid var(--fg);
  outline-offset: 2px;
}
.password-reveal svg {
  width: 20px;
  height: 20px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linecap: round;
  stroke-linejoin: round;
}
</style>
