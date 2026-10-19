import config from '@commitlint/config-conventional'

/**
 * §11：Conventional Commits，必需类型只有 feat / fix —— 这里把 type-enum 收紧到
 * 仓库实际在用的集合，其余约定（! 或 BREAKING CHANGE 标破坏性）沿用 preset 默认。
 * subject-case 关掉：提交信息里有中文与专有名词，大小写判定不适用。
 */
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    ...config.rules,
    'type-enum': [
      2,
      'always',
      ['feat', 'fix', 'refactor', 'docs', 'test', 'perf', 'build', 'ci', 'chore'],
    ],
    'subject-case': [0],
  },
}
