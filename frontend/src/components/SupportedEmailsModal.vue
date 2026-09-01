<script setup lang="ts">
// 支持的邮箱列表弹窗：展示白名单域名。供注册页"查看支持的邮箱"入口复用。
// 样式自持（scoped）：迁移自原站 #supported-emails-list / .email-provider-*（值不变，命名 BEM 化）。
import { computed } from 'vue'
import AppModal from '@/components/AppModal.vue'
import AppButton from '@/components/AppButton.vue'
import { getEmailProviders } from '@/composables/useEmailWhitelist'

defineProps<{ open: boolean }>()

const emit = defineEmits<{ 'update:open': [value: boolean] }>()

// 模块级 ref 的响应式视图：白名单是异步加载的，须随加载完成自动更新列表
const items = computed(() =>
  Object.entries(getEmailProviders()).map(([domain, p]) => ({
    domain,
    signupUrl: p.signup_url ?? '',
    logoUrl: p.logo_url ?? '',
  })),
)

// 无 logo 的域名渲染 24px 透明占位（对齐原站 .empty-logo，保持域名左侧对齐）
const EMPTY_LOGO_SRC =
  'data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 24 24%22%3E%3C/svg%3E'
</script>

<template>
  <AppModal :open="open" :title="$t('account.register.supportedEmailsTitle')" :width="'400px'" @update:open="(v) => emit('update:open', v)">
    <div class="supported-emails__list">
      <a
        v-for="it in items"
        :key="it.domain"
        class="supported-emails__item"
        :href="it.signupUrl || undefined"
        :target="it.signupUrl ? '_blank' : undefined"
        :rel="it.signupUrl ? 'noopener noreferrer' : undefined"
        :style="it.signupUrl ? { cursor: 'pointer' } : undefined"
      >
        <img
          class="supported-emails__logo"
          :class="{ 'supported-emails__logo--empty': !it.logoUrl }"
          :src="it.logoUrl || EMPTY_LOGO_SRC"
          :alt="it.logoUrl ? it.domain : ''"
          loading="lazy"
        />
        <span class="supported-emails__domain">{{ it.domain }}</span>
      </a>
    </div>
    <template #footer>
      <AppButton :arrow="false" @click="emit('update:open', false)">
        {{ $t('modal.close') }}
      </AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
/* 迁移自原站 #supported-emails-list（值不变）：限高滚动且隐藏滚动条 */
.supported-emails__list {
  max-height: 300px;
  overflow-y: auto;
  -ms-overflow-style: none;
  scrollbar-width: none;
}
.supported-emails__list::-webkit-scrollbar {
  display: none;
}

/* 迁移自原站 .email-provider-item（值不变；原站为 div，此处为 a，下划线由全局 a 重置去除） */
.supported-emails__item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--fg);
  border-bottom: 1px solid var(--dim);
  transition: background 0.2s, color 0.2s;
}
.supported-emails__item:last-child {
  border-bottom: none;
}
.supported-emails__item:hover {
  background: var(--line);
  color: var(--fg);
}

/* 迁移自原站 .email-provider-logo / .empty-logo（值不变） */
.supported-emails__logo {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  object-fit: contain;
  border-radius: 4px;
}
.supported-emails__logo--empty {
  opacity: 0;
  pointer-events: none;
}

/* 迁移自原站 .email-provider-domain（值不变） */
.supported-emails__domain {
  flex: 1;
}
</style>
