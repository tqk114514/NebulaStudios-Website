// Toast 组合式：模块级单例状态 + 全局函数式调用。
// 用法（组件内）：useToast().success('已保存')
// 用法（组件外）：toast.error('网络错误')
import { ref } from 'vue'

export type ToastType = 'info' | 'success' | 'error'

interface ToastItem {
  id: number
  message: string
  type: ToastType
}

// shared module state（全站单例）
const list = ref<ToastItem[]>([])
let nextId = 0

function remove(id: number) {
  list.value = list.value.filter((t) => t.id !== id)
}

function show(message: string, type: ToastType = 'info', duration = 3000) {
  const id = ++nextId
  list.value.push({ id, message, type })
  setTimeout(() => remove(id), duration)
}

export const toast = {
  show: (message: string, type: ToastType = 'info', duration?: number) => show(message, type, duration),
  success: (message: string, duration?: number) => show(message, 'success', duration),
  error: (message: string, duration?: number) => show(message, 'error', duration),
}

export function useToast() {
  return { list, ...toast }
}

// 供 AppToast.vue 渲染层使用的原始状态（避免依赖 hook 返回值）
export function useToastState() {
  return { list, toast }
}