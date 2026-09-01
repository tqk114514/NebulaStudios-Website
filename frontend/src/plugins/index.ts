// 全局组件/指令注册：避免每个页面重复 import 基础组件，符合"复用不造轮子"。
import type { App } from 'vue'
import { AppModal, ConfirmDialog, AppToast, FormField, AppCountdown } from '@/components'

// 约定：基础组件 kebab-case 全局可用（<app-modal> / <confirm-dialog> / …）
export function registerGlobal(app: App) {
  app.component('AppModal', AppModal)
  app.component('ConfirmDialog', ConfirmDialog)
  app.component('AppToast', AppToast)
  app.component('FormField', FormField)
  app.component('AppCountdown', AppCountdown)
}