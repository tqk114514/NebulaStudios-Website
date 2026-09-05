<script setup lang="ts">
// 政策中心页（迁移自 modules/policy/pages/policy.html + policy.ts）。
// 样式自持：政策导航、版本切换器、文档容器与 Markdown 排版全部收编在本页 scoped 中
//（v-html 注入的内容通过 :deep() 包裹选择器；值照旧来自原 policy.css / general.css）。
// hash 路由：{policy}[/{version}]，从 /api/policy/versions 获取版本元数据，
// 从 /policy-content/{type}/{lang}/{version}.md 加载正文，marked + DOMPurify 渲染。
// 支持版本切换、公示期/历史版本提示、语言回退。
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter } from 'vue-router'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useI18n } from 'vue-i18n'
import { get } from '@/api/client'
import PolicyFooter from '@/components/PolicyFooter.vue'

type PolicyType = 'privacy' | 'terms' | 'cookies'

interface PolicyVersionMeta {
  update_date: string
  effective_date: string
  languages: string[]
  status: 'effective' | 'public_notice' | 'scheduled'
}

type VersionsMap = Record<string, Record<string, PolicyVersionMeta>>

const router = useRouter()
const { locale, t } = useI18n()

const POLICIES: PolicyType[] = ['privacy', 'terms', 'cookies']

const versions = ref<VersionsMap>({})
const currentPolicy = ref<PolicyType>('privacy')
const specifiedVersion = ref<string | null>(null)
const contentHtml = ref('')
const fallbackLang = ref('')
const fallbackVersion = ref('')
const showFallback = ref(false)
const viewingPublicNotice = ref(false)
const versionOpen = ref(false)

// 版本下拉点击外部收起
const versionSwitchRef = ref<HTMLElement | null>(null)
function onDocClick(e: MouseEvent) {
  if (versionSwitchRef.value && !versionSwitchRef.value.contains(e.target as Node)) {
    versionOpen.value = false
  }
}

const LANG_NAMES: Record<string, string> = {
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  en: 'English',
  ja: '日本語',
  ko: '한국어',
}

const POLICY_NAME_KEYS: Record<PolicyType, string> = {
  privacy: 'policy.privacyPolicy',
  terms: 'policy.termsOfService',
  cookies: 'policy.cookiePolicy',
}

// 缓存：type:lang[:version] -> result
const policyCache = new Map<string, LoadResult>()

interface LoadResult {
  markdown: string | null
  isFallback: boolean
  displayLang: string
  displayVersion: string
}

// ---- 版本辅助 ----
function filenameToVersion(fn: string): string {
  return fn.replace(/\.md$/, '')
}

function getAllVersions(type: PolicyType): { version: string; meta: PolicyVersionMeta }[] {
  const v = versions.value[type]
  if (!v) return []
  return Object.entries(v)
    .filter(([, meta]) => meta.status === 'effective')
    .map(([fn, meta]) => ({ version: filenameToVersion(fn), meta }))
    .sort((a, b) => b.meta.effective_date.localeCompare(a.meta.effective_date))
}

function getLatestVersion(type: PolicyType): string {
  const all = getAllVersions(type)
  return all.length > 0 ? all[0].version : ''
}

function getPublicNoticeVersion(type: PolicyType): { version: string } | null {
  const v = versions.value[type]
  if (!v) return null
  for (const fn in v) {
    if (v[fn].status === 'public_notice') return { version: filenameToVersion(fn) }
  }
  return null
}

function getLatestEntryForLang(type: PolicyType, lang: string): { version: string; meta: PolicyVersionMeta } | null {
  const v = versions.value[type]
  if (!v) return null
  let latest: { version: string; meta: PolicyVersionMeta } | null = null
  for (const fn in v) {
    const meta = v[fn]
    if (meta.status !== 'effective' || !meta.languages.includes(lang)) continue
    if (!latest || meta.effective_date > latest.meta.effective_date) {
      latest = { version: filenameToVersion(fn), meta }
    }
  }
  return latest
}

// ---- 数据加载 ----
async function loadPolicyVersions(): Promise<void> {
  try {
    const data = await get<VersionsMap>('/api/policy/versions')
    versions.value = data
  } catch (e) {
    console.error('[POLICY] Failed to load policy versions:', e)
  }
}

async function tryLoadMd(type: PolicyType, lang: string, version: string): Promise<string | null> {
  try {
    // md 静态服务路径为 /policy-content/（服务端映射到 dist/policy）
    const res = await fetch(`/policy-content/${type}/${lang}/${version}.md`)
    if (!res.ok) return null
    return await res.text()
  } catch {
    return null
  }
}

async function loadPolicyMarkdown(type: PolicyType, specVersion?: string | null): Promise<LoadResult> {
  const lang = locale.value as string
  const cacheKey = specVersion ? `${type}:${lang}:${specVersion}` : `${type}:${lang}`
  const cached = policyCache.get(cacheKey)
  if (cached) return cached

  const typeVersions = versions.value[type]
  if (!typeVersions) return emptyResult()

  // 指定版本：按回退顺序加载该版本
  if (specVersion) {
    const meta = typeVersions[`${specVersion}.md`]
    if (!meta) return emptyResult()
    let result: LoadResult | null = null
    if (meta.languages.includes(lang)) {
      const md = await tryLoadMd(type, lang, specVersion)
      if (md) result = { markdown: md, isFallback: false, displayLang: lang, displayVersion: specVersion }
    }
    if (!result && meta.languages.includes('zh-CN')) {
      const md = await tryLoadMd(type, 'zh-CN', specVersion)
      if (md) result = { markdown: md, isFallback: true, displayLang: 'zh-CN', displayVersion: specVersion }
    }
    if (!result) {
      for (const l of meta.languages) {
        if (l === lang || l === 'zh-CN') continue
        const md = await tryLoadMd(type, l, specVersion)
        if (md) {
          result = { markdown: md, isFallback: true, displayLang: l, displayVersion: specVersion }
          break
        }
      }
    }
    if (result) {
      policyCache.set(cacheKey, result)
      return result
    }
    return emptyResult()
  }

  // 最新生效版
  const latestVersion = getLatestVersion(type)
  if (!latestVersion) return emptyResult()

  let markdown: string | null = null
  let isFallback = false
  let displayLang = ''
  let displayVersion = ''

  const curLangEntry = getLatestEntryForLang(type, lang)
  if (curLangEntry && curLangEntry.version === latestVersion) {
    markdown = await tryLoadMd(type, lang, latestVersion)
    if (markdown) {
      displayLang = lang
      displayVersion = latestVersion
    }
  }
  if (!markdown) {
    const zhEntry = getLatestEntryForLang(type, 'zh-CN')
    if (zhEntry) {
      markdown = await tryLoadMd(type, 'zh-CN', zhEntry.version)
      if (markdown) {
        isFallback = true
        displayLang = 'zh-CN'
        displayVersion = zhEntry.version
      }
    }
  }
  if (!markdown) {
    const meta = typeVersions[`${latestVersion}.md`]
    if (meta) {
      for (const l of meta.languages) {
        if (l === lang || l === 'zh-CN') continue
        const md = await tryLoadMd(type, l, latestVersion)
        if (md) {
          markdown = md
          isFallback = true
          displayLang = l
          displayVersion = latestVersion
          break
        }
      }
    }
  }

  const result: LoadResult = { markdown, isFallback, displayLang, displayVersion }
  if (markdown) policyCache.set(cacheKey, result)
  return result
}

function emptyResult(): LoadResult {
  return { markdown: null, isFallback: false, displayLang: '', displayVersion: '' }
}

// ---- 渲染 ----
function renderBannerNotice(html: string): string {
  let out = html
  // 语言回退提示（t 直接带命名参数插值；无参调用会把占位符渲染为空串）
  if (showFallback.value) {
    const policyName = t(POLICY_NAME_KEYS[currentPolicy.value]) as string
    const langName = LANG_NAMES[locale.value as string] || locale.value
    const displayLangName = LANG_NAMES[fallbackLang.value] || fallbackLang.value
    const msg = t('policy.versionFallback', {
      policy: policyName,
      version: fallbackVersion.value,
      lang: langName,
      displayLang: displayLangName,
    }) as string
    out = `<div class="notice-banner">${msg}</div>` + out
  }

  // 公示期/历史提示
  const publicNotice = viewingPublicNotice.value
  const publicVer = getPublicNoticeVersion(currentPolicy.value)
  const latestVersion = getLatestVersion(currentPolicy.value)
  if (publicNotice && publicVer) {
    out = `<div class="notice-banner">${t('policy.publicNoticePeriod')}（${publicVer.version}）<a href="#${currentPolicy.value}">${t('policy.historyLatest')}</a></div>` + out
  } else if (specifiedVersion.value && latestVersion && specifiedVersion.value !== latestVersion) {
    out = `<div class="notice-banner">${t('policy.historyNotice')}（${specifiedVersion.value}）<a href="#${currentPolicy.value}">${t('policy.historyLatest')}</a></div>` + out
  }
  return out
}

async function render(): Promise<void> {
  contentHtml.value = ''
  showFallback.value = false
  fallbackLang.value = ''
  fallbackVersion.value = ''

  const { policy, version, isPublicNotice } = parseHash()
  currentPolicy.value = policy
  specifiedVersion.value = version
  viewingPublicNotice.value = isPublicNotice

  // 公示期路由 → 解析为实际版本
  let renVersion = version
  if (isPublicNotice && version) renVersion = version

  const result = await loadPolicyMarkdown(policy, renVersion)
  if (!result.markdown) {
    contentHtml.value = '<div class="policy-not-found"><h1>404</h1></div>'
    return
  }
  showFallback.value = result.isFallback
  fallbackLang.value = result.displayLang
  fallbackVersion.value = result.displayVersion

  let html = (await marked.parse(result.markdown)) as string
  html = DOMPurify.sanitize(html)
  html = renderBannerNotice(html)
  contentHtml.value = html
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

// ---- hash 路由解析 ----
function parseHash(): { policy: PolicyType; version: string | null; isPublicNotice: boolean } {
  const hash = window.location.hash.replace(/^#\/?/, '')
  if (!hash) return { policy: 'privacy', version: null, isPublicNotice: false }
  const parts = hash.split('/')
  const policy = (POLICIES as string[]).includes(parts[0]) ? (parts[0] as PolicyType) : 'privacy'
  let version = parts[1] || null
  let isPublicNotice = false
  if (version === 'public-notice-period') {
    const pn = getPublicNoticeVersion(policy)
    isPublicNotice = true
    version = pn ? pn.version : null
    if (!pn) {
      router.replace('/policy')
    }
  }
  return { policy, version, isPublicNotice }
}

const activeVersions = computed(() => getAllVersions(currentPolicy.value))
const currentDisplayVersion = computed(() => specifiedVersion.value || (activeVersions.value[0]?.version ?? ''))

function selectVersion(version: string) {
  const target = `#${currentPolicy.value}/${version}`
  window.location.hash = target
  versionOpen.value = false
}

function navigateToPolicy(policy: PolicyType) {
  window.location.hash = `#${policy}`
}

onMounted(async () => {
  document.addEventListener('click', onDocClick)
  await loadPolicyVersions()
  await render()
  window.addEventListener('hashchange', render)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  window.removeEventListener('hashchange', render)
})

// 监听语言切换重新渲染（vue-i18n 不支持时也保留 hash 监听覆盖）
watch(locale, () => {
  render()
})
</script>

<template>
  <div>
    <nav class="policy-nav">
      <div class="policy-nav-items">
        <button
          v-for="p in POLICIES"
          :key="p"
          class="policy-nav-item"
          :class="{ active: currentPolicy === p }"
          @click="navigateToPolicy(p)"
        >
          {{ t(POLICY_NAME_KEYS[p]) }}
        </button>
      </div>

      <div
        v-if="activeVersions.length > 1"
        ref="versionSwitchRef"
        class="version-switch"
        :class="{ 'is-open': versionOpen }"
        role="listbox"
      >
        <button class="version-current" type="button" aria-haspopup="listbox" :aria-expanded="versionOpen" @click="versionOpen = !versionOpen">
          <span class="version-text">{{ currentDisplayVersion }}</span>
          <svg class="version-arrow" width="12" height="12" viewBox="0 0 12 12" fill="none">
            <path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <div class="version-dropdown" role="listbox">
          <button
            v-for="v in activeVersions"
            :key="v.version"
            type="button"
            class="version-option"
            :class="{ active: currentDisplayVersion === v.version }"
            role="option"
            :aria-selected="currentDisplayVersion === v.version"
            @click="selectVersion(v.version)"
          >
            <!-- 版本号即日期，不再重复展示生效日期 -->
            <span>{{ v.version }}</span>
            <svg class="check-icon" width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path d="M2.5 7L5.5 10L11.5 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>
      </div>
    </nav>

    <main class="policy-container">
      <div v-if="!contentHtml" class="policy-loading">
        <div class="loader-spinner"></div>
      </div>
      <div v-else class="policy-doc" v-html="contentHtml"></div>
    </main>

    <PolicyFooter :links="false" />
  </div>
</template>

<style scoped>
/* ---- 政策导航栏（迁移自 policy.css .policy-nav 系列，值不变） ---- */
.policy-nav {
  position: fixed;
  top: 60px;
  left: 0;
  right: 0;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  background: var(--bg);
  border-bottom: 1px solid var(--line);
  /* 全站层级约定：顶栏 100、政策二级导航 90、页面遮罩 110、弹窗 200+ */
  z-index: 90;
}

.policy-nav-items {
  display: flex;
  gap: 8px;
}

.policy-nav-item {
  padding: 8px 16px;
  font-family: var(--font-mono);
  font-size: var(--text-md);
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--mid);
  background: transparent;
  border: none;
  border-radius: 0;
  cursor: pointer;
  transition: color 0.2s;
}

.policy-nav-item:hover {
  color: var(--fg);
}

.policy-nav-item.active {
  color: var(--fg);
  font-weight: 700;
}

/* ---- 版本切换器（迁移自 general.css .language-switcher 系列，值不变） ---- */
.version-switch {
  position: relative;
}

.version-current {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: transparent;
  border: 1px solid var(--line);
  color: var(--fg);
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: 0.1em;
  cursor: pointer;
  transition: border-color 0.2s;
}

.version-current:hover {
  border-color: var(--mid);
}

.version-text {
  min-width: 60px;
  text-align: left;
}

.version-arrow {
  flex-shrink: 0;
  transition: transform 0.2s;
}

.version-switch.is-open .version-arrow {
  transform: rotate(180deg);
}

.version-dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  min-width: 140px;
  background: var(--bg);
  border: 1px solid var(--line);
  opacity: 0;
  visibility: hidden;
  transform: translateY(-10px);
  transition:
    opacity 0.2s,
    visibility 0.2s,
    transform 0.2s;
}

.version-switch.is-open .version-dropdown {
  opacity: 1;
  visibility: visible;
  transform: translateY(0);
}

.version-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 10px 12px;
  background: transparent;
  border: none;
  color: var(--mid);
  font-family: var(--font-mono);
  font-size: var(--text-base);
  letter-spacing: 0.1em;
  text-align: left;
  cursor: pointer;
  transition: color 0.2s, background 0.2s;
}

.version-option:hover {
  color: var(--fg);
  background: var(--dim);
}

.version-option.active {
  color: var(--fg);
}

.version-option .check-icon {
  flex-shrink: 0;
  opacity: 0;
}

.version-option.active .check-icon {
  opacity: 1;
}

/* ---- 文档容器（迁移自 policy.css .policy-container，值不变） ---- */
.policy-container {
  width: 100%;
  max-width: 800px;
  margin: 0 auto;
  padding: 140px 40px 60px;
  box-sizing: border-box;
}

/* ---- 加载状态（迁移自 policy.css .policy-loading / .loader-spinner，值不变） ---- */
.policy-loading {
  text-align: center;
  padding: 60px 20px;
  display: flex;
  justify-content: center;
  align-items: center;
}

.loader-spinner {
  width: 32px;
  height: 32px;
  border: 2px solid var(--line);
  border-top-color: var(--fg);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* ---- Markdown 排版（迁移自 policy.css .policy-container 元素样式，值不变；v-html 内容用 :deep） ---- */
.policy-doc :deep(h1) {
  font-family: var(--font-display);
  font-size: clamp(24px, 3vw, 32px);
  font-weight: 700;
  color: var(--fg);
  margin: 0 0 16px;
  letter-spacing: -0.02em;
}

.policy-doc :deep(h2) {
  font-family: var(--font-display);
  font-size: var(--text-lg);
  font-weight: 700;
  color: var(--fg);
  margin: 40px 0 16px;
}

.policy-doc :deep(h3) {
  font-family: var(--font-display);
  font-size: var(--text-md);
  font-weight: 700;
  color: var(--fg);
  margin: 24px 0 12px;
}

.policy-doc :deep(p) {
  font-size: var(--text-base);
  line-height: 1.8;
  color: var(--fg);
  margin: 0 0 16px;
}

.policy-doc :deep(p:last-child) {
  margin-bottom: 0;
}

.policy-doc :deep(ul) {
  margin: 0 0 16px;
  padding-left: 24px;
}

.policy-doc :deep(ol) {
  margin: 0 0 16px;
  padding-left: 24px;
}

.policy-doc :deep(li) {
  font-size: var(--text-base);
  line-height: 1.8;
  color: var(--fg);
  margin-bottom: 8px;
}

.policy-doc :deep(li:last-child) {
  margin-bottom: 0;
}

.policy-doc :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 16px 0;
  font-size: var(--text-sm);
}

.policy-doc :deep(th),
.policy-doc :deep(td) {
  padding: 12px;
  text-align: left;
  border: 1px solid var(--line);
}

.policy-doc :deep(th) {
  background-color: var(--dim);
  color: var(--fg);
  font-weight: 700;
}

.policy-doc :deep(td) {
  color: var(--fg);
}

.policy-doc :deep(a) {
  color: var(--fg);
  text-decoration: underline;
  text-underline-offset: 3px;
  transition: opacity 0.2s ease;
}

.policy-doc :deep(a:hover) {
  opacity: 0.8;
}

.policy-doc :deep(strong) {
  color: var(--fg);
}

.policy-doc :deep(em) {
  /* 中文没有真斜体字形，合成斜体小字渲染发虚；改为加粗强调 */
  font-style: normal;
  font-weight: 600;
  color: var(--fg);
}

.policy-doc :deep(hr) {
  border: none;
  border-top: 1px solid var(--line);
  margin: 32px 0;
}

/* ---- 提示横幅（迁移自 general.css .notice-banner，值不变；随 v-html 注入） ---- */
.policy-doc :deep(.notice-banner) {
  padding: 16px;
  margin-bottom: 24px;
  background: var(--dim);
  border: 1px solid var(--line);
  font-family: var(--font-mono);
  font-size: var(--text-md);
  letter-spacing: 0.12em;
  color: var(--fg);
}

.policy-doc :deep(.notice-banner a) {
  color: var(--fg);
  text-decoration: underline;
  text-underline-offset: 3px;
}

/* ---- 未找到（迁移自 policy.css .policy-not-found，值不变；随 v-html 注入） ---- */
.policy-doc :deep(.policy-not-found) {
  text-align: center;
  padding: 60px 20px;
}

.policy-doc :deep(.policy-not-found h1) {
  font-family: var(--font-display);
  font-size: var(--text-lg);
  color: var(--fg);
}
</style>
