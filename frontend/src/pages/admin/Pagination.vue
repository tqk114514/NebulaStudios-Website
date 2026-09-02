<!-- 分页控件（迁移自旧版 common.ts renderPagination 的窗口算法，值不变） -->
<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  current: number
  total: number
}>()

const emit = defineEmits<{
  change: [page: number]
}>()

const pages = computed<(number | 'ellipsis')[]>(() => {
  if (props.total <= 1) return []
  const start = Math.max(1, props.current - 2)
  const end = Math.min(props.total, props.current + 2)
  const list: (number | 'ellipsis')[] = []
  if (start > 1) {
    list.push(1)
    if (start > 2) list.push('ellipsis')
  }
  for (let i = start; i <= end; i++) list.push(i)
  if (end < props.total) {
    if (end < props.total - 1) list.push('ellipsis')
    list.push(props.total)
  }
  return list
})
</script>

<template>
  <div v-if="pages.length > 0" class="adm-pagination">
    <button type="button" :disabled="current === 1" @click="emit('change', current - 1)">上一页</button>
    <template v-for="(p, i) in pages" :key="`${p}-${i}`">
      <button v-if="p === 'ellipsis'" type="button" disabled>...</button>
      <button v-else type="button" :class="{ active: p === current }" @click="p !== current && emit('change', p)">
        {{ p }}
      </button>
    </template>
    <button type="button" :disabled="current === total" @click="emit('change', current + 1)">下一页</button>
  </div>
</template>
