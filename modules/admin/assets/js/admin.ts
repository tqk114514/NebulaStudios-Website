/**
 * modules/admin/assets/js/admin.ts
 * 管理后台入口
 *
 * 功能：
 * - 页面初始化
 * - 路由管理
 * - 侧边栏/顶栏交互
 */

import {
  getCurrentUser,
  logout,
  hideModal,
  userModal,
  confirmModal,
  confirmCancel
} from './common';
import { loadStats } from './stats';
import { loadUsers, setCurrentUserRole, initUsersPage } from './users';
import { loadLogs } from './logs';
import { loadOAuthClients, initOAuthPage } from './oauth';
import { initWhitelistPage, loadWhitelist } from './email-whitelist';

// ==================== DOM 元素 ====================

const pageLoader = document.getElementById('page-loader') as HTMLElement | null;
const sidebar = document.getElementById('sidebar') as HTMLElement | null;
const sidebarToggle = document.getElementById('sidebar-toggle') as HTMLButtonElement | null;
const navItems = document.querySelectorAll('.nav-item[data-page]') as NodeListOf<HTMLAnchorElement>;
const pageTitle = document.getElementById('page-title') as HTMLElement | null;
const currentAvatarEl = document.getElementById('current-avatar') as HTMLElement | null;
const logoutBtn = document.getElementById('logout-btn') as HTMLButtonElement | null;
const navLogs = document.getElementById('nav-logs') as HTMLAnchorElement | null;
const navOAuth = document.getElementById('nav-oauth') as HTMLAnchorElement | null;
const navWhitelist = document.getElementById('nav-whitelist') as HTMLAnchorElement | null;

// ==================== 页面路由 ====================

function navigateTo(page: string): void {
  console.log('[ADMIN] navigateTo called for page:', page);
  
  // 更新导航状态
  navItems.forEach(item => {
    item.classList.toggle('active', item.dataset.page === page);
  });

  // 更新页面显示
  const titles: Record<string, string> = {
    dashboard: '仪表盘',
    users: '用户管理',
    logs: '操作日志',
    oauth: 'OAuth 应用',
    whitelist: '邮箱白名单'
  };

  // 更新页面显示
  document.querySelectorAll('.page').forEach(p => {
    p.classList.toggle('active', p.id === `page-${page}`);
  });

  // 更新标题
  if (pageTitle) {
    const titles: Record<string, string> = {
      dashboard: '仪表盘',
      users: '用户管理',
      logs: '操作日志',
      oauth: 'OAuth 应用',
      whitelist: '邮箱白名单'
    };
    pageTitle.textContent = titles[page] || page;
  }

  // 加载页面数据
  if (page === 'dashboard') {
    loadStats();
  } else if (page === 'users') {
    loadUsers();
  } else if (page === 'logs') {
    loadLogs();
  } else if (page === 'oauth') {
    loadOAuthClients();
  } else if (page === 'whitelist') {
    initWhitelistPage();
  }
}

// ==================== 初始化 ====================

async function init(): Promise<void> {
  console.log('[ADMIN] Initializing admin page...');
  
  // 验证必需的 DOM 元素
  console.log('[ADMIN] Checking DOM elements...');
  const requiredElements = {
    pageLoader,
    sidebar,
    sidebarToggle,
    pageTitle,
    currentAvatarEl,
    logoutBtn,
    navLogs,
    navOAuth,
    navWhitelist
  };
  
  for (const [name, el] of Object.entries(requiredElements)) {
    if (!el) {
      console.error(`[ADMIN] Required element ${name} not found`);
    } else {
      console.log(`[ADMIN] Element ${name} found`);
    }
  }
  
  // 获取当前用户信息
  console.log('[ADMIN] Getting current user...');
  const user = await getCurrentUser();
  if (!user) {
    console.log('[ADMIN] No user found, redirecting to login');
    window.location.href = '/account/login';
    return;
  }
  
  console.log('[ADMIN] Current user:', user);

  setCurrentUserRole(user.role);

  // 显示日志导航（所有管理员可见）
  if (navLogs) {
    navLogs.classList.remove('is-hidden');
  }

  // 显示 OAuth 导航（超级管理员可见）
  if (user.role >= 2 && navOAuth) {
    navOAuth.classList.remove('is-hidden');
  }

  // 显示白名单导航（超级管理员可见）
  if (user.role >= 2 && navWhitelist) {
    navWhitelist.classList.remove('is-hidden');
  }

  // 显示头像
  if (currentAvatarEl) {
    currentAvatarEl.innerHTML = '';
    // 如果是 "microsoft"，用实际的微软头像 URL
    const avatarUrl = user.avatar_url === 'microsoft' ? user.microsoft_avatar_url : user.avatar_url;
    const img = document.createElement('img');
    img.src = avatarUrl || 'https://cdn01.nebulastudios.top/images/default-avatar.svg';
    img.alt = user.username;
    currentAvatarEl.appendChild(img);
  }

  // 隐藏加载器
  if (pageLoader) {
    pageLoader.classList.add('is-hidden');
  }

  console.log('[ADMIN] Initializing routes...');
  
  // 初始化路由
  const hash = window.location.hash.slice(1) || 'dashboard';
  navigateTo(hash);

  // 绑定导航事件
  navItems.forEach(item => {
    item.addEventListener('click', (e) => {
      e.preventDefault();
      const page = item.dataset.page;
      if (page) {
        window.location.hash = page;
        // 移动端点击导航后关闭侧边栏
        if (sidebar) {
          sidebar.classList.remove('is-open');
        }
      }
    });
  });

  // 绑定侧边栏切换
  if (sidebarToggle && sidebar) {
    sidebarToggle.addEventListener('click', () => {
      sidebar.classList.toggle('is-open');
    });
  }

  // 点击主内容区关闭侧边栏（移动端）
  document.querySelector('.main-content')?.addEventListener('click', () => {
    if (sidebar) {
      sidebar.classList.remove('is-open');
    }
  });

  console.log('[ADMIN] Initializing users page...');
  // 初始化用户管理页面
  initUsersPage();

  console.log('[ADMIN] Initializing OAuth page...');
  // 初始化 OAuth 管理页面
  initOAuthPage();

  // 绑定退出登录
  if (logoutBtn) {
    logoutBtn.addEventListener('click', logout);
  }

  // 绑定弹窗关闭
  if (userModal) {
    userModal.querySelector('.modal-close-btn')?.addEventListener('click', () => hideModal(userModal));
  }
  if (confirmCancel) {
    confirmCancel.addEventListener('click', () => hideModal(confirmModal));
  }

  // 点击遮罩关闭弹窗
  [userModal, confirmModal].forEach(modal => {
    if (modal) {
      modal.addEventListener('click', (e) => {
        if (e.target === modal) hideModal(modal);
      });
    }
  });

  // 监听 hash 变化
  window.addEventListener('hashchange', () => {
    const hash = window.location.hash.slice(1) || 'dashboard';
    navigateTo(hash);
  });
  
  console.log('[ADMIN] Initialization complete');
}

// 启动
init();
