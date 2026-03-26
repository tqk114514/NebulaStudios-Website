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
  ROLE_NAMES,
  ROLE_CLASSES,
  userModal,
  userModalBody,
  userModalFooter,
  banModal,
  banReason,
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
  escapeHtml,
  renderPagination,
  DataCache,
  updateTableRow,
  removeTableRow,
  initSearch,
  renderRoleBadge
} from './common';
import { loadStats } from './stats';

function translateBanReason(reason: string): string {
  const reasonMap: Record<string, string> = {
    'violation': '违反服务条款',
    'abuse': '滥用服务',
    'malicious': '恶意行为',
    'spam': '垃圾信息'
  };
  return reasonMap[reason] || reason;
}

// ==================== 状态 ====================

let currentPage = 1;
let currentSearch = '';
let currentUserRole = 0;
const usersCache = new DataCache<UserPublic>();

// ==================== DOM 元素 ====================

const userSearch = document.getElementById('user-search') as HTMLInputElement | null;
const searchBtn = document.getElementById('search-btn') as HTMLButtonElement | null;
const usersTableBody = document.getElementById('users-table-body') as HTMLTableSectionElement | null;
const pagination = document.getElementById('pagination') as HTMLElement | null;

// ==================== API ====================

async function getUsers(page: number, search: string): Promise<UserListResponse | null> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  if (search) params.set('search', search);
  
  const result = await fetchApi<UserListResponse>(`/admin/api/users?${params}`);
  return result.success ? result.data! : null;
}

async function getUser(uid: string): Promise<UserPublic | null> {
  const result = await fetchApi<UserPublic>(`/admin/api/users/${uid}`);
  return result.success ? result.data! : null;
}

async function setUserRole(uid: string, role: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${uid}/role`, {
    method: 'PUT',
    body: JSON.stringify({ role })
  });
  return result.success;
}

async function deleteUser(uid: string): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${uid}`, {
    method: 'DELETE'
  });
  return result.success;
}

async function banUser(uid: string, reason: string, days: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${uid}/ban`, {
    method: 'POST',
    body: JSON.stringify({ reason, days })
  });
  return result.success;
}

async function unbanUser(uid: string): Promise<boolean> {
  const result = await fetchApi(`/admin/api/users/${uid}/unban`, {
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
    <tr data-user-uid="${user.uid}">
      <td>${user.uid}</td>
      <td>${escapeHtml(user.username)}</td>
      <td>${escapeHtml(user.email)}</td>
      <td>${renderRoleBadge(user.role)}</td>
      <td>${formatDate(user.created_at)}</td>
      <td>
        <button class="action-btn view" data-user-uid="${user.uid}">查看</button>
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
    const userUid = (btn as HTMLElement).dataset.userUid!;
    showUserDetail(userUid);
  });
}

/**
 * 更新指定用户的表格行（重新获取数据并刷新显示）
 * @param userUid - 用户 UID
 */
async function updateUserRow(userUid: string): Promise<void> {
  if (!usersTableBody) return;
  
  await updateTableRow({
    tableBody: usersTableBody,
    rowId: userUid,
    rowIdAttr: 'data-user-uid',
    fetchData: () => getUser(userUid),
    renderRow: renderUserRow,
    bindEvents: bindUserRowEvents,
    cache: usersCache,
    cacheKey: userUid
  });
}

/**
 * 从表格中移除用户行（带动画效果）
 * @param userUid - 用户 UID
 */
function removeUserRow(userUid: string): void {
  if (!usersTableBody) return;
  
  removeTableRow({
    tableBody: usersTableBody,
    rowId: userUid,
    rowIdAttr: 'data-user-uid',
    cache: usersCache as DataCache<unknown>,
    cacheKey: userUid,
    colspan: 6
  });
}

export async function loadUsers(): Promise<void> {
  console.log('[ADMIN][USERS] loadUsers called');
  
  if (!usersTableBody) {
    console.error('[ADMIN][USERS] usersTableBody element not found');
    return;
  }
  
  usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">加载中...</td></tr>';

  const data = await getUsers(currentPage, currentSearch);
  if (!data) {
    usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">加载失败</td></tr>';
    return;
  }

  if (data.users.length === 0) {
    usersTableBody.innerHTML = '<tr><td colspan="6" class="loading-cell">暂无数据</td></tr>';
    if (pagination) {
      pagination.innerHTML = '';
    }
    return;
  }

  const now = Date.now();
  data.users.forEach(user => usersCache.set(user.uid, user));

  usersTableBody.innerHTML = data.users.map(user => renderUserRow(user)).join('');

  usersTableBody.querySelectorAll('tr[data-user-uid]').forEach(row => {
    bindUserRowEvents(row as HTMLTableRowElement);
  });

  if (pagination) {
    renderPagination({
      container: pagination,
      current: data.page,
      total: data.totalPages,
      onPageChange: (page) => {
        currentPage = page;
        loadUsers();
      }
    });
  }
}

// ==================== 用户详情 ====================

const userDetailSkeleton = `
  <div class="user-detail">
    <div class="user-detail-row">
      <span class="user-detail-label">UID</span>
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
  console.log('[ADMIN][USERS] renderUserDetailContent called');
  
  if (!userModalBody) {
    console.error('[ADMIN][USERS] userModalBody not found');
    return;
  }
  
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
      <span class="user-detail-value">${user.ban_reason ? escapeHtml(translateBanReason(user.ban_reason)) : '-'}</span>
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
        <span class="user-detail-label">UID</span>
        <span class="user-detail-value">${user.uid}</span>
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
          ${renderRoleBadge(user.role)}
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
  console.log('[ADMIN][USERS] bindUserDetailButtons called');
  
  if (!userModalFooter) {
    console.error('[ADMIN][USERS] userModalFooter not found');
    return;
  }
  
  let footerHtml = '<button class="btn btn-secondary" id="close-user-modal">关闭</button>';

  const isBanned = checkUserBanned(user);

  // 管理员可以封禁/解封普通用户
  if (currentUserRole >= 1 && user.role < 1) {
    if (isBanned) {
      footerHtml += `<button class="btn btn-success" id="unban-user" data-user-uid="${user.uid}">解除封禁</button>`;
    } else {
      footerHtml += `<button class="btn btn-warning" id="ban-user" data-user-uid="${user.uid}">封禁用户</button>`;
    }
  }

  // 超级管理员可以设置角色和删除用户
  if (currentUserRole >= 2 && user.role < 2) {
    // 封禁用户不能设为管理员
    if (user.role === 0 && !isBanned) {
      footerHtml += `<button class="btn btn-warning" id="promote-user" data-user-uid="${user.uid}">设为管理员</button>`;
    } else if (user.role === 1) {
      footerHtml += `<button class="btn btn-secondary" id="demote-user" data-user-uid="${user.uid}">撤销管理员</button>`;
    }
    footerHtml += `<button class="btn btn-danger" id="delete-user" data-user-uid="${user.uid}">删除用户</button>`;
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
      const success = await unbanUser(user.uid);
      if (success) {
        showToast('已解除封禁', 'success');
        hideModal(userModal);
        updateUserRow(user.uid);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('promote-user')?.addEventListener('click', async () => {
    showConfirm('确认操作', `确定要将 ${user.username} 设为管理员吗？`, async () => {
      const success = await setUserRole(user.uid, 1);
      if (success) {
        showToast('已设为管理员', 'success');
        hideModal(userModal);
        updateUserRow(user.uid);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('demote-user')?.addEventListener('click', async () => {
    showConfirm('确认操作', `确定要撤销 ${user.username} 的管理员权限吗？`, async () => {
      const success = await setUserRole(user.uid, 0);
      if (success) {
        showToast('已撤销管理员', 'success');
        hideModal(userModal);
        updateUserRow(user.uid);
        loadStats();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('delete-user')?.addEventListener('click', async () => {
    showConfirm('确认删除', `确定要删除用户 ${user.username} 吗？此操作不可恢复！`, async () => {
      const success = await deleteUser(user.uid);
      if (success) {
        showToast('用户已删除', 'success');
        hideModal(userModal);
        removeUserRow(user.uid);
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
  console.log('[ADMIN][USERS] showBanModal called');
  
  const localBanReason = banReason;
  const localBanDuration = banDuration;
  const localBanConfirm = banConfirm;
  
  if (!localBanReason || !localBanDuration || !localBanConfirm) {
    console.error('[ADMIN][USERS] Ban modal elements not found');
    return;
  }
  
  currentBanUser = user;
  
  // 重置表单
  localBanReason.value = '';
  localBanDuration.value = '7';
  localBanConfirm.disabled = true;

  showModal(banModal);
}

/**
 * 初始化封禁弹窗事件
 */
function initBanModal(): void {
  console.log('[ADMIN][USERS] initBanModal called');
  
  const localBanReason = banReason;
  const localBanConfirm = banConfirm;
  const localBanCancel = banCancel;
  const localBanModalClose = banModalClose;
  const localBanDuration = banDuration;
  
  // 检查必要元素是否存在
  if (!localBanReason || !localBanConfirm || !localBanCancel || !localBanModalClose || !localBanDuration) {
    console.warn('[ADMIN][USERS] Ban modal elements not all found, skipping ban modal init');
    return;
  }
  
  // 封禁原因选择
  localBanReason.addEventListener('change', () => {
    localBanConfirm.disabled = !localBanReason.value;
  });

  // 取消按钮
  localBanCancel.addEventListener('click', () => {
    hideModal(banModal);
    currentBanUser = null;
  });

  // 关闭按钮
  localBanModalClose.addEventListener('click', () => {
    hideModal(banModal);
    currentBanUser = null;
  });

  // 确认封禁
  localBanConfirm.addEventListener('click', async () => {
    if (!currentBanUser) return;

    const reason = localBanReason.value;
    const days = parseInt(localBanDuration.value, 10);

    if (!reason) {
      showToast('请选择封禁原因', 'error');
      return;
    }

    localBanConfirm.disabled = true;

    const success = await banUser(currentBanUser.uid, reason, days);
    if (success) {
      showToast('用户已封禁', 'success');
      hideModal(banModal);
      updateUserRow(currentBanUser.uid);
      loadStats();
      currentBanUser = null;
    } else {
      showToast('封禁失败', 'error');
      localBanConfirm.disabled = false;
    }
  });
}

/**
 * 显示用户详情弹窗（优先使用缓存，后台刷新数据）
 * @param userUid - 用户 UID
 */
async function showUserDetail(userUid: string): Promise<void> {
  console.log('[ADMIN][USERS] showUserDetail called');
  
  if (!userModal || !userModalBody || !userModalFooter) {
    console.error('[ADMIN][USERS] User modal elements not found');
    return;
  }
  
  const cached = usersCache.get(userUid);
  
  if (cached) {
    renderUserDetailContent(cached.data, cached.cachedAt, true);
    showModal(userModal);
    
    getUser(userUid).then(freshUser => {
      if (!freshUser) {
        const metaEl = document.getElementById('user-detail-meta');
        if (metaEl) metaEl.textContent = `数据更新于 ${formatRelativeTime(cached.cachedAt)}`;
        return;
      }
      
      usersCache.set(userUid, freshUser);
      const newCachedAt = Date.now();
      
      if (userModal && !userModal.classList.contains('is-hidden')) {
        renderUserDetailContent(freshUser, newCachedAt, false);
        bindUserDetailButtons(freshUser);
      }
    });
    
    bindUserDetailButtons(cached.data);
    return;
  }

  userModalBody.innerHTML = userDetailSkeleton;
  userModalFooter.innerHTML = '<button class="btn btn-secondary" id="close-user-modal">关闭</button>';
  document.getElementById('close-user-modal')?.addEventListener('click', () => hideModal(userModal));
  showModal(userModal);

  const user = await getUser(userUid);
  if (!user) {
    hideModal(userModal);
    showToast('获取用户信息失败', 'error');
    return;
  }

  usersCache.set(userUid, user);
  renderUserDetailContent(user, Date.now(), false);
  bindUserDetailButtons(user);
}

// ==================== 初始化 ====================

export function setCurrentUserRole(role: number): void {
  currentUserRole = role;
}

export function initUsersPage(): void {
  console.log('[ADMIN][USERS] initUsersPage called');
  
  if (searchBtn && userSearch) {
    initSearch(userSearch, searchBtn, (query) => {
      currentSearch = query;
      currentPage = 1;
      loadUsers();
    });
  } else {
    console.warn('[ADMIN][USERS] search elements not found, skipping search initialization');
  }

  initBanModal();
}
