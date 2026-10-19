<script setup lang="ts">
import { useHead, useLocaleHead } from '#imports'

// `<html lang>` 跟着当前 locale 走：读屏按它选发音，CJK 字体回退也看它。
// 因此 nuxt.config 里不再静态写死 lang。
const head = useLocaleHead({ dir: false, lang: true })
useHead(head)
</script>

<template>
  <NuxtRouteAnnouncer />
  <NuxtLayout>
    <NuxtPage />
  </NuxtLayout>
</template>
