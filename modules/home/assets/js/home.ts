/**
 * modules/home/assets/js/home.ts
 * 主页交互逻辑
 *
 * 功能：
 * - 自定义光标
 * - 滚动显示动画
 * - Ticker 无限滚动
 * - 语言切换
 */

import { initLanguageSwitcher, waitForTranslations, updatePageTitle } from '../../../../shared/js/utils/language-switcher.ts';

// ==================== 自定义光标 ====================

const cursor = document.getElementById('cursor');
const ring = document.getElementById('cursor-ring');
let mouseX = 0;
let mouseY = 0;
let ringX = 0;
let ringY = 0;

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

  requestAnimationFrame(animateRing);
}

animateRing();

// ==================== 滚动显示动画 ====================

const reveals = document.querySelectorAll('.reveal');

const observer = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        observer.unobserve(entry.target);
      }
    });
  },
  { threshold: 0.12 }
);

reveals.forEach((el) => observer.observe(el));

// ==================== Ticker 无限滚动 ====================

(function initTicker() {
  const inner = document.getElementById('ticker-inner') as HTMLElement | null;
  const seed = inner?.querySelector('.ticker-item') as HTMLElement | null;

  if (!inner || !seed) return;

  while (inner.scrollWidth < window.innerWidth + seed.offsetWidth * 2) {
    inner.appendChild(seed.cloneNode(true));
  }

  const trackW = inner.offsetWidth;

  const clone = inner.cloneNode(true) as HTMLElement;
  clone.removeAttribute('id');

  const tickerEl = document.getElementById('ticker') as HTMLElement | null;
  const runner = document.createElement('div');
  runner.style.cssText = 'display:inline-flex;will-change:transform;';
  tickerEl?.appendChild(runner);
  runner.appendChild(inner);
  runner.appendChild(clone);

  let x = 0;
  const speed = 0.6;

  function step() {
    x -= speed;
    if (x <= -trackW) x += trackW;
    runner.style.transform = `translateX(${x}px)`;
    requestAnimationFrame(step);
  }
  requestAnimationFrame(step);
})();

// ==================== 语言切换初始化 ====================

// 等待翻译准备就绪后初始化语言切换器
waitForTranslations().then(() => {
  initLanguageSwitcher(() => {
    updatePageTitle();
  });
  updatePageTitle();
});
