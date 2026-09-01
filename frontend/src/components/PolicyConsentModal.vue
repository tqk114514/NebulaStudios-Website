<script setup lang="ts">
// 强制政策同意弹窗：由 usePolicyConsent().check() 触发，通过单例 consentState 驱动。
// 关闭不可点（closable=false）：必须明确同意或拒绝。同意后 resolve(true)，拒绝登出 resolve(false)。
// 样式自持（scoped）：迁移自原站 .policy-consent-* / .modal-footer-buttons（值不变，命名 BEM 化）。
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'
import { consentState, usePolicyConsent } from '@/composables/usePolicyConsent'

const { accept, decline } = usePolicyConsent()
const agreed = ref(false)

const POLICY_KEYS: Record<string, string> = {
  privacy: 'policy.privacyPolicy',
  terms: 'policy.termsOfService',
}

function policyName(type: string): string {
  return POLICY_KEYS[type] ?? type
}

function toRecords() {
  return consentState.policies.value.map((p) => ({
    policy_type: p.policy_type,
    policy_version: p.version,
  }))
}
</script>

<template>
  <AppModal :open="consentState.visible.value" :closable="false" :title="$t('account.policy.consent.title')">
    <p class="policy-consent__message">{{ $t('account.policy.consent.message') }}</p>

    <ul class="policy-consent__list">
      <li v-for="p in consentState.policies.value" :key="p.policy_type + p.version" class="policy-consent__item">
        <a :href="`/policy#${p.policy_type}`" target="_blank" rel="noopener noreferrer" class="policy-consent__link">
          {{ $t(policyName(p.policy_type)) }}
        </a>
        <span v-if="p.effective_date" class="policy-consent__date">
          {{ $t('account.policy.consent.effectiveDate') }} {{ p.effective_date }}
        </span>
      </li>
    </ul>

    <label class="policy-consent__checkbox">
      <input v-model="agreed" type="checkbox" />
      <span>{{ $t('account.policy.consent.agree') }}</span>
    </label>

    <p v-if="consentState.error.value" class="policy-consent__error">
      {{ $t('account.policy.consent.failed') }}
    </p>

    <template #footer>
      <div class="policy-consent__footer">
        <AppButton variant="secondary" :arrow="false" :disabled="consentState.loading.value" @click="decline">
          {{ $t('account.policy.consent.decline') }}
        </AppButton>
        <AppButton
          variant="primary"
          :arrow="false"
          :disabled="!agreed || consentState.loading.value"
          @click="accept(toRecords())"
        >
          {{ consentState.loading.value ? $t('account.policy.consent.submitting') : $t('account.policy.consent.accept') }}
        </AppButton>
      </div>
    </template>
  </AppModal>
</template>

<style scoped>
/* 迁移自原站 .modal-message + .policy-consent-message（值不变） */
.policy-consent__message {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
  margin-bottom: 20px;
}

/* 迁移自原站 .policy-consent-list（值不变；list-style:none 对齐原站 div 列表无符号） */
.policy-consent__list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 24px;
  list-style: none;
}

/* 迁移自原站 .policy-consent-item（值不变） */
.policy-consent__item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: var(--dim);
  border: 1px solid var(--line);
}

/* 迁移自原站 .policy-consent-link（值不变） */
.policy-consent__link {
  color: var(--fg);
  text-decoration: underline;
  text-underline-offset: 2px;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  transition: color 0.2s;
}
.policy-consent__link:hover {
  color: var(--fg-hover);
}

/* 迁移自原站 .policy-consent-date（值不变） */
.policy-consent__date {
  margin-left: auto;
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  color: var(--mid);
  white-space: nowrap;
}

/* 迁移自原站 .policy-consent-checkbox（值不变） */
.policy-consent__checkbox {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 20px;
  cursor: pointer;
  user-select: none;
}
.policy-consent__checkbox input[type='checkbox'] {
  width: 16px;
  height: 16px;
  accent-color: var(--fg);
  cursor: pointer;
}
.policy-consent__checkbox span {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--mid);
}

/* 迁移自原站 .policy-consent-error（值不变） */
.policy-consent__error {
  font-size: var(--text-sm);
  letter-spacing: 0.1em;
  color: var(--error);
  margin-bottom: 16px;
}

/* 迁移自原站 .modal-footer-buttons（值不变） */
.policy-consent__footer {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
