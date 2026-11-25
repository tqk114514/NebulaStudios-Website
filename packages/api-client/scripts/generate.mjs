#!/usr/bin/env node
// 从 Rust 侧的 `ApiDoc` 生成 OpenAPI 快照与 TypeScript 类型。
// 生成物入库，CI 比对由 `git diff --exit-code` 之外的门禁覆盖；本地改动后运行
// `npm run codegen:api` 重新生成。
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const packageDir = dirname(here)
const repoRoot = dirname(dirname(packageDir))
const specPath = join(packageDir, 'openapi.json')
const outputPath = join(packageDir, 'src', 'schema.d.ts')

const cargo = process.platform === 'win32' ? 'cargo.exe' : 'cargo'
const spec = execFileSync(cargo, ['run', '--quiet', '--bin', 'openapi'], {
  cwd: repoRoot,
  encoding: 'utf8',
  maxBuffer: 16 * 1024 * 1024,
})
// 先解析一次：Rust 输出被日志污染时立即失败，不写坏快照
JSON.parse(spec)
writeFileSync(specPath, spec)
mkdirSync(dirname(outputPath), { recursive: true })

const cli = join(repoRoot, 'node_modules', 'openapi-typescript', 'bin', 'cli.js')
if (!existsSync(cli)) {
  throw new Error(`找不到 openapi-typescript，先执行 npm install：${cli}`)
}
execFileSync(process.execPath, [cli, specPath, '--output', outputPath], {
  cwd: repoRoot,
  stdio: 'inherit',
})
