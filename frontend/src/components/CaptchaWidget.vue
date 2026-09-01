<script setup lang="ts">
// Turnstile 人机验证组件。系统未启用时渲染为空。
// 用法：<CaptchaWidget />（内部自管理导入SDK+渲染；token 通过 useCaptcha().getCaptchaToken() 读取）
// 注意：子组件 onMounted 早于父页面（后者负责 loadCaptchaConfig），因此监听全局 enabled
// 状态而非只在挂载时读一次——配置异步就绪后再渲染。
import { ref, watch, onMounted, nextTick } from 'vue'
import { initCaptcha, captchaEnabled } from '@/composables/useCaptcha'

const el = ref<HTMLElement | null>(null)
const show = ref(false)
let rendered = false

async function tryRender() {
  if (!captchaEnabled.value) return
  show.value = true
  await nextTick() // v-if 容器先落地
  if (el.value && !rendered) {
    rendered = true
    await initCaptcha(el.value)
  }
}

onMounted(tryRender)
watch(captchaEnabled, (v) => v && tryRender())
</script>

<template>
  <div v-if="show" ref="el" class="captcha-wrapper" />
</template>

<style scoped>
.captcha-wrapper {
  margin-top: 16px;
}
</style>