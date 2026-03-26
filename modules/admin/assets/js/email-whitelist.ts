/**
 * modules/admin/assets/js/email-whitelist.ts
 * 管理后台邮箱白名单管理模块
 *
 * 功能：
 * - 白名单列表（分页）
 * - 白名单详情弹窗
 * - 白名单操作（启用/禁用、编辑、删除）
 * - 白名单数据缓存
 */

import {
  fetchApi,
  showToast,
  showConfirm,
  formatDate,
  formatRelativeTime,
  escapeHtml,
  renderPagination,
  whitelistModal,
  whitelistModalBody,
  whitelistModalFooter,
  whitelistModalClose,
  showModal,
  hideModal,
  DataCache,
  updateTableRow,
  removeTableRow,
  renderStatusBadge,
  showDetailWithCache
} from './common';

// ==================== 类型定义 ====================

export interface EmailWhitelistEntry {
  id: number;
  domain: string;
  signup_url: string;
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

interface EmailWhitelistListResponse {
  whitelist: EmailWhitelistEntry[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

// ==================== 状态 ====================

let currentPage = 1;
let currentEntries: EmailWhitelistEntry[] = [];
let editingEntryId: number | null = null;
const whitelistCache = new DataCache<EmailWhitelistEntry>();

// ==================== DOM 元素 ====================

const whitelistTableBody = document.getElementById('whitelist-table-body') as HTMLTableSectionElement | null;
const whitelistPagination = document.getElementById('whitelist-pagination') as HTMLElement | null;

// ==================== API ====================

async function getWhitelist(page: number): Promise<EmailWhitelistListResponse | null> {
  try {
    const params = new URLSearchParams({ page: String(page), pageSize: '20' });
    const result = await fetchApi<EmailWhitelistListResponse>(`/admin/api/email-whitelist?${params}`);
    if (result.success && result.data) {
      return result.data;
    }
    return null;
  } catch (e) {
    console.error('[WHITELIST] Failed to load whitelist:', e);
    return null;
  }
}

async function getEntry(id: number): Promise<EmailWhitelistEntry | null> {
  try {
    const result = await fetchApi<{ item: EmailWhitelistEntry }>(`/admin/api/email-whitelist/${id}`);
    if (result.success && result.data) {
      return result.data.item || null;
    }
    return null;
  } catch (e) {
    console.error('[WHITELIST] Failed to get entry:', e);
    return null;
  }
}

async function createEntry(domain: string, signupUrl: string): Promise<EmailWhitelistEntry | null> {
  try {
    const result = await fetchApi<{ item: EmailWhitelistEntry }>('/admin/api/email-whitelist', {
      method: 'POST',
      body: JSON.stringify({ domain, signup_url: signupUrl }),
    });
    if (result.success && result.data) {
      return result.data.item || null;
    }
    return null;
  } catch (e) {
    console.error('[WHITELIST] Failed to create entry:', e);
    throw e;
  }
}

async function updateEntry(id: number, domain: string, signupUrl: string, isEnabled: boolean): Promise<EmailWhitelistEntry | null> {
  try {
    const result = await fetchApi<{ item: EmailWhitelistEntry }>(`/admin/api/email-whitelist/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ domain, signup_url: signupUrl, is_enabled: isEnabled }),
    });
    if (result.success && result.data) {
      return result.data.item || null;
    }
    return null;
  } catch (e) {
    console.error('[WHITELIST] Failed to update entry:', e);
    throw e;
  }
}

async function deleteEntry(id: number): Promise<void> {
  try {
    const result = await fetchApi(`/admin/api/email-whitelist/${id}`, {
      method: 'DELETE',
    });
    if (!result.success) {
      throw new Error('Delete failed');
    }
  } catch (e) {
    console.error('[WHITELIST] Failed to delete entry:', e);
    throw e;
  }
}

async function toggleEntry(id: number, isEnabled: boolean): Promise<void> {
  try {
    const result = await fetchApi(`/admin/api/email-whitelist/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ is_enabled: isEnabled }),
    });
    if (!result.success) {
      throw new Error('Toggle failed');
    }
  } catch (e) {
    console.error('[WHITELIST] Failed to toggle entry:', e);
    throw e;
  }
}

// ==================== 渲染 ====================

function renderWhitelistRow(entry: EmailWhitelistEntry): string {
  return `
    <tr data-id="${entry.id}">
      <td>${escapeHtml(entry.domain)}</td>
      <td class="url-cell" title="${escapeHtml(entry.signup_url)}">${escapeHtml(entry.signup_url)}</td>
      <td>${renderStatusBadge(entry.is_enabled)}</td>
      <td>${formatDate(entry.created_at)}</td>
      <td>
        <button class="action-btn view" data-id="${entry.id}">查看</button>
      </td>
    </tr>
  `;
}

function bindWhitelistRowEvents(row: HTMLTableRowElement): void {
  const btn = row.querySelector('.action-btn.view');
  btn?.addEventListener('click', () => {
    const entryId = Number((btn as HTMLElement).dataset.id);
    showWhitelistDetail(entryId);
  });
}

/**
 * 更新指定白名单的表格行（重新获取数据并刷新显示）
 * @param entryId - 白名单 ID
 */
async function updateWhitelistRow(entryId: number): Promise<void> {
  if (!whitelistTableBody) return;
  
  await updateTableRow({
    tableBody: whitelistTableBody,
    rowId: entryId,
    rowIdAttr: 'data-id',
    fetchData: () => getEntry(entryId),
    renderRow: renderWhitelistRow,
    bindEvents: bindWhitelistRowEvents,
    cache: whitelistCache,
    cacheKey: entryId
  });
}

/**
 * 从表格中移除白名单行（带动画效果）
 * @param entryId - 白名单 ID
 */
function removeWhitelistRow(entryId: number): void {
  if (!whitelistTableBody) return;
  
  removeTableRow({
    tableBody: whitelistTableBody,
    rowId: entryId,
    rowIdAttr: 'data-id',
    cache: whitelistCache as DataCache<unknown>,
    cacheKey: entryId,
    colspan: 5
  });
}

function renderWhitelist(entries: EmailWhitelistEntry[], total: number, page: number, totalPages: number): void {
  const tableBody = document.getElementById('whitelist-table-body') as HTMLTableSectionElement | null;
  if (!tableBody) return;

  if (entries.length === 0) {
    tableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">暂无数据</td></tr>';
    if (whitelistPagination) {
      whitelistPagination.innerHTML = '';
    }
    return;
  }

  entries.forEach(entry => whitelistCache.set(entry.id, entry));

  tableBody.innerHTML = entries.map(entry => renderWhitelistRow(entry)).join('');

  tableBody.querySelectorAll('tr[data-id]').forEach(row => {
    bindWhitelistRowEvents(row as HTMLTableRowElement);
  });

  if (whitelistPagination) {
    renderPagination({
      container: whitelistPagination,
      current: page,
      total: totalPages,
      onPageChange: (newPage) => {
        currentPage = newPage;
        loadWhitelist();
      }
    });
  }
}

// ==================== 白名单详情 ====================

const whitelistDetailSkeleton = `
  <div class="detail">
    <div class="detail-row">
      <span class="detail-label">域名</span>
      <span class="detail-value skeleton-text"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">注册页面 URL</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">状态</span>
      <span class="detail-value skeleton-text"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">创建时间</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">更新时间</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
    </div>
  </div>
`;

function renderWhitelistDetailContent(entry: EmailWhitelistEntry, cachedAt?: number, isRefreshing?: boolean): string {
  return `
    <div class="detail">
      <div class="detail-row">
        <span class="detail-label">域名</span>
        <span class="detail-value">${escapeHtml(entry.domain)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">注册页面 URL</span>
        <span class="detail-value">${escapeHtml(entry.signup_url)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">状态</span>
        <span class="detail-value">${renderStatusBadge(entry.is_enabled)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">创建时间</span>
        <span class="detail-value">${formatDate(entry.created_at)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">更新时间</span>
        <span class="detail-value">${formatDate(entry.updated_at)}</span>
      </div>
    </div>
    <div class="detail-meta" id="whitelist-detail-meta">
      ${cachedAt ? `数据更新于 ${formatRelativeTime(cachedAt)}` : ''}${isRefreshing ? ' · 刷新中...' : ''}
    </div>
  `;
}

function renderWhitelistDetailFooter(entry: EmailWhitelistEntry): string {
  const toggleClass = entry.is_enabled ? 'btn-warning' : 'btn-success';
  const toggleText = entry.is_enabled ? '禁用' : '启用';
  return `
    <button class="btn btn-secondary" data-close-modal>关闭</button>
    <button class="btn ${toggleClass}" id="toggle-whitelist" data-id="${entry.id}">${toggleText}</button>
    <button class="btn btn-primary" id="edit-whitelist" data-id="${entry.id}">编辑</button>
    <button class="btn btn-danger" id="delete-whitelist" data-id="${entry.id}">删除</button>
  `;
}

function bindWhitelistDetailEvents(entry: EmailWhitelistEntry, modal: HTMLElement): void {
  modal.querySelector('[data-close-modal]')?.addEventListener('click', () => hideModal(modal));

  document.getElementById('toggle-whitelist')?.addEventListener('click', async () => {
    showConfirm(entry.is_enabled ? '禁用白名单' : '启用白名单', `确定要${entry.is_enabled ? '禁用' : '启用'}域名 "${entry.domain}" 吗？`, async () => {
      try {
        await toggleEntry(entry.id, !entry.is_enabled);
        showToast(entry.is_enabled ? '已禁用' : '已启用', 'success');
        hideModal(modal);
        updateWhitelistRow(entry.id);
      } catch {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('edit-whitelist')?.addEventListener('click', () => {
    hideModal(modal);
    openEditModal(entry);
  });

  document.getElementById('delete-whitelist')?.addEventListener('click', async () => {
    showConfirm('删除白名单', `确定要删除域名 "${entry.domain}" 吗？`, async () => {
      try {
        await deleteEntry(entry.id);
        showToast('删除成功', 'success');
        hideModal(modal);
        removeWhitelistRow(entry.id);
      } catch {
        showToast('删除失败', 'error');
      }
    });
  });
}

function showWhitelistDetail(entryId: number): void {
  console.log('[ADMIN][WHITELIST] showWhitelistDetail called');

  showDetailWithCache<EmailWhitelistEntry>({
    modal: whitelistModal,
    modalBody: whitelistModalBody,
    modalFooter: whitelistModalFooter,
    cache: whitelistCache,
    cacheKey: entryId,
    fetchData: () => getEntry(entryId),
    skeletonHtml: whitelistDetailSkeleton,
    renderContent: renderWhitelistDetailContent,
    renderFooter: renderWhitelistDetailFooter,
    bindFooterEvents: bindWhitelistDetailEvents
  });
}

// ==================== 弹窗操作 ====================

function openCreateModal(): void {
  const formTitle = document.getElementById('whitelist-form-title') as HTMLElement | null;
  const formSubmit = document.getElementById('whitelist-form-submit') as HTMLButtonElement | null;
  const domainInput = document.getElementById('whitelist-domain') as HTMLInputElement | null;
  const urlInput = document.getElementById('whitelist-signup-url') as HTMLInputElement | null;
  const formModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;

  editingEntryId = null;
  if (formTitle) formTitle.textContent = '添加白名单';
  if (formSubmit) formSubmit.textContent = '添加';
  if (domainInput) domainInput.value = '';
  if (urlInput) urlInput.value = '';
  if (formModal) formModal.classList.remove('is-hidden');
}

function openEditModal(entry: EmailWhitelistEntry): void {
  const formTitle = document.getElementById('whitelist-form-title') as HTMLElement | null;
  const formSubmit = document.getElementById('whitelist-form-submit') as HTMLButtonElement | null;
  const domainInput = document.getElementById('whitelist-domain') as HTMLInputElement | null;
  const urlInput = document.getElementById('whitelist-signup-url') as HTMLInputElement | null;
  const formModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;

  editingEntryId = entry.id;
  if (formTitle) formTitle.textContent = '编辑白名单';
  if (formSubmit) formSubmit.textContent = '保存';
  if (domainInput) domainInput.value = entry.domain;
  if (urlInput) urlInput.value = entry.signup_url;
  if (formModal) formModal.classList.remove('is-hidden');
}

function closeFormModal(): void {
  const formModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;
  editingEntryId = null;
  if (formModal) formModal.classList.add('is-hidden');
}

async function handleSubmit(e: Event): Promise<void> {
  e.preventDefault();

  const domainInput = document.getElementById('whitelist-domain') as HTMLInputElement | null;
  const urlInput = document.getElementById('whitelist-signup-url') as HTMLInputElement | null;

  if (!domainInput || !urlInput) return;

  const domain = domainInput.value.trim();
  const signupUrl = urlInput.value.trim();

  if (!domain) {
    showToast('请输入域名', 'error');
    return;
  }

  if (!signupUrl) {
    showToast('请输入注册页面 URL', 'error');
    return;
  }

  try {
    if (editingEntryId) {
      const entry = currentEntries.find(e => e.id === editingEntryId);
      await updateEntry(editingEntryId, domain, signupUrl, entry?.is_enabled ?? true);
      showToast('更新成功', 'success');
    } else {
      await createEntry(domain, signupUrl);
      showToast('添加成功', 'success');
    }
    closeFormModal();
    await loadWhitelist();
  } catch {
    showToast('操作失败', 'error');
  }
}

// ==================== 初始化 ====================

async function loadWhitelist(): Promise<void> {
  console.log('[ADMIN][WHITELIST] loadWhitelist called');

  if (!whitelistTableBody) {
    console.error('[ADMIN][WHITELIST] whitelistTableBody element not found');
    return;
  }

  whitelistTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载中...</td></tr>';

  const data = await getWhitelist(currentPage);
  if (!data) {
    whitelistTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载失败</td></tr>';
    return;
  }

  currentEntries = data.whitelist;
  renderWhitelist(data.whitelist, data.total, data.page, data.totalPages);
}

export function initWhitelistPage(): void {
  const createBtn = document.getElementById('create-whitelist-btn');
  const form = document.getElementById('whitelist-form') as HTMLFormElement | null;
  const formCancel = document.getElementById('whitelist-form-cancel');
  const formClose = document.getElementById('whitelist-form-close');

  if (whitelistModalClose) {
    whitelistModalClose.addEventListener('click', () => hideModal(whitelistModal));
  }

  if (createBtn) {
    createBtn.addEventListener('click', openCreateModal);
  }

  if (form) {
    form.addEventListener('submit', handleSubmit);
  }

  if (formCancel) {
    formCancel.addEventListener('click', closeFormModal);
  }

  if (formClose) {
    formClose.addEventListener('click', closeFormModal);
  }

  loadWhitelist();
}

export { loadWhitelist };
