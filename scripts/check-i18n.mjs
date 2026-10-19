#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const LOCALES = ['zh-CN', 'en']
const SOURCE_DIR = 'app'

function flatten(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) => {
    // 数组是富文本分段这类消息本体，不是命名空间：展开会把 text1 拆成 text1.0.s
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return [`${prefix}${key}`]
    return flatten(value, `${prefix}${key}.`)
  })
}

function filesIn(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    return statSync(path).isDirectory() ? filesIn(path) : [path]
  })
}

const used = new Set()
// 任何点号字面量都算一次引用：t()、tm()、<i18n-t keypath>、以及放进数组里再间接
// 取用的键都要覆盖到。按调用名匹配会漏掉后两种，而 app/** 里不存在第二种形状的
// 字符串（点号字面量只有键），所以这条规则没有误报面。
const KEY_LITERAL = /'([a-z][\w-]*(?:\.[\w-]+)+)'/g
for (const file of filesIn(SOURCE_DIR)) {
  if (!file.endsWith('.vue') && !file.endsWith('.ts')) continue
  const source = readFileSync(file, 'utf8')
  for (const match of source.matchAll(KEY_LITERAL)) used.add(match[1])
}

const keysByLocale = Object.fromEntries(
  LOCALES.map((locale) => [
    locale,
    flatten(JSON.parse(readFileSync(`i18n/locales/${locale}.json`, 'utf8'))).sort(),
  ]),
)

const failures = []

for (const locale of LOCALES) {
  const keys = new Set(keysByLocale[locale])
  const missing = [...used].filter((key) => !keys.has(key))
  if (missing.length > 0) failures.push(`${locale} 缺键：${missing.join(', ')}`)
}

const [reference, ...others] = LOCALES.map((locale) => keysByLocale[locale])
for (const [index, keys] of others.entries()) {
  const other = LOCALES[index + 1]
  const asymmetric = reference.filter((key) => !keys.includes(key)).concat(keys.filter((key) => !reference.includes(key)))
  if (asymmetric.length > 0) failures.push(`zh-CN 与 ${other} 键集不一致：${[...new Set(asymmetric)].join(', ')}`)
}

const unused = reference.filter((key) => !used.has(key))
if (unused.length > 0) failures.push(`未被引用的键：${unused.join(', ')}`)

if (failures.length > 0) {
  console.error(failures.join('\n'))
  process.exit(1)
}

console.log(`i18n ok：${used.size} 个键在用，${LOCALES.length} 份语言文件键集一致`)
