// vue-i18n 配置：5 语言，扁平 key 结构，消息按语言打包（中小型站直接全量加载）。
// JSON 消息由 @intlify/unplugin-vue-i18n 构建期预编译（CSP 禁 eval，运行时不能 new Function），
// 插件同时将 vue-i18n 别名到 runtime-only 版本，产物不含消息编译器。

import { createI18n } from 'vue-i18n'

import zhCN from '@/i18n/locales/zh-CN.json'
import zhTW from '@/i18n/locales/zh-TW.json'
import en from '@/i18n/locales/en.json'
import ja from '@/i18n/locales/ja.json'
import ko from '@/i18n/locales/ko.json'

export const LOCALES = [
  { code: 'zh-CN', label: '简体中文' },
  { code: 'zh-TW', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
] as const

export type LocaleCode = (typeof LOCALES)[number]['code']

const STORAGE_KEY = 'nebula-lang'

export function resolveInitialLocale(): LocaleCode {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved && LOCALES.some((l) => l.code === saved)) return saved as LocaleCode
  const nav = navigator.language.toLowerCase()
  const match = LOCALES.find((l) => l.code.toLowerCase() === nav || nav.startsWith(l.code.split('-')[0]))
  return match ? match.code : 'zh-CN'
}

export function persistLocale(code: LocaleCode): void {
  localStorage.setItem(STORAGE_KEY, code)
  document.documentElement.lang = code
}

// 数据由 scripts/gen-locales.mjs 从 src/i18n/sources/ 合并生成（保留 key 结构）
const messages = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
  en,
  ja,
  ko,
}

export const i18n = createI18n({
  legacy: false,
  locale: resolveInitialLocale(),
  fallbackLocale: 'zh-CN',
  messages,
  missingWarn: false,
  fallbackWarn: false,
})