<script setup lang="ts">
// Cloudflare Turnstile 显式渲染封装；脚本全局只加载一次，组件实例负责自己的容器。
// Turnstile 各模式均不出认知题目（AGENTS §9.12），无需替代验证路径。
import { onMounted, ref } from 'vue'

interface TurnstileApi {
  render: (container: HTMLElement, options: {
    sitekey: string
    callback: (token: string) => void
    'expired-callback': () => void
    'error-callback': () => void
  }) => string
}

const token = defineModel<string>({ default: '' })
const props = defineProps<{ siteKey: string }>()
const container = ref<HTMLElement | null>(null)

const SCRIPT_ID = 'cloudflare-turnstile'
const SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

function loadScript(): Promise<void> {
  if (document.getElementById(SCRIPT_ID)) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const script = document.createElement('script')
    script.id = SCRIPT_ID
    script.src = SCRIPT_URL
    script.async = true
    script.defer = true
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('Turnstile 脚本加载失败'))
    document.head.appendChild(script)
  })
}

onMounted(async () => {
  try {
    await loadScript()
  } catch {
    return
  }
  const turnstile = (window as Window & { turnstile?: TurnstileApi }).turnstile
  if (!turnstile || !container.value) return
  turnstile.render(container.value, {
    sitekey: props.siteKey,
    callback: (value) => { token.value = value },
    'expired-callback': () => { token.value = '' },
    'error-callback': () => { token.value = '' },
  })
})
</script>

<template>
  <div ref="container" class="base-turnstile" />
</template>

<style scoped>
.base-turnstile {
  min-height: 65px;
}
</style>
