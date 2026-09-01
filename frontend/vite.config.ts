import { fileURLToPath, URL } from 'node:url'
import { cpSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { brotliCompressSync } from 'node:zlib'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import VueI18nPlugin from '@intlify/unplugin-vue-i18n/vite'

/**
 * 构建期配置。独立于服务端运行，无法读取后端 .env，
 * 故在此集中维护构建期常量（对应旧前端 cmd/build/config.go 的 cdnURL）。
 * 修改 CDN 域名时只需改这一处；须与后端 .env 的 CDN_URL 保持一致（后者用于运行时 CSP 注入）。
 */
const CDN_URL = 'https://fast-cdn01.nebulastudios.top'

/** 替换 index.html 中的 {{CDN_URL}} 占位符（对应旧构建工具的 replaceCDNURL）。 */
function cdnUrlReplace(): Plugin {
  return {
    name: 'cdn-url-replace',
    transformIndexHtml(html) {
      return html.replaceAll('{{CDN_URL}}', CDN_URL)
    },
  }
}

/**
 * 构建后把后端运行时依赖的数据文件复制进 dist：
 * - 根 data/（email-template.html、email-texts.json）→ dist/data/
 * - frontend/policy/（政策 Markdown + manifest.json）→ dist/policy/
 * 这些是服务端运行时读取/静态服务的文件，必须随 dist 一起部署。
 */
function copyBackendData(): Plugin {
  return {
    name: 'copy-backend-data',
    apply: 'build',
    closeBundle() {
      cpSync(new URL('../data', import.meta.url), new URL('../dist/data', import.meta.url), {
        recursive: true,
        filter: (src) => !/[/\\]avatars([/\\]|$)/.test(src), // data/avatars 是运行时本地存储，不复制
      })
      // 政策正文 + manifest（frontend/policy/ 下只有 md 与 manifest.json）
      cpSync(new URL('./policy', import.meta.url), new URL('../dist/policy', import.meta.url), {
        recursive: true,
      })
      console.log('[vite:copy-backend-data] copied data/ -> dist/, policy -> dist/policy/')
    },
  }
}

/**
 * Brotli 预压缩（对应旧构建工具 cmd/build 的预压缩步骤）。
 * 服务端 PreCompressedStatic 中间件优先服务 .br 副本，浏览器不支持时回退原文件。
 * 覆盖 dist/assets（JS/CSS/字体）与 dist/policy（政策 Markdown，多级子目录）。
 */
function brotliPrecompress(): Plugin {
  const compressibleExt = new Set(['.js', '.css', '.json', '.svg', '.md', '.woff', '.woff2'])

  function compressDir(dir: string): number {
    let count = 0
    for (const name of readdirSync(dir)) {
      const fullPath = join(dir, name)
      if (statSync(fullPath).isDirectory()) {
        count += compressDir(fullPath)
        continue
      }
      const i = name.lastIndexOf('.')
      if (i === -1 || !compressibleExt.has(name.slice(i))) continue
      writeFileSync(fullPath + '.br', brotliCompressSync(readFileSync(fullPath)))
      count++
    }
    return count
  }

  return {
    name: 'brotli-precompress',
    apply: 'build',
    closeBundle() {
      // copyBackendData 与本插件同为 closeBundle，此插件注册在后，保证政策 md 已复制完成
      const distRoot = fileURLToPath(new URL('../dist', import.meta.url))
      const count = compressDir(join(distRoot, 'assets')) + compressDir(join(distRoot, 'policy'))
      console.log(`[vite:brotli-precompress] generated ${count} .br files`)
    },
  }
}

export default defineConfig({
  plugins: [
    vue(),
    // i18n 消息构建期预编译：服务端 CSP 禁 unsafe-eval，vue-i18n 默认运行时编译消息
    // 内部用 new Function 会被拦截（页面白屏）。预编译后产物走 runtime-only，无 eval。
    VueI18nPlugin({
      include: [resolve(fileURLToPath(new URL('.', import.meta.url)), 'src/i18n/locales/**')],
    }),
    cdnUrlReplace(),
    copyBackendData(),
    brotliPrecompress(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    // 输出到项目根 dist（部署时 server + dist 同目录）：dist/index.html + dist/assets/* + dist/data + dist/policy
    outDir: '../dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:3000', changeOrigin: true },
      '/oauth': { target: 'http://localhost:3000', changeOrigin: true },
    },
  },
})
