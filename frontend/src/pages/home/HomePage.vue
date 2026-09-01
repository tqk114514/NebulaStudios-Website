<script setup lang="ts">
// 首页（迁移自 modules/home/pages/index.html + home.ts）。
// 保留自定义光标、星云环、ticker 无限滚动、reveal 滚动动画。
import { ref, onMounted, onBeforeUnmount } from 'vue'

const isMobile = /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent)

// ---- 自定义光标 ----
const cursor = ref<HTMLDivElement | null>(null)
const ring = ref<HTMLDivElement | null>(null)
let mouseX = 0
let mouseY = 0
let ringX = 0
let ringY = 0
let cursorAnimId: number | null = null
const cursorVisible = ref(false)
let cursorHooks = false

function animateRing() {
  ringX += (mouseX - ringX) * 0.12
  ringY += (mouseY - ringY) * 0.12
  if (ring.value) {
    ring.value.style.left = `${ringX}px`
    ring.value.style.top = `${ringY}px`
  }
  cursorAnimId = requestAnimationFrame(animateRing)
}

function setupCursor() {
  if (isMobile) return
  document.body.classList.add('has-custom-cursor')
  document.addEventListener('mousemove', (e) => {
    mouseX = e.clientX
    mouseY = e.clientY
    if (cursor.value) {
      cursor.value.style.left = `${mouseX}px`
      cursor.value.style.top = `${mouseY}px`
      if (!cursorVisible.value) {
        cursorVisible.value = true
        cursor.value.classList.add('visible')
        ring.value?.classList.add('visible')
      }
    }
  })
  cursorAnimId = requestAnimationFrame(animateRing)
  cursorHooks = true
}

// ---- 星云环动画 ----
const ringAngles = [0, 0, 0, 0]
const ringSpeeds = [45, -18, 9, -6]
let nebulaAnimId: number | null = null
let lastNebulaTime = 0

function nebulaStep(ts: number) {
  if (lastNebulaTime === 0) lastNebulaTime = ts
  const delta = (ts - lastNebulaTime) / 1000
  lastNebulaTime = ts
  const els = document.querySelectorAll<HTMLElement>('.nebula-ring')
  els.forEach((ringEl, i) => {
    ringAngles[i] += ringSpeeds[i] * delta
    ringEl.style.transform = `translate(-50%, -50%) rotate(${ringAngles[i]}deg)`
  })
  nebulaAnimId = requestAnimationFrame(nebulaStep)
}

// ---- Ticker 无限滚动 ----
let tickerAnimId: number | null = null
let tickerX = 0
let tickerTrackW = 0
let tickerRunner: HTMLElement | null = null
const tickerSpeed = 0.6

function tickerStep() {
  tickerX -= tickerSpeed
  if (tickerX <= -tickerTrackW) tickerX += tickerTrackW
  if (tickerRunner) tickerRunner.style.transform = `translateX(${tickerX}px)`
  tickerAnimId = requestAnimationFrame(tickerStep)
}

function initTicker() {
  const inner = document.getElementById('ticker-inner')
  const seed = inner?.querySelector<HTMLElement>('.ticker-item')
  if (!inner || !seed) return
  while (inner.scrollWidth < window.innerWidth + seed.offsetWidth * 2) {
    inner.appendChild(seed.cloneNode(true))
  }
  tickerTrackW = inner.offsetWidth
  const clone = inner.cloneNode(true) as HTMLElement
  clone.removeAttribute('id')
  const tickerEl = document.getElementById('ticker')
  tickerRunner = document.createElement('div')
  tickerRunner.style.cssText = 'display:inline-flex;will-change:transform;'
  tickerEl?.appendChild(tickerRunner)
  tickerRunner.appendChild(inner)
  tickerRunner.appendChild(clone)
  tickerAnimId = requestAnimationFrame(tickerStep)
}

// ---- Reveal 滚动动画 ----
function initReveal() {
  const els = document.querySelectorAll<HTMLElement>('.reveal')
  const obs = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add('visible')
          obs.unobserve(entry.target)
        }
      })
    },
    { threshold: 0.12 }
  )
  els.forEach((el) => obs.observe(el))
}

onMounted(() => {
  setupCursor()
  if (!isMobile) {
    nebulaAnimId = requestAnimationFrame(nebulaStep)
  }
  initTicker()
  initReveal()
})

onBeforeUnmount(() => {
  if (cursorAnimId !== null) cancelAnimationFrame(cursorAnimId)
  if (nebulaAnimId !== null) cancelAnimationFrame(nebulaAnimId)
  if (tickerAnimId !== null) cancelAnimationFrame(tickerAnimId)
  if (cursorHooks) window.removeEventListener('mousemove', () => {})
})
</script>

<template>
  <div class="home-root" :class="{ 'no-custom-cursor': isMobile }">
    <div v-if="!isMobile" ref="cursor" class="cursor" :class="{ visible: cursorVisible }"></div>
    <div v-if="!isMobile" ref="ring" class="cursor-ring"></div>

    <!-- Hero -->
    <section class="hero">
      <div class="hero-left">
        <h1 class="hero-title" translate="no">ONE<br />IDENTITY<br /><em>Everywhere</em></h1>
        <p class="hero-desc">{{ $t('home.hero.description') }}</p>
        <div class="hero-cta">
          <RouterLink class="cta-btn cta-btn--primary" to="/account/register">{{ $t('home.hero.createAccount') }}</RouterLink>
          <RouterLink class="cta-btn cta-btn--secondary" to="/account/login">{{ $t('home.hero.login') }}</RouterLink>
        </div>
      </div>
      <div class="hero-right">
        <div class="nebula-graphic">
          <div v-for="i in 4" :key="i" class="nebula-ring"></div>
          <div class="nebula-dot"></div>
          <svg class="nebula-stars" viewBox="0 0 360 360">
            <circle cx="60" cy="80" r="1" fill="#f0ede8" opacity="0.5" />
            <circle cx="300" cy="120" r="1.5" fill="#f0ede8" opacity="0.35" />
            <circle cx="40" cy="280" r="1" fill="#f0ede8" opacity="0.4" />
            <circle cx="320" cy="300" r="1" fill="#f0ede8" opacity="0.3" />
            <circle cx="180" cy="30" r="1" fill="#f0ede8" opacity="0.6" />
            <circle cx="100" cy="340" r="1.5" fill="#f0ede8" opacity="0.25" />
            <circle cx="280" cy="60" r="1" fill="#f0ede8" opacity="0.45" />
            <circle cx="200" cy="310" r="1" fill="#f0ede8" opacity="0.3" />
            <circle cx="340" cy="200" r="1.5" fill="#f0ede8" opacity="0.35" />
            <circle cx="20" cy="160" r="1" fill="#f0ede8" opacity="0.4" />
          </svg>
        </div>
        <div class="hero-stats">
          <div class="stat-item">
            <span class="stat-num" translate="no">99.9%</span>
            <span class="stat-label">{{ $t('home.stats.availability') }}</span>
          </div>
          <div class="stat-item">
            <span class="stat-num" translate="no">&lt; 80ms</span>
            <span class="stat-label">{{ $t('home.stats.authResponse') }}</span>
          </div>
        </div>
      </div>
    </section>

    <!-- Ticker -->
    <div class="ticker" id="ticker">
      <div id="ticker-inner"><span class="ticker-item" translate="no">NEBULA</span></div>
    </div>

    <!-- Features -->
    <section class="features">
      <div class="section-header">
        <div class="reveal">
          <p class="section-label">{{ $t('home.features.label') }}</p>
          <h2 class="section-title" translate="no">Everything<br />You <em>Need</em></h2>
        </div>
        <p class="section-body reveal reveal-delay-2">{{ $t('home.features.description') }}</p>
      </div>

      <div class="feature-grid">
        <div v-for="(f, i) in [
          { n: '01', key: 'identitySecurity' },
          { n: '02', key: 'profile' },
          { n: '03', key: 'thirdParty' },
          { n: '04', key: 'activityLog' },
          { n: '05', key: 'oauth' },
          { n: '06', key: 'dataExport' },
        ]" :key="f.key" class="feature-card reveal" :class="`reveal-delay-${i % 3}`">
          <p class="feature-number" translate="no">{{ f.n }}</p>
          <h3 class="feature-name">{{ $t(`home.feature.${f.key}.name`) }}</h3>
          <p class="feature-desc">{{ $t(`home.feature.${f.key}.desc`) }}</p>
        </div>
      </div>
    </section>

    <!-- Manifesto -->
    <section class="manifesto">
      <div class="manifesto-left reveal">
        <div class="manifesto-quote" translate="no">YOUR<br /><span class="accent">DATA</span><br />YOUR<br />RULES</div>
      </div>
      <div class="manifesto-right">
        <p class="manifesto-text reveal">
          <span>{{ $t('home.manifesto.text1.before') }}</span>
          <strong>{{ $t('home.manifesto.text1.strong') }}</strong>
          <span>{{ $t('home.manifesto.text1.after') }}</span>
        </p>
        <p class="manifesto-text reveal reveal-delay-1">
          <span>{{ $t('home.manifesto.text2.before') }}</span>
          <strong>{{ $t('home.manifesto.text2.strong') }}</strong>
          <span>{{ $t('home.manifesto.text2.after') }}</span>
        </p>
        <p class="manifesto-text reveal reveal-delay-2">
          <span>{{ $t('home.manifesto.text3.part1') }}</span>
          <strong>{{ $t('home.manifesto.text3.strong1') }}</strong>
          <span>{{ $t('home.manifesto.text3.part2') }}</span>
          <strong>{{ $t('home.manifesto.text3.strong2') }}</strong>
          <span>{{ $t('home.manifesto.text3.part3') }}</span>
        </p>
      </div>
    </section>

    <!-- CTA -->
    <section class="cta-section">
      <div class="cta-title reveal" translate="no">Start<br /><em>Today</em><br />Free</div>
      <div class="cta-right reveal reveal-delay-2">
        <p class="cta-sub">{{ $t('home.cta.description') }}</p>
        <div class="cta-actions">
          <RouterLink class="cta-btn cta-btn--primary" to="/account/register">{{ $t('home.cta.registerNow') }}</RouterLink>
          <RouterLink class="cta-btn cta-btn--secondary" to="/account/login">{{ $t('home.cta.alreadyHaveAccount') }}</RouterLink>
        </div>
      </div>
    </section>

    <footer class="home-footer">
      <span class="footer-logo" translate="no">Nebula Studios</span>
      <div class="footer-links">
        <RouterLink class="footer-link" to="/policy#privacy">{{ $t('policy.privacyPolicy') }}</RouterLink>
        <RouterLink class="footer-link" to="/policy#terms">{{ $t('policy.termsOfService') }}</RouterLink>
        <RouterLink class="footer-link" to="/policy#cookies">{{ $t('policy.cookiePolicy') }}</RouterLink>
      </div>
      <div class="page-footer" translate="no">{{ $t('footer.copyright', { year: new Date().getFullYear() }) }}</div>
    </footer>
  </div>
</template>

<style scoped>
/* 主页专用样式（迁移自 modules/home/assets/css/home.css，scoped 隔离同名类）。
   原站的全局副作用选择器（body::before 噪点、html/body 滚动、自定义光标、.reveal 隐藏态）
   已限定到 .home-root 内，避免污染其它页面。 */

/* ---- 自定义光标：隐藏系统光标（仅在 home 页子树内生效） ---- */
body.has-custom-cursor .home-root,
body.has-custom-cursor .home-root * {
  cursor: none !important;
}
/* 移动端恢复默认光标（根节点带 .no-custom-cursor 时） */
.home-root.no-custom-cursor,
.home-root.no-custom-cursor * {
  cursor: auto !important;
}

.home-root {
  scroll-behavior: smooth;
  overflow-x: hidden;
}

/* ---- 噪点覆盖层（限定到 home-root） ---- */
.home-root::before {
  content: '';
  position: fixed;
  inset: 0;
  background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)' opacity='1'/%3E%3C/svg%3E");
  opacity: 0.025;
  pointer-events: none;
  z-index: 1000;
}

/* ---- 自定义光标 ---- */
.cursor {
  position: fixed;
  width: 6px;
  height: 6px;
  background: var(--fg);
  border-radius: 50%;
  pointer-events: none;
  z-index: 9999;
  transform: translate(-50%, -50%);
  transition: transform 0.1s, width 0.3s var(--ease), height 0.3s var(--ease), opacity 0.3s;
  mix-blend-mode: difference;
  opacity: 0;
}
.cursor.visible {
  opacity: 1;
}
.cursor-ring {
  position: fixed;
  width: 32px;
  height: 32px;
  border: 1px solid rgba(240, 237, 232, 0.3);
  border-radius: 50%;
  pointer-events: none;
  z-index: 9998;
  transform: translate(-50%, -50%);
  transition: transform 0.18s var(--ease), width 0.4s var(--ease), height 0.4s var(--ease), border-color 0.3s;
  opacity: 0;
}
.cursor-ring.visible {
  opacity: 1;
}
.home-root:has(a:hover) .cursor-ring,
.home-root:has(button:hover) .cursor-ring {
  width: 48px;
  height: 48px;
  border-color: rgba(240, 237, 232, 0.6);
}

/* ---- Hero ---- */
.hero {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 1fr 1fr;
  position: relative;
  overflow: hidden;
  padding-top: 60px;
}
.hero::before {
  content: '';
  position: absolute;
  left: 50%;
  top: 60px;
  bottom: 0;
  width: 1px;
  background: var(--line);
}
.hero-left {
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  padding: 120px 64px 80px 80px;
  position: relative;
}
.hero-eyebrow {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--mid);
  margin-bottom: 32px;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.2s forwards;
}
.hero-eyebrow span {
  display: inline-block;
  width: 24px;
  height: 1px;
  background: var(--mid);
  vertical-align: middle;
  margin-right: 12px;
}
.hero-title {
  font-family: var(--font-display);
  font-size: clamp(48px, 6vw, 88px);
  font-weight: 900;
  line-height: 0.92;
  letter-spacing: -0.02em;
  text-transform: uppercase;
  color: var(--fg);
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.4s forwards;
}
.hero-title em {
  font-style: italic;
  font-family: var(--font-serif);
  font-weight: 300;
  display: block;
  font-size: 0.85em;
  letter-spacing: 0;
}
.hero-desc {
  margin-top: 40px;
  font-family: var(--font-serif);
  font-size: 16px;
  font-style: italic;
  color: var(--mid);
  max-width: 340px;
  line-height: 1.7;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.6s forwards;
}
.hero-cta {
  margin-top: 56px;
  display: flex;
  gap: 16px;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 0.8s forwards;
}
.hero-cta .cta-btn,
.cta-actions .cta-btn {
  width: auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 48px;
  padding: 0 20px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s;
}
.hero-cta .cta-btn--primary,
.cta-actions .cta-btn--primary {
  background: var(--fg);
  color: var(--bg);
  font-family: var(--font-display);
  font-weight: 700;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.hero-cta .cta-btn--primary:hover,
.cta-actions .cta-btn--primary:hover {
  background: var(--fg-hover);
}
.hero-cta .cta-btn--secondary,
.cta-actions .cta-btn--secondary {
  background: transparent;
  border: 1px solid var(--dim);
  color: var(--fg);
  font-family: var(--font-mono);
  font-weight: 300;
  font-size: var(--text-base);
  letter-spacing: 0.08em;
}
.hero-cta .cta-btn--secondary:hover,
.cta-actions .cta-btn--secondary:hover {
  border-color: var(--mid);
  background: var(--dim);
}
.hero-right {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-end;
  padding: 120px 80px 80px 64px;
  position: relative;
}

/* ---- 星云图形 ---- */
.nebula-graphic {
  width: 360px;
  height: 360px;
  position: relative;
  opacity: 0;
  animation: fadeIn 1.5s var(--ease) 0.6s forwards;
}
.nebula-ring {
  position: absolute;
  border-radius: 50%;
  border: 1px solid;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}
.nebula-ring:nth-child(1) {
  width: 80px;
  height: 80px;
  border-color: rgba(240, 237, 232, 0.6);
}
.nebula-ring:nth-child(2) {
  width: 160px;
  height: 160px;
  border-color: rgba(240, 237, 232, 0.15);
  border-style: dashed;
}
.nebula-ring:nth-child(3) {
  width: 260px;
  height: 260px;
  border-color: rgba(240, 237, 232, 0.06);
}
.nebula-ring:nth-child(4) {
  width: 340px;
  height: 340px;
  border-color: rgba(240, 237, 232, 0.03);
}
.nebula-dot {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 4px;
  height: 4px;
  background: var(--fg);
  border-radius: 50%;
  transform: translate(-50%, -50%);
}
.nebula-dot::before,
.nebula-dot::after {
  content: '';
  position: absolute;
  background: var(--fg);
  border-radius: 50%;
}
.nebula-dot::before {
  width: 2px;
  height: 2px;
  top: -60px;
  left: -1px;
  opacity: 0.4;
}
.nebula-dot::after {
  width: 2px;
  height: 2px;
  top: 120px;
  left: 58px;
  opacity: 0.25;
}
.nebula-stars {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  overflow: visible;
}

/* ---- Hero 统计数据 ---- */
.hero-stats {
  position: absolute;
  bottom: 80px;
  right: 80px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  align-items: flex-end;
  opacity: 0;
  animation: fadeUp 1s var(--ease) 1s forwards;
}
.stat-item {
  text-align: right;
}
.stat-num {
  font-family: var(--font-display);
  font-size: 28px;
  font-weight: 700;
  color: var(--fg);
  letter-spacing: -0.02em;
  display: block;
}
.stat-label {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.15em;
  color: var(--mid);
  text-transform: uppercase;
}

/* ---- 滚动 Ticker ---- */
.ticker {
  border-top: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
  overflow: hidden;
  white-space: nowrap;
  padding: 12px 0;
  background: var(--bg);
  position: relative;
  z-index: 2;
}
.ticker-inner {
  display: inline-flex;
  gap: 0;
  will-change: transform;
}
.ticker-item {
  font-family: var(--font-display);
  font-size: var(--text-sm);
  font-weight: 700;
  letter-spacing: 0.35em;
  text-transform: uppercase;
  color: var(--mid);
  padding: 0 48px;
  white-space: nowrap;
}

/* ---- 功能特性 ---- */
.features {
  padding: 120px 80px;
  border-top: 1px solid var(--line);
}
.section-header {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 64px;
  margin-bottom: 80px;
  align-items: end;
}
.section-label {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: var(--mid);
  margin-bottom: 20px;
}
.section-label::before {
  content: '//';
  margin-right: 8px;
  opacity: 0.5;
}
.section-title {
  font-family: var(--font-display);
  font-size: clamp(28px, 3.5vw, 48px);
  font-weight: 700;
  line-height: 1.05;
  letter-spacing: -0.02em;
  text-transform: uppercase;
  color: var(--fg);
}
.section-title em {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 300;
}
.section-body {
  font-family: var(--font-serif);
  font-size: 15px;
  font-style: italic;
  color: var(--mid);
  line-height: 1.8;
  max-width: 380px;
  align-self: end;
}
.feature-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  border-top: 1px solid var(--line);
}
.feature-card {
  padding: 48px 40px;
  border-right: 1px solid var(--line);
  border-bottom: 1px solid var(--line);
  position: relative;
  overflow: hidden;
  transition: background 0.5s var(--ease);
}
.feature-card:nth-child(3n) {
  border-right: none;
}
.feature-card:nth-last-child(-n+3) {
  border-bottom: none;
}
.feature-card::before {
  content: '';
  position: absolute;
  inset: 0;
  background: var(--line);
  transform: scaleY(0);
  transform-origin: bottom;
  transition: transform 0.5s var(--ease);
}
.feature-card:hover::before {
  transform: scaleY(1);
}
.feature-number {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  color: var(--mid);
  margin-bottom: 32px;
  position: relative;
}
.feature-icon {
  width: 36px;
  height: 36px;
  margin-bottom: 24px;
  position: relative;
  opacity: 0.7;
}
.feature-icon svg {
  width: 100%;
  height: 100%;
  stroke: var(--fg);
  fill: none;
  stroke-width: 1;
  stroke-linecap: square;
}
.feature-name {
  font-family: var(--font-display);
  font-size: var(--text-md);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg);
  margin-bottom: 12px;
  position: relative;
}
.feature-desc {
  font-family: var(--font-serif);
  font-size: 13px;
  font-style: italic;
  color: var(--mid);
  line-height: 1.7;
  position: relative;
}

/* ---- 宣言区域 ---- */
.manifesto {
  padding: 140px 80px;
  border-top: 1px solid var(--line);
  display: grid;
  grid-template-columns: 1fr 2fr;
  gap: 80px;
  align-items: start;
}
.manifesto-left {
  position: sticky;
  top: 80px;
}
.manifesto-quote {
  font-family: var(--font-display);
  font-size: clamp(36px, 5vw, 72px);
  font-weight: 900;
  line-height: 0.95;
  text-transform: uppercase;
  letter-spacing: -0.02em;
  color: var(--fg);
}
.manifesto-quote .accent {
  color: var(--mid);
}
.manifesto-right {
  padding-top: 8px;
}
.manifesto-text {
  font-family: var(--font-serif);
  font-size: 18px;
  font-style: italic;
  color: var(--mid);
  line-height: 1.9;
  margin-bottom: 32px;
}
.manifesto-text strong {
  color: var(--fg);
  font-style: normal;
  font-weight: 400;
}

/* ---- CTA 区域 ---- */
.cta-section {
  border-top: 1px solid var(--line);
  padding: 120px 80px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 80px;
  align-items: center;
}
.cta-title {
  font-family: var(--font-display);
  font-size: clamp(32px, 4vw, 60px);
  font-weight: 900;
  line-height: 0.95;
  text-transform: uppercase;
  letter-spacing: -0.02em;
  color: var(--fg);
}
.cta-title em {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 300;
  display: block;
  font-size: 0.8em;
}
.cta-right {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
.cta-sub {
  font-family: var(--font-serif);
  font-style: italic;
  color: var(--mid);
  font-size: 15px;
  line-height: 1.7;
}
.cta-actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

/* ---- 页脚 ---- */
.home-footer {
  border-top: 1px solid var(--line);
  padding: 32px 80px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.footer-logo {
  font-family: var(--font-display);
  font-size: var(--text-xs);
  letter-spacing: 0.15em;
  text-transform: uppercase;
  color: var(--mid);
}
.footer-copy {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  color: var(--mid);
}
.footer-links {
  display: flex;
  gap: 24px;
}
.footer-link {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--mid);
  transition: color 0.3s;
}
.footer-link:hover {
  color: var(--fg);
}

/* ---- 动画 ---- */
@keyframes fadeUp {
  from {
    opacity: 0;
    transform: translateY(24px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
@keyframes fadeIn {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

/* ---- 滚动显示动画（限 home-root，保留 reveal 滚动显示） ---- */
.reveal {
  transition: opacity 0.9s var(--ease), transform 0.9s var(--ease);
  opacity: 0;
  transform: translateY(32px);
}
.reveal.visible {
  opacity: 1;
  transform: none;
}
.reveal-delay-1 {
  transition-delay: 0.1s;
}
.reveal-delay-2 {
  transition-delay: 0.2s;
}
.reveal-delay-3 {
  transition-delay: 0.3s;
}
.reveal-delay-4 {
  transition-delay: 0.4s;
}
.reveal-delay-5 {
  transition-delay: 0.5s;
}

/* ---- 响应式 ---- */
@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
    min-height: auto;
  }
  .hero::before {
    display: none;
  }
  .hero-left {
    padding: 100px 32px 48px;
  }
  .hero-right {
    display: none;
  }
  .features {
    padding: 80px 32px;
  }
  .section-header {
    grid-template-columns: 1fr;
    gap: 24px;
  }
  .feature-grid {
    grid-template-columns: 1fr;
  }
  .feature-card {
    border-right: none;
  }
  .feature-card:nth-last-child(-n+3) {
    border-bottom: 1px solid var(--line);
  }
  .feature-card:last-child {
    border-bottom: none;
  }
  .manifesto {
    grid-template-columns: 1fr;
    padding: 80px 32px;
  }
  .manifesto-left {
    position: static;
  }
  .cta-section {
    grid-template-columns: 1fr;
    padding: 80px 32px;
  }
  .home-footer {
    padding: 24px 32px;
    flex-direction: column;
    gap: 16px;
    text-align: center;
  }
  .footer-links {
    justify-content: center;
  }
}
</style>