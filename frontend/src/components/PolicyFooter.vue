<script setup lang="ts">
// 页面尾部：政策链接行 + 版权（迁移自原站 .policy-links / .page-footer）。
// 原站用绝对定位让 "|" 精确居中，这里改用 flex 等间距布局，视觉等价。
// 政策页自身再挂这组链接属于自引用，传入 links=false 隐藏链接行、仅保留版权。
withDefaults(defineProps<{ links?: boolean }>(), { links: true })
</script>

<template>
  <nav v-if="links" class="policy-footer" aria-label="policy">
    <a href="/policy#privacy" target="_blank">{{ $t('policy.privacyPolicy') }}</a>
    <span>|</span>
    <a href="/policy#terms" target="_blank">{{ $t('policy.termsOfService') }}</a>
  </nav>
  <p class="policy-footer__copyright" translate="no">{{ $t('footer.copyright') }}</p>
</template>

<style scoped>
.policy-footer {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 32px;
  margin-top: 24px;
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
}

.policy-footer a {
  color: var(--mid);
  transition: color 0.2s;
}

.policy-footer a:hover {
  color: var(--fg);
}

.policy-footer span {
  color: var(--dim);
}

.policy-footer__copyright {
  margin-top: 16px;
  margin-bottom: 40px;
  font-size: var(--text-xs);
  letter-spacing: 0.18em;
  color: var(--mid);
  text-align: center;
}
</style>