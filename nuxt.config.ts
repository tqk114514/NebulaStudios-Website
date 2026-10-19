export default defineNuxtConfig({
  compatibilityDate: '2026-10-18',

  modules: ['@nuxtjs/i18n', '@nuxt/eslint'],

  devtools: { enabled: false },

  // 类型感知规则只在给出 tsconfigPath 时才叠加；strict 默认为 true，会再叠一层
  // strict-type-checked，§7 要的是 recommended-type-checked，故关掉
  eslint: {
    config: {
      typescript: { tsconfigPath: './tsconfig.json', strict: false },
    },
  },

  i18n: {
    strategy: 'no_prefix',
    defaultLocale: 'zh-CN',
    locales: [
      { code: 'zh-CN', language: 'zh-Hans', file: 'zh-CN.json', name: '简体中文' },
      { code: 'en', language: 'en-US', file: 'en.json', name: 'English' },
    ],
  },

  app: {
    head: {
      // htmlAttrs.lang 由 app.vue 读 locales[].language 写入，跟着当前 locale 走
      title: 'Nebula Studios',
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
    },
  },

  css: [
    '@fontsource/unbounded/200.css',
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

  components: [
    { path: '~/components/base', pathPrefix: false },
    { path: '~/components/patterns', pathPrefix: false },
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
