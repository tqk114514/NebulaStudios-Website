<script setup lang="ts">
import { ref } from 'vue'

const email = ref('')
const password = ref('')
</script>

<template>
  <main>
    <AuthPanel
      num="01"
      title="登录 Nebula"
      subtitle="邮箱与密码，或从已绑定的第三方账号进入。"
    >
      <div class="form">
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
        <BaseButton type="submit">登录</BaseButton>
        <BaseButton variant="secondary">使用恢复码</BaseButton>
      </div>
    </AuthPanel>
  </main>
</template>

<style scoped>
.form {
  display: grid;
  gap: var(--space-4);
}
</style>
