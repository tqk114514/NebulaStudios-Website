<script setup lang="ts">
// 支持的邮箱列表弹窗：展示白名单域名。供注册页"查看支持的邮箱"入口复用。
// 点击服务商不直接跳转：先经外链确认弹窗（迁移自旧版 showExternalLinkConfirm），
// 确认后 window.open(_blank, noopener)；悬停时条目右侧显示箭头提示可跳转。
// 样式自持（scoped）：迁移自原站 #supported-emails-list / .email-provider-*（值不变，命名 BEM 化）。
import { computed, ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
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

const pendingUrl = ref('')
const confirmOpen = ref(false)

function requestExternal(url: string): void {
  pendingUrl.value = url
  confirmOpen.value = true
}

function goExternal(): void {
  if (pendingUrl.value) {
    window.open(pendingUrl.value, '_blank', 'noopener,noreferrer')
  }
  confirmOpen.value = false
}
</script>

<template>
  <AppModal :open="open" :title="$t('account.register.supportedEmailsTitle')" :width="'400px'" @update:open="(v) => emit('update:open', v)">
    <div class="supported-emails__list">
      <button
        v-for="it in items"
        :key="it.domain"
        class="supported-emails__item"
        :class="{ 'supported-emails__item--link': !!it.signupUrl }"
        type="button"
        @click="it.signupUrl && requestExternal(it.signupUrl)"
      >
        <img
          class="supported-emails__logo"
          :class="{ 'supported-emails__logo--empty': !it.logoUrl }"
          :src="it.logoUrl || EMPTY_LOGO_SRC"
          :alt="it.logoUrl ? it.domain : ''"
          loading="lazy"
        />
        <span class="supported-emails__domain">{{ it.domain }}</span>
        <svg
          v-if="it.signupUrl"
          class="supported-emails__arrow"
          viewBox="0 0 24 24"
          width="16"
          height="16"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path d="M5 12h14M13 6l6 6-6 6" />
        </svg>
      </button>
    </div>
    <template #footer>
      <AppButton :arrow="false" @click="emit('update:open', false)">
        {{ $t('modal.close') }}
      </AppButton>
    </template>

    <!-- 外链确认：迁移自旧版 external-link-modal；点击遮罩关闭与全站弹窗一致 -->
    <ConfirmDialog
      v-model:open="confirmOpen"
      :title="$t('modal.externalLink.title')"
      :cancel-text="$t('modal.cancel')"
      :confirm-text="$t('modal.externalLink.continue')"
      @confirm="goExternal"
    >
      <p class="supported-emails__confirm-line">{{ $t('modal.externalLink.message') }}</p>
      <p class="supported-emails__confirm-url">{{ pendingUrl }}</p>
      <p class="supported-emails__confirm-disclaimer">{{ $t('modal.externalLink.disclaimer') }}</p>
    </ConfirmDialog>
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

/* 迁移自原站 .email-provider-item（值不变；原站为 div，此处为 button，补齐原生控件重置） */
.supported-emails__item {
  width: 100%;
  background: none;
  border: none;
  cursor: default;
  font-family: inherit;
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
.supported-emails__item--link {
  cursor: pointer;
}

/* 悬停出现的右侧箭头（样式对齐 app-button 箭头） */
.supported-emails__arrow {
  flex-shrink: 0;
  opacity: 0;
  transform: translateX(-4px);
  transition: opacity 0.2s, transform 0.2s;
}
.supported-emails__item--link:hover .supported-emails__arrow {
  opacity: 1;
  transform: translateX(0);
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

/* 外链确认正文（迁移自旧版 external-link-modal） */
.supported-emails__confirm-line {
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  color: var(--mid);
  line-height: 1.8;
}
.supported-emails__confirm-url {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  color: var(--fg);
  word-break: break-all;
  margin-bottom: 12px;
}
.supported-emails__confirm-disclaimer {
  font-size: var(--text-xs);
  color: var(--mid);
  line-height: 1.7;
}
</style>
