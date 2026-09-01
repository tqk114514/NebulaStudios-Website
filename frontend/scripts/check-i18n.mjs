// 一次性脚本：扫描 frontend/src 所有 .vue/.ts 里用到的 i18n key，与 zh-CN locale 对比，
// 列出代码中引用但 locale 里缺失的 key。
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const srcDir = join(root, 'src')
const localePath = join(root, 'src', 'i18n', 'locales', 'zh-CN.ts')

// 读取 locale 内容，提取已定义的 key
const localeSrc = readFileSync(localePath, 'utf-8')
const definedKeys = new Set()
const re = /"([^"]+)":/g
let m
while ((m = re.exec(localeSrc)) !== null) {
  definedKeys.add(m[1])
}

// 递归收集所有可能含 i18n key 的文件
const files = []
function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) {
      walk(p)
    } else if (/\.(vue|ts)$/.test(name) && !/i18n\/locales/.test(p)) {
      files.push(p)
    }
  }
}
walk(srcDir)

// 提取字符串字面量 key：$t('...') 或 t('...') 或 `home.feature.${..}.name` 动态模板
const used = new Set()
const dynUsed = new Set()
for (const f of files) {
  const src = readFileSync(f, 'utf-8')
  // 精确匹配 $t('xxx.yyy') 或 $t("xxx.yyy")，key 形如"含点的一级或两级路径"
  const staticRe = /[$]t\(\s*['"]([a-zA-Z][a-zA-Z0-9]+(?:\.[a-zA-Z0-9_$.-]+)+)['"]/g
  let mm
  while ((mm = staticRe.exec(src)) !== null) {
    const k = mm[1]
    if (k.includes('${')) {
      dynUsed.add('dynamic: ' + k)
    } else {
      used.add(k)
    }
  }
}

const missing = [...used].filter((k) => !definedKeys.has(k)).sort()
console.log('=== 静态 key 共', used.size, '个，缺失如下 ===')
console.log(missing.length ? missing : '(无缺失)')

console.log('\n=== 动态 key 模板（${...}），需人工核对 ===')
console.log(dynUsed.size ? [...dynUsed] : '(无)')