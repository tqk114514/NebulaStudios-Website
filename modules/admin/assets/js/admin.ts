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

// ==================== DOM 元素 ====================

const pageLoader = document.getElementById('page-loader') as HTMLElement;
const sidebar = document.getElementById('sidebar') as HTMLElement;
const sidebarToggle = document.getElementById('sidebar-toggle') as HTMLButtonElement;
const navItems = document.querySelectorAll('.nav-item[data-page]') as NodeListOf<HTMLAnchorElement>;
const pageTitle = document.getElementById('page-title') as HTMLElement;
const currentAvatarEl = document.getElementById('current-avatar') as HTMLElement;
const logoutBtn = document.getElementById('logout-btn') as HTMLButtonElement;

// ==================== 页面路由 ====================

function navigateTo(page: string): void {
  // 更新导航状态
  navItems.forEach(item => {
    item.classList.toggle('active', item.dataset.page === page);
  });

  // 更新页面显示
  document.querySelectorAll('.page').forEach(p => {
    p.classList.toggle('active', p.id === `page-${page}`);
  });

  // 更新标题
  const titles: Record<string, string> = {
    dashboard: '仪表盘',
    users: '用户管理'
  };
  pageTitle.textContent = titles[page] || page;

  // 加载页面数据
  if (page === 'dashboard') {
    loadStats();
  } else if (page === 'users') {
    loadUsers();
  }
}

// ==================== 初始化 ====================

async function init(): Promise<void> {
  // 获取当前用户信息
  const user = await getCurrentUser();
  if (!user) {
    window.location.href = '/account/login';
    return;
  }

  setCurrentUserRole(user.role);

  // 显示头像
  currentAvatarEl.innerHTML = '';
  // 如果是 "microsoft"，用实际的微软头像 URL
  const avatarUrl = user.avatar_url === 'microsoft' ? user.microsoft_avatar_url : user.avatar_url;
  const img = document.createElement('img');
  img.src = avatarUrl || 'https://cdn01.nebulastudios.top/images/default-avatar.svg';
  img.alt = user.username;
  currentAvatarEl.appendChild(img);

  // 隐藏加载器
  pageLoader.classList.add('is-hidden');

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
        navigateTo(page);
        // 移动端点击导航后关闭侧边栏
        sidebar.classList.remove('is-open');
      }
    });
  });

  // 绑定侧边栏切换
  sidebarToggle.addEventListener('click', () => {
    sidebar.classList.toggle('is-open');
  });

  // 点击主内容区关闭侧边栏（移动端）
  document.querySelector('.main-content')?.addEventListener('click', () => {
    sidebar.classList.remove('is-open');
  });

  // 初始化用户管理页面
  initUsersPage();

  // 绑定退出登录
  logoutBtn.addEventListener('click', logout);

  // 绑定弹窗关闭
  userModal.querySelector('.modal-close-btn')?.addEventListener('click', () => hideModal(userModal));
  confirmCancel.addEventListener('click', () => hideModal(confirmModal));

  // 点击遮罩关闭弹窗
  [userModal, confirmModal].forEach(modal => {
    modal.addEventListener('click', (e) => {
      if (e.target === modal) hideModal(modal);
    });
  });

  // 监听 hash 变化
  window.addEventListener('hashchange', () => {
    const hash = window.location.hash.slice(1) || 'dashboard';
    navigateTo(hash);
  });
}

// 启动
init();
