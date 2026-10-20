<script setup lang="ts">
import { computed } from 'vue'
import { useHead, useI18n } from '#imports'

// `<html lang>` 跟着当前 locale 走：读屏按它选发音，CJK 字体回退也看它。
// 语言标签的唯一出处是 nuxt.config 的 locales[].language，这里只做查表。
// 不用 useLocaleHead：它无论如何都会构造 hreflang 的 alternate 链接（`seo: false`
// 只挡产出标签，挡不住构造），没有绝对 baseUrl 时就只剩一句告警。
const { locale, locales } = useI18n()

const language = computed(() => {
  for (const entry of locales.value) {
    if (typeof entry !== 'string' && entry.code === locale.value) return entry.language ?? entry.code
  }
  return locale.value
})

useHead({ htmlAttrs: { lang: language } })
</script>

<template>
  <NuxtRouteAnnouncer />
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
