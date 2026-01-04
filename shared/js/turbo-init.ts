/**
 * Turbo Drive 初始化
 * 实现无刷新页面切换
 * 
 * 使用 CDN 加载 Turbo，避免打包体积过大
 */

// 声明全局 Turbo 类型
declare const Turbo: {
  start: () => void;
  visit: (url: string, options?: { action?: string }) => void;
};

// 页面切换时的过渡动画
document.addEventListener('turbo:before-render', () => {
  document.body.classList.add('turbo-exit');
});

document.addEventListener('turbo:render', () => {
  document.body.classList.remove('turbo-exit');
  document.body.classList.add('turbo-enter');
  
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      document.body.classList.remove('turbo-enter');
    });
  });
});

// 页面加载进度条
document.addEventListener('turbo:before-fetch-request', () => {
  const loader = document.getElementById('page-loader');
  if (loader) loader.classList.remove('is-hidden');
});

document.addEventListener('turbo:before-render', () => {
  const loader = document.getElementById('page-loader');
  if (loader) loader.classList.add('is-hidden');
});

// 表单提交完成
document.addEventListener('turbo:submit-end', () => {
  console.log('[Turbo] Form submitted');
});

// 缓存前清理页面状态
document.addEventListener('turbo:before-cache', () => {
  document.querySelectorAll('.modal-overlay:not(.is-hidden)').forEach(modal => {
    modal.classList.add('is-hidden');
  });
});

console.log('[Turbo] Initialized');
