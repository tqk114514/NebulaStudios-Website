/**
 * 样式只许引用令牌，不许写字面值（AGENTS.md §7）。
 * 两条规则分工：disallowed-list 挡住具体写法（十六进制、rgb()、cubic-bezier、
 * 硬写 font-family），strict-value 要求这些属性必须给出 var() 引用。
 */
const COLOR_PROPERTIES = [
  '/color$/',
  'background',
  'background-color',
  'fill',
  'stroke',
  'font-family',
  'transition-timing-function',
]

export default {
  plugins: ['stylelint-declaration-strict-value'],
  overrides: [
    { files: ['**/*.vue'], customSyntax: 'postcss-html' },
    {
      // 令牌的定义处只能写字面值，其余地方一律只许引用
      files: ['app/assets/css/tokens.css'],
      rules: {
        'declaration-property-value-disallowed-list': null,
        'scale-unlimited/declaration-strict-value': null,
      },
    },
  ],
  rules: {
    'declaration-property-value-disallowed-list': {
      '/^(color|background|background-color|border|border-color|border-top|border-bottom|border-left|border-right|outline|outline-color|fill|stroke|box-shadow|accent-color)$/':
        ['/^#[0-9a-fA-F]/', '/^rgb/', '/^hsl/'],
      'font-family': ['/^(?!var\\()/'],
      'transition-timing-function': ['/^cubic-bezier\\(/'],
    },
    'scale-unlimited/declaration-strict-value': [
      COLOR_PROPERTIES,
      {
        disableFix: true,
        ignoreValues: ['transparent', 'currentColor', 'inherit', 'initial', 'unset', 'none'],
        message: '该值必须引用设计令牌 var(--…)（AGENTS.md §7）',
      },
    ],
  },
}
