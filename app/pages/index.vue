<script setup lang="ts">
import { ref } from 'vue'

const email = ref('')
const password = ref('')
</script>

<template>
  <main class="atoms">
    <h1 class="atoms__title">ONE IDENTITY EVERYWHERE</h1>

    <div class="atoms__group">
      <BaseButton>开始使用</BaseButton>
      <BaseButton variant="secondary">登录</BaseButton>
      <BaseButton size="sm" :arrow="false">撤销</BaseButton>
    </div>

    <div class="atoms__group atoms__group--narrow">
      <BaseField
        v-model="email"
        label="邮箱"
        name="email"
        type="email"
        autocomplete="username email"
      />
      <BaseField
        v-model="password"
        label="密码"
        name="password"
        type="password"
        autocomplete="current-password"
        error="邮箱或密码不正确"
      />
    </div>
  </main>
</template>

<style scoped>
.atoms {
  max-width: var(--page-max);
  margin-inline: auto;
  padding: var(--space-16) var(--page-pad);
  display: grid;
  gap: var(--space-12);
}

.atoms__title {
  font-family: var(--font-display);
  font-size: var(--text-hero);
  font-weight: 700;
  line-height: var(--lh-heading);
  letter-spacing: var(--track-none);
}

.atoms__group {
  display: grid;
  gap: var(--space-4);
  max-width: var(--measure);
}

.atoms__group--narrow {
  max-width: var(--form-max);
}
</style>
