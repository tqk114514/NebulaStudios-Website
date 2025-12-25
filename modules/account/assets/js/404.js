/**
 * assets/js/404.js
 * 404 页面逻辑
 * 
 * 功能�?
 * - 显示 404 错误页面
 * - 支持多语言
 * - 卡片高度自适应
 */

// ==================== 模块导入 ====================
import { initLanguageSwitcher, loadLanguageSwitcher, updatePageTitle, hidePageLoader, waitForTranslations } from '../../../../shared/js/utils/language-switcher.js';
import { adjustCardHeight, delayedExecution, enableCardAutoResize } from './lib/helpers.js';

// ==================== 页面初始�?====================

document.addEventListener('DOMContentLoaded', async () => {
  try {
    // 等待翻译加载完成
    await waitForTranslations();
    await loadLanguageSwitcher();
    
    // 隐藏页面加载遮罩
    hidePageLoader();
    
    // 获取卡片元素
    const card = document.querySelector('.card');
    
    // 初始化语言切换�?
    initLanguageSwitcher(() => {
      updatePageTitle();
      if (card) delayedExecution(() => adjustCardHeight(card));
    });
    
    // 启用卡片自动调整大小
    if (card) enableCardAutoResize(card);
    
    // 更新页面标题
    updatePageTitle();
    
    // 初始调整卡片高度
    if (card) delayedExecution(() => adjustCardHeight(card));
    
    // 返回按钮
    const backBtn = document.getElementById('back-btn');
    if (backBtn) {
      backBtn.addEventListener('click', () => {
        window.location.href = '/account/login';
      });
    }
    
  } catch (error) {
    console.error('[404] ERROR: Page initialization failed:', error.message);
    hidePageLoader();
  }
});
