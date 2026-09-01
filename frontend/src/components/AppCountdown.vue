<script setup lang="ts">
// 倒计时组件：常用于"重新获取验证码"按钮。
// usage: <Countdown :seconds="60" v-slot="{ remaining, done }">重新发送({{ remaining }}s)</Countdown>
import { ref, watch, onBeforeUnmount } from 'vue'

const props = withDefaults(defineProps<{ seconds?: number; start?: boolean }>(), {
  seconds: 60,
  start: true,
})
const emit = defineEmits<{ finish: [] }>()

const remaining = ref(0)
let timer: ReturnType<typeof setInterval> | null = null

function clear() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

function run() {
  clear()
  remaining.value = props.seconds
  timer = setInterval(() => {
    remaining.value--
    if (remaining.value <= 0) {
      clear()
      emit('finish')
    }
  }, 1000)
}

watch(
  () => props.start,
  (v) => (v ? run() : clear()),
  { immediate: true },
)
onBeforeUnmount(clear)
</script>

<template>
  <slot :remaining="remaining" :done="remaining <= 0" />
</template>