// @ts-check
import pluginVitest from '@vitest/eslint-plugin'
import withNuxt from './.nuxt/eslint.config.mjs'

/**
 * 规范权威只有 @nuxt/eslint（§7）：不叠 antfu 配置、不装 Prettier。
 * 这里只补两类东西——§7 点名要求的规则（逐条写出，不依赖 preset 落在哪个档位），
 * 以及项目自定的行数上限与目录例外。
 */
export default withNuxt(
  {
    name: 'nebula/ignores',
    // Reference/ 是 v3 归档，不参与构建、测试与检索（AGENTS.md 开头）
    ignores: ['Reference/**'],
  },
  {
    name: 'nebula/line-budget',
    rules: {
      // 普通模块。SFC 与 composable 入口的更严上限在下面的块里覆盖
      'max-lines': ['error', { max: 120, skipBlankLines: false, skipComments: false }],
    },
  },
  {
    name: 'nebula/vue',
    files: ['**/*.vue'],
    rules: {
      'max-lines': ['error', { max: 200, skipBlankLines: false, skipComments: false }],
      'vue/block-order': ['error', { order: ['script', 'template', 'style'] }],
      'vue/component-name-in-template-casing': ['error', 'PascalCase'],
      'vue/custom-event-name-casing': ['error', 'kebab-case'],
      'vue/define-macros-order': ['error', { order: ['defineOptions', 'defineModel', 'defineProps', 'defineEmits', 'defineSlots'] }],
      'vue/html-indent': ['error', 2],
      'vue/html-self-closing': 'error',
      // 单行放行 3 个属性：官方默认是 1，会把 `<form class novalidate @submit>` 这类
      // 短标签全部炸成多行；而 @nuxt/eslint 默认不开 stylistic，该规则的 fixer 只换行
      // 不缩进，产出的是一堆顶格的烂代码。多行标签仍要求逐属性一行。
      'vue/max-attributes-per-line': ['error', { singleline: { max: 3 }, multiline: { max: 1 } }],
      'vue/no-use-v-if-with-v-for': 'error',
      'vue/prop-name-casing': ['error', 'camelCase'],
      'vue/require-v-for-key': 'error',
    },
  },
  {
    name: 'nebula/composable-budget',
    files: ['app/composables/**/index.ts'],
    rules: {
      'max-lines': ['error', { max: 80, skipBlankLines: false, skipComments: false }],
    },
  },
  {
    // tsconfig 引入了 @types/node（构建期文件需要），客户端代码里的 Node 全局在此拦掉
    name: 'nebula/no-node-globals-in-app',
    files: ['app/**/*.{ts,vue}'],
    rules: {
      'no-restricted-globals': ['error', 'process', 'Buffer', '__dirname', 'require'],
    },
  },
  {
    name: 'nebula/typescript',
    files: ['**/*.ts', '**/*.tsx', '**/*.vue'],
    rules: {
      '@typescript-eslint/consistent-type-imports': ['error', { fixStyle: 'separate-type-imports' }],
      '@typescript-eslint/no-import-type-side-effects': 'error',
      '@typescript-eslint/ban-ts-comment': ['error', { 'ts-expect-error': 'allow-with-description' }],
    },
  },
  {
    // 生成代码目录落地后在此设例外
    name: 'nebula/no-escape-hatches',
    files: ['app/**/*.{ts,vue}'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
    },
  },
  {
    name: 'nebula/vitest',
    files: ['test/**/*.ts'],
    plugins: { vitest: pluginVitest },
    rules: {
      'vitest/expect-expect': 'error',
      'vitest/no-commented-out-tests': 'error',
      'vitest/no-disabled-tests': 'error',
      'vitest/no-focused-tests': 'error',
      'vitest/valid-title': 'error',
    },
  },
)
