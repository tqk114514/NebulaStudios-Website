#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const LOCALES = ['zh-CN', 'en']
const SOURCE_DIR = 'app'

function flatten(obj, prefix = '') {
  return Object.entries(obj).flatMap(([key, value]) =>
    value !== null && typeof value === 'object'
      ? flatten(value, `${prefix}${key}.`)
      : [`${prefix}${key}`],
  )
}

function filesIn(dir) {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    return statSync(path).isDirectory() ? filesIn(path) : [path]
  })
}

const used = new Set()
for (const file of filesIn(SOURCE_DIR)) {
  if (!file.endsWith('.vue') && !file.endsWith('.ts')) continue
  const source = readFileSync(file, 'utf8')
  for (const match of source.matchAll(/\bt\(\s*'([^']+)'/g)) used.add(match[1])
  for (const match of source.matchAll(/\$t\(\s*'([^']+)'/g)) used.add(match[1])
  // <i18n-t> 的键写在模板属性上，不扫到就会被误报为未引用
  for (const match of source.matchAll(/keypath="([^"]+)"/g)) used.add(match[1])
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
