// 一次性脚本：从 src/i18n/sources/{general,account,home,policy} 各语言 JSON
// 合并生成 frontend/src/i18n/locales/<lang>.json
// 运行：node scripts/gen-locales.mjs
// 为避免 key 冲突，account/home 的 key 加前缀（account 原本可能就是扁平 key）。
// 输出 JSON 而非 TS：@intlify/unplugin-vue-i18n 构建期预编译消息（CSP 禁 eval），
// 消息在运行时以函数形式存在，不再需要 new Function。
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const srcDir = join(root, 'src', 'i18n', 'sources')
const outDir = join(root, 'src', 'i18n', 'locales')

const languages = ['en', 'ja', 'ko', 'zh-CN', 'zh-TW']
// admin 仅维护 zh-CN（后台保持纯中文，其他语言缺键时由 fallbackLocale 回落中文）
const types = ['general', 'account', 'home', 'policy', 'admin']

mkdirSync(outDir, { recursive: true })

// policy JSON 不带模块前缀（其 key 本身业务化）；general 直通
const noPrefix = new Set(['general', 'policy'])

for (const lang of languages) {
  const merged = {}
  for (const type of types) {
    const file = join(srcDir, type, `${lang}.json`)
    let data = {}
    try {
      data = JSON.parse(readFileSync(file, 'utf-8'))
    } catch (e) {
      console.warn(`skip ${file}: ${e.message}`)
      continue
    }
    const prefix = noPrefix.has(type) ? '' : type + '.'
    for (const [k, v] of Object.entries(data)) {
      // 合并冲突时后者覆盖，输出警告便于检查
      if (merged[prefix + k] !== undefined) console.warn(`[${lang}] key collision: ${prefix + k}`)
      merged[prefix + k] = v
    }
  }
  writeFileSync(join(outDir, `${lang}.json`), JSON.stringify(merged, null, 2) + '\n')
  console.log(`written ${lang}: ${Object.keys(merged).length} keys`)
}