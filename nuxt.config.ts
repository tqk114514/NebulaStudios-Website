export default defineNuxtConfig({
  compatibilityDate: '2026-10-18',

  app: {
    head: {
      htmlAttrs: { lang: 'zh-Hans' },
      title: 'Nebula Studios',
    },
  },

  css: [
    '@fontsource/unbounded/400.css',
    '@fontsource/unbounded/700.css',
    '@fontsource/unbounded/900.css',
    '@fontsource/azeret-mono/300.css',
    '@fontsource/azeret-mono/400.css',
    '@fontsource/azeret-mono/500.css',
    '@fontsource-variable/newsreader/wght.css',
    '@fontsource-variable/newsreader/wght-italic.css',
    '~/assets/css/tokens.css',
    '~/assets/css/base.css',
  ],

  // 公开页走 SSR；账号与后台为客户端渲染（见 AGENTS.md §10）
  routeRules: {
    '/account/**': { ssr: false },
    '/admin': { ssr: false },
  },

  typescript: {
    typeCheck: true,
    strict: true,
  },
})
