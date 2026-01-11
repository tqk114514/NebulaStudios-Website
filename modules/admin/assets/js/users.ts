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

// ==================== 用户列表 ====================

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

function bindUserRowEvents(row: HTMLTableRowElement): void {
  const btn = row.querySelector('.action-btn.view');
  btn?.addEventListener('click', () => {
    const userId = Number((btn as HTMLElement).dataset.userId);
    showUserDetail(userId);
  });
}

async function updateUserRow(userId: number): Promise<void> {
  const oldRow = usersTableBody.querySelector(`tr[data-user-id="${userId}"]`) as HTMLTableRowElement;
  if (!oldRow) return;

  oldRow.classList.add('is-updating');

  const user = await getUser(userId);
  if (!user) {
    oldRow.classList.remove('is-updating');
    return;
  }

  usersCache.set(userId, { user, cachedAt: Date.now() });

  const temp = document.createElement('tbody');
  temp.innerHTML = renderUserRow(user);
  const newRow = temp.firstElementChild as HTMLTableRowElement;

  oldRow.replaceWith(newRow);
  bindUserRowEvents(newRow);
}

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
  data.users.forEach(user => usersCache.set(user.id, { user, cachedAt: now }));

  usersTableBody.innerHTML = data.users.map(user => renderUserRow(user)).join('');

  usersTableBody.querySelectorAll('tr[data-user-id]').forEach(row => {
    bindUserRowEvents(row as HTMLTableRowElement);
  });

  renderPagination(data.page, data.totalPages);
}

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

function renderUserDetailContent(user: UserPublic, cachedAt?: number, isRefreshing?: boolean): void {
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
    </div>
    <div class="user-detail-meta" id="user-detail-meta">
      ${cachedAt ? `数据更新于 ${formatRelativeTime(cachedAt)}` : ''}${isRefreshing ? ' · 刷新中...' : ''}
    </div>
  `;
}

function bindUserDetailButtons(user: UserPublic): void {
  let footerHtml = '<button class="btn btn-secondary" id="close-user-modal">关闭</button>';

  if (currentUserRole >= 2 && user.role < 2) {
    if (user.role === 0) {
      footerHtml += `<button class="btn btn-warning" id="promote-user" data-user-id="${user.id}">设为管理员</button>`;
    } else if (user.role === 1) {
      footerHtml += `<button class="btn btn-secondary" id="demote-user" data-user-id="${user.id}">撤销管理员</button>`;
    }
    footerHtml += `<button class="btn btn-danger" id="delete-user" data-user-id="${user.id}">删除用户</button>`;
  }

  userModalFooter.innerHTML = footerHtml;

  document.getElementById('close-user-modal')?.addEventListener('click', () => hideModal(userModal));

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
      usersCache.set(userId, { user: freshUser, cachedAt: newCachedAt });
      
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
  usersCache.set(userId, { user, cachedAt });
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
}
