/**
 * modules/home/assets/js/home.ts
 * 主页交互逻辑
 *
 * 功能：
 * - 自定义光标（页面可见时运行）
 * - 滚动显示动画
 * - Ticker 无限滚动（可见时运行）
 * - 语言切换
 */

import { initLanguageSwitcher, waitForTranslations, updatePageTitle } from '../../../../shared/js/utils/language-switcher.ts';

// ==================== 页面可见性管理 ====================

let pageVisible = true;

document.addEventListener('visibilitychange', () => {
  pageVisible = !document.hidden;
  if (pageVisible) {
    startCursorAnimation();
    startTickerAnimation();
  } else {
    stopCursorAnimation();
    stopTickerAnimation();
  }
});

// ==================== 自定义光标 ====================

const cursor = document.getElementById('cursor');
const ring = document.getElementById('cursor-ring');
let mouseX = 0;
let mouseY = 0;
let ringX = 0;
let ringY = 0;
let cursorAnimFrameId: number | null = null;

document.addEventListener('mousemove', (e) => {
  mouseX = e.clientX;
  mouseY = e.clientY;

  if (cursor) {
    cursor.style.left = mouseX + 'px';
    cursor.style.top = mouseY + 'px';
  }
});

function animateRing(): void {
  ringX += (mouseX - ringX) * 0.12;
  ringY += (mouseY - ringY) * 0.12;

  if (ring) {
    ring.style.left = ringX + 'px';
    ring.style.top = ringY + 'px';
  }

  cursorAnimFrameId = requestAnimationFrame(animateRing);
}

function startCursorAnimation(): void {
  if (cursorAnimFrameId === null) {
    cursorAnimFrameId = requestAnimationFrame(animateRing);
  }
}

function stopCursorAnimation(): void {
  if (cursorAnimFrameId !== null) {
    cancelAnimationFrame(cursorAnimFrameId);
    cursorAnimFrameId = null;
  }
}

startCursorAnimation();

// ==================== 滚动显示动画 ====================

const reveals = document.querySelectorAll('.reveal');

const revealObserver = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        revealObserver.unobserve(entry.target);
      }
    });
  },
  { threshold: 0.12 }
);

reveals.forEach((el) => revealObserver.observe(el));

// ==================== Ticker 无限滚动 ====================

let tickerAnimFrameId: number | null = null;
let tickerX = 0;
let tickerTrackW = 0;
let tickerRunner: HTMLElement | null = null;
const tickerSpeed = 0.6;

function tickerStep(): void {
  tickerX -= tickerSpeed;
  if (tickerX <= -tickerTrackW) tickerX += tickerTrackW;
  if (tickerRunner) {
    tickerRunner.style.transform = `translateX(${tickerX}px)`;
  }
  tickerAnimFrameId = requestAnimationFrame(tickerStep);
}

function startTickerAnimation(): void {
  if (tickerAnimFrameId === null && pageVisible) {
    tickerAnimFrameId = requestAnimationFrame(tickerStep);
  }
}

function stopTickerAnimation(): void {
  if (tickerAnimFrameId !== null) {
    cancelAnimationFrame(tickerAnimFrameId);
    tickerAnimFrameId = null;
  }
}

(function initTicker() {
  const inner = document.getElementById('ticker-inner') as HTMLElement | null;
  const seed = inner?.querySelector('.ticker-item') as HTMLElement | null;

  if (!inner || !seed) return;

  while (inner.scrollWidth < window.innerWidth + seed.offsetWidth * 2) {
    inner.appendChild(seed.cloneNode(true));
  }

  tickerTrackW = inner.offsetWidth;

  const clone = inner.cloneNode(true) as HTMLElement;
  clone.removeAttribute('id');

  const tickerEl = document.getElementById('ticker') as HTMLElement | null;
  tickerRunner = document.createElement('div');
  tickerRunner.style.cssText = 'display:inline-flex;will-change:transform;';
  tickerEl?.appendChild(tickerRunner);
  tickerRunner.appendChild(inner);
  tickerRunner.appendChild(clone);

  const tickerObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          startTickerAnimation();
        } else {
          stopTickerAnimation();
        }
      });
    },
    { threshold: 0 }
  );

  if (tickerEl) {
    tickerObserver.observe(tickerEl);
  }

  startTickerAnimation();
})();

// ==================== 语言切换初始化 ====================

waitForTranslations().then(() => {
  initLanguageSwitcher(() => {
    updatePageTitle();
  });
  updatePageTitle();
});
