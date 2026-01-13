/**
 * modules/admin/assets/js/users.ts
 * 管理后台用户管理模块
 *
 * 功能：
 * - 用户列表（分页、搜索）
 * - 用户详情弹窗
 * - 用户操作（设置角色、删除）
 * - 用户数据缓存
 */

import {
  fetchApi,
  UserPublic,
  UserListResponse,
  CachedUser,
  ROLE_NAMES,
  ROLE_CLASSES,
  userModal,
  userModalBody,
  userModalFooter,
  banModal,
  banReason,
  banCustomReasonGroup,
  banCustomReason,
  banDuration,
  banCancel,
  banConfirm,
  banModalClose,
  showToast,
  showModal,
  hideModal,
  showConfirm,
  formatDate,
  formatRelativeTime,
  escapeHtml
} from './common';
import { loadStats } from './stats';

// ==================== 状态 ====================

let currentPage = 1;
let currentSearch = '';
let currentUserRole = 0;
let usersCache: Map<number, CachedUser> = new Map();

/** 缓存最大条目数 */
const CACHE_MAX_SIZE = 100;

/**
 * 添加用户到缓存（带大小限制）
 * @param userId - 用户 ID
 * @param cached - 缓存数据
 */
function setCacheUser(userId: number, cached: CachedUser): void {
  // 如果缓存已满，删除最旧的条目
  if (usersCache.size >= CACHE_MAX_SIZE && !usersCache.has(userId)) {
    const oldestKey = usersCache.keys().next().value;
    if (oldestKey !== undefined) {
      usersCache.delete(oldestKey);
    }
  }
  usersCache.set(userId, cached);
}

// ==================== DOM 元素 ====================

const userSearch = document.getElementById('user-search') as HTMLInputElement;
const searchBtn = document.getElementById('search-btn') as HTMLButtonElement;
const usersTableBody = document.getElementById('users-table-body') as HTMLTableSectionElement;
const pagination = document.getElementById('pagination') as HTMLElement;

// ==================== API ====================

async function getUsers(page: number, search: string): Promise<UserListResponse | null> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  if (search) params.set('search', search);
  
  const result = await fetchApi<UserListResponse>(`/admin/api/users?${params}`);
  return result.success ? result.data! : null;
}

async function getUser(id: number): Promise<UserPublic | null> {
  const result = await fetchApi<UserPublic>(`/admin/api/users/${id}`);
  return result.success ? result.data! : null;
}

async function setUserRole(id: number, role: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${id}/role`, {
    method: 'PUT',
    body: JSON.stringify({ role })
  });
  return result.success;
}

async function deleteUser(id: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${id}`, {
    method: 'DELETE'
  });
  return result.success;
}

async function banUser(id: number, reason: string, days: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${id}/ban`, {
    method: 'POST',
    body: JSON.stringify({ reason, days })
  });
  return result.success;
}

async function unbanUser(id: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${id}/unban`, {
    method: 'POST'
  });
  return result.success;
}

// ==================== 用户列表 ====================

/**
 * 渲染用户表格行 HTML
 * @param user - 用户数据
 * @returns 表格行 HTML 字符串
 */
function renderUserRow(user: UserPublic): string {
  return `
    <tr data-user-id="${user.id}">
      <td>${user.id}</td>
      <td>${escapeHtml(user.username)}</td>
      <td>${escapeHtml(user.email)}</td>
      <td><span class="role-badge ${ROLE_CLASSES[user.role]}">${ROLE_NAMES[user.role]}</span></td>
      <td>${formatDate(user.created_at)}</td>
      <td>
        <button class="action-btn view" data-user-id="${user.id}">查看</button>
      </td>
    </tr>
  `;
}

/**
 * 绑定用户行的事件监听器
 * @param row - 表格行元素
 */
function bindUserRowEvents(row: HTMLTableRowElement): void {
  const btn = row.querySelector('.action-btn.view');
  btn?.addEventListener('click', () => {
    const userId = Number((btn as HTMLElement).dataset.userId);
    showUserDetail(userId);
  });
}

/**
 * 更新指定用户的表格行（重新获取数据并刷新显示）
 * @param userId - 用户 ID
 */
async function updateUserRow(userId: number): Promise<void> {
  const oldRow = usersTableBody.querySelector(`tr[data-user-id="${userId}"]`) as HTMLTableRowElement;
  if (!oldRow) return;

  oldRow.classList.add('is-updating');

  const user = await getUser(userId);
  if (!user) {
    oldRow.classList.remove('is-updating');
    return;
  }

  setCacheUser(userId, { user, cachedAt: Date.now() });

  const temp = document.createElement('tbody');
  temp.innerHTML = renderUserRow(user);
  const newRow = temp.firstElementChild as HTMLTableRowElement;

  oldRow.replaceWith(newRow);
  bindUserRowEvents(newRow);
}

/**
 * 从表格中移除用户行（带动画效果）
 * @param userId - 用户 ID
 */
function removeUserRow(userId: number): void {
  const row = usersTableBody.querySelector(`tr[data-user-id="${userId}"]`) as HTMLTableRowElement;
  if (!row) return;

  usersCache.delete(userId);
  row.classList.add('is-deleting');

  setTimeout(() => {
    row.style.transition = 'opacity 0.2s, transform 0.2s';
    row.style.opacity = '0';
    row.style.transform = 'translateX(-20px)';

    setTimeout(() => {
      row.remove();
      if (usersTableBody.children.length === 0) {
        usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">暂无数据</td></tr>';
      }
    }, 200);
  }, 600);
}

export async function loadUsers(): Promise<void> {
  usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">加载中...</td></tr>';

  const data = await getUsers(currentPage, currentSearch);
  if (!data) {
    usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">加载失败</td></tr>';
    return;
  }

  if (data.users.length === 0) {
    usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">暂无数据</td></tr>';
    pagination.innerHTML = '';
    return;
  }

  const now = Date.now();
  data.users.forEach(user => setCacheUser(user.id, { user, cachedAt: now }));

  usersTableBody.innerHTML = data.users.map(user => renderUserRow(user)).join('');

  usersTableBody.querySelectorAll('tr[data-user-id]').forEach(row => {
    bindUserRowEvents(row as HTMLTableRowElement);
  });

  renderPagination(data.page, data.totalPages);
}

/**
 * 渲染分页控件
 * @param current - 当前页码
 * @param total - 总页数
 */
function renderPagination(current: number, total: number): void {
  if (total <= 1) {
    pagination.innerHTML = '';
    return;
  }

  let html = '';
  html += `<button ${current === 1 ? 'disabled' : ''} data-page="${current - 1}">上一页</button>`;

  const start = Math.max(1, current - 2);
  const end = Math.min(total, current + 2);

  if (start > 1) {
    html += `<button data-page="1">1</button>`;
    if (start > 2) html += `<button disabled>...</button>`;
  }

  for (let i = start; i <= end; i++) {
    html += `<button ${i === current ? 'class="active"' : ''} data-page="${i}">${i}</button>`;
  }

  if (end < total) {
    if (end < total - 1) html += `<button disabled>...</button>`;
    html += `<button data-page="${total}">${total}</button>`;
  }

  html += `<button ${current === total ? 'disabled' : ''} data-page="${current + 1}">下一页</button>`;

  pagination.innerHTML = html;

  pagination.querySelectorAll('button[data-page]').forEach(btn => {
    btn.addEventListener('click', () => {
      const page = Number((btn as HTMLElement).dataset.page);
      if (page && page !== currentPage) {
        currentPage = page;
        loadUsers();
      }
    });
  });
}

// ==================== 用户详情 ====================

const userDetailSkeleton = `
  <div class="user-detail">
    <div class="user-detail-row">
      <span class="user-detail-label">ID</span>
      <span class="user-detail-value skeleton-text"></span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">用户名</span>
      <span class="user-detail-value skeleton-text"></span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">邮箱</span>
      <span class="user-detail-value skeleton-text skeleton-wide"></span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">角色</span>
      <span class="user-detail-value skeleton-text"></span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">微软账户</span>
      <span class="user-detail-value skeleton-text"></span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">注册时间</span>
      <span class="user-detail-value skeleton-text skeleton-wide"></span>
    </div>
  </div>
`;

/**
 * 检查用户是否被封禁（考虑解封时间）
 */
function checkUserBanned(user: UserPublic): boolean {
  if (!user.is_banned) return false;
  if (user.unban_at && new Date(user.unban_at) < new Date()) {
    return false;
  }
  return true;
}

/**
 * 渲染用户详情弹窗内容
 * @param user - 用户数据
 * @param cachedAt - 缓存时间戳（可选）
 * @param isRefreshing - 是否正在刷新数据（可选）
 */
function renderUserDetailContent(user: UserPublic, cachedAt?: number, isRefreshing?: boolean): void {
  const isBanned = checkUserBanned(user);
  const banStatusHtml = isBanned ? `
    <div class="user-detail-row user-detail-banned">
      <span class="user-detail-label">封禁状态</span>
      <span class="user-detail-value">
        <span class="status-badge banned">已封禁</span>
      </span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">封禁原因</span>
      <span class="user-detail-value">${escapeHtml(user.ban_reason || '-')}</span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">封禁时间</span>
      <span class="user-detail-value">${formatDate(user.banned_at)}</span>
    </div>
    <div class="user-detail-row">
      <span class="user-detail-label">解封时间</span>
      <span class="user-detail-value ${!user.unban_at ? 'permanent-ban' : ''}">${user.unban_at ? formatDate(user.unban_at) : '永久封禁'}</span>
    </div>
  ` : '';

  userModalBody.innerHTML = `
    <div class="user-detail">
      <div class="user-detail-row">
        <span class="user-detail-label">ID</span>
        <span class="user-detail-value">${user.id}</span>
      </div>
      <div class="user-detail-row">
        <span class="user-detail-label">用户名</span>
        <span class="user-detail-value">${escapeHtml(user.username)}</span>
      </div>
      <div class="user-detail-row">
        <span class="user-detail-label">邮箱</span>
        <span class="user-detail-value">${escapeHtml(user.email)}</span>
      </div>
      <div class="user-detail-row">
        <span class="user-detail-label">角色</span>
        <span class="user-detail-value">
          <span class="role-badge ${ROLE_CLASSES[user.role]}">${ROLE_NAMES[user.role]}</span>
        </span>
      </div>
      <div class="user-detail-row">
        <span class="user-detail-label">微软账户</span>
        <span class="user-detail-value">${user.microsoft_name || '未绑定'}</span>
      </div>
      <div class="user-detail-row">
        <span class="user-detail-label">注册时间</span>
        <span class="user-detail-value">${formatDate(user.created_at)}</span>
      </div>
      ${banStatusHtml}
    </div>
    <div class="user-detail-meta" id="user-detail-meta">
      ${cachedAt ? `数据更新于 ${formatRelativeTime(cachedAt)}` : ''}${isRefreshing ? ' · 刷新中...' : ''}
    </div>
  `;
}

/**
 * 绑定用户详情弹窗的操作按钮事件
 * @param user - 用户数据
 */
function bindUserDetailButtons(user: UserPublic): void {
  let footerHtml = '<button class="btn btn-secondary" id="close-user-modal">关闭</button>';

  const isBanned = checkUserBanned(user);

  // 管理员可以封禁/解封普通用户
  if (currentUserRole >= 1 && user.role < 1) {
    if (isBanned) {
      footerHtml += `<button class="btn btn-success" id="unban-user" data-user-id="${user.id}">解除封禁</button>`;
    } else {
      footerHtml += `<button class="btn btn-warning" id="ban-user" data-user-id="${user.id}">封禁用户</button>`;
    }
  }

  // 超级管理员可以设置角色和删除用户
  if (currentUserRole >= 2 && user.role < 2) {
    // 封禁用户不能设为管理员
    if (user.role === 0 && !isBanned) {
      footerHtml += `<button class="btn btn-warning" id="promote-user" data-user-id="${user.id}">设为管理员</button>`;
    } else if (user.role === 1) {
      footerHtml += `<button class="btn btn-secondary" id="demote-user" data-user-id="${user.id}">撤销管理员</button>`;
    }
    footerHtml += `<button class="btn btn-danger" id="delete-user" data-user-id="${user.id}">删除用户</button>`;
  }

  userModalFooter.innerHTML = footerHtml;

  document.getElementById('close-user-modal')?.addEventListener('click', () => hideModal(userModal));

  // 封禁用户
  document.getElementById('ban-user')?.addEventListener('click', () => {
    hideModal(userModal);
    showBanModal(user);
  });

  // 解封用户
  document.getElementById('unban-user')?.addEventListener('click', async () => {
    showConfirm('确认解封', `确定要解除 ${user.username} 的封禁吗？`, async () => {
      const success = await unbanUser(user.id);
      if (success) {
        showToast('已解除封禁', 'success');
        hideModal(userModal);
        updateUserRow(user.id);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('promote-user')?.addEventListener('click', async () => {
    showConfirm('确认操作', `确定要将 ${user.username} 设为管理员吗？`, async () => {
      const success = await setUserRole(user.id, 1);
      if (success) {
        showToast('已设为管理员', 'success');
        hideModal(userModal);
        updateUserRow(user.id);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('demote-user')?.addEventListener('click', async () => {
    showConfirm('确认操作', `确定要撤销 ${user.username} 的管理员权限吗？`, async () => {
      const success = await setUserRole(user.id, 0);
      if (success) {
        showToast('已撤销管理员', 'success');
        hideModal(userModal);
        updateUserRow(user.id);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('delete-user')?.addEventListener('click', async () => {
    showConfirm('确认删除', `确定要删除用户 ${user.username} 吗？此操作不可恢复！`, async () => {
      const success = await deleteUser(user.id);
      if (success) {
        showToast('用户已删除', 'success');
        hideModal(userModal);
        removeUserRow(user.id);
        loadStats();
      } else {
        showToast('删除失败', 'error');
      }
    });
  });
}

// ==================== 封禁弹窗 ====================

let currentBanUser: UserPublic | null = null;

/**
 * 显示封禁用户弹窗
 */
function showBanModal(user: UserPublic): void {
  currentBanUser = user;
  
  // 重置表单
  banReason.value = '';
  banCustomReason.value = '';
  banCustomReasonGroup.style.display = 'none';
  banDuration.value = '7';
  banConfirm.disabled = true;

  showModal(banModal);
}

/**
 * 初始化封禁弹窗事件
 */
function initBanModal(): void {
  // 封禁原因选择
  banReason.addEventListener('change', () => {
    if (banReason.value === '其他') {
      banCustomReasonGroup.style.display = 'block';
      banConfirm.disabled = !banCustomReason.value.trim();
    } else {
      banCustomReasonGroup.style.display = 'none';
      banConfirm.disabled = !banReason.value;
    }
  });

  // 自定义原因输入
  banCustomReason.addEventListener('input', () => {
    banConfirm.disabled = !banCustomReason.value.trim();
  });

  // 取消按钮
  banCancel.addEventListener('click', () => {
    hideModal(banModal);
    currentBanUser = null;
  });

  // 关闭按钮
  banModalClose.addEventListener('click', () => {
    hideModal(banModal);
    currentBanUser = null;
  });

  // 确认封禁
  banConfirm.addEventListener('click', async () => {
    if (!currentBanUser) return;

    const reason = banReason.value === '其他' ? banCustomReason.value.trim() : banReason.value;
    const days = parseInt(banDuration.value, 10);

    if (!reason) {
      showToast('请选择或输入封禁原因', 'error');
      return;
    }

    banConfirm.disabled = true;

    const success = await banUser(currentBanUser.id, reason, days);
    if (success) {
      showToast('用户已封禁', 'success');
      hideModal(banModal);
      updateUserRow(currentBanUser.id);
      loadStats();
      currentBanUser = null;
    } else {
      showToast('封禁失败', 'error');
      banConfirm.disabled = false;
    }
  });
}

/**
 * 显示用户详情弹窗（优先使用缓存，后台刷新数据）
 * @param userId - 用户 ID
 */
async function showUserDetail(userId: number): Promise<void> {
  const cached = usersCache.get(userId);
  
  if (cached) {
    renderUserDetailContent(cached.user, cached.cachedAt, true);
    showModal(userModal);
    
    getUser(userId).then(freshUser => {
      if (!freshUser) {
        const metaEl = document.getElementById('user-detail-meta');
        if (metaEl) metaEl.textContent = `数据更新于 ${formatRelativeTime(cached.cachedAt)}`;
        return;
      }
      
      const newCachedAt = Date.now();
      setCacheUser(userId, { user: freshUser, cachedAt: newCachedAt });
      
      if (!userModal.classList.contains('is-hidden')) {
        renderUserDetailContent(freshUser, newCachedAt, false);
        bindUserDetailButtons(freshUser);
      }
    });
    
    bindUserDetailButtons(cached.user);
    return;
  }

  userModalBody.innerHTML = userDetailSkeleton;
  userModalFooter.innerHTML = '<button class="btn btn-secondary" id="close-user-modal">关闭</button>';
  document.getElementById('close-user-modal')?.addEventListener('click', () => hideModal(userModal));
  showModal(userModal);

  const user = await getUser(userId);
  if (!user) {
    hideModal(userModal);
    showToast('获取用户信息失败', 'error');
    return;
  }

  const cachedAt = Date.now();
  setCacheUser(userId, { user, cachedAt });
  renderUserDetailContent(user, cachedAt, false);
  bindUserDetailButtons(user);
}

// ==================== 初始化 ====================

export function setCurrentUserRole(role: number): void {
  currentUserRole = role;
}

export function initUsersPage(): void {
  searchBtn.addEventListener('click', () => {
    currentSearch = userSearch.value.trim();
    currentPage = 1;
    loadUsers();
  });

  userSearch.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
      currentSearch = userSearch.value.trim();
      currentPage = 1;
      loadUsers();
    }
  });

  // 初始化封禁弹窗
  initBanModal();
}
