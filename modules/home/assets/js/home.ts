/**
 * modules/home/assets/js/home.ts
 * 主页交互逻辑
 *
 * 功能：
 * - 自定义光标
 * - 滚动显示动画
 */

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
