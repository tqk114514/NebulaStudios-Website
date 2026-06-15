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
  renderList,
  whitelistModal,
  whitelistModalBody,
  whitelistModalFooter,
  whitelistModalClose,
  hideModal,
  DataCache,
  updateTableRow,
  animateTableRow,
  getToggleProps,
  renderStatusBadge,
  showDetailWithCache,
  initModalCloseEvents
} from './common';

// ==================== 类型定义 ====================

export interface EmailWhitelistEntry {
  id: number;
  domain: string;
  signup_url: string;
  logo_url: string;
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

async function getWhitelist(page: number): Promise<EmailWhitelistListResponse | null | 'forbidden'> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  const result = await fetchApi<EmailWhitelistListResponse>(`/admin/api/email-whitelist?${params}`);
  if (!result.success) {
    return result.errorCode === 'FORBIDDEN' || result.errorCode === 'ACCESS_DENIED' ? 'forbidden' : null;
  }
  return result.data;
}

async function getEntry(id: number): Promise<EmailWhitelistEntry | null> {
  const result = await fetchApi<{ item: EmailWhitelistEntry }>(`/admin/api/email-whitelist/${id}`);
  return result.success && result.data ? result.data.item || null : null;
}

async function createEntry(domain: string, signupUrl: string, logoUrl: string): Promise<EmailWhitelistEntry | null> {
  const result = await fetchApi<{ item: EmailWhitelistEntry }>('/admin/api/email-whitelist', {
    method: 'POST',
    body: JSON.stringify({ domain, signup_url: signupUrl, logo_url: logoUrl }),
  });
  return result.success && result.data ? result.data.item || null : null;
}

async function updateEntry(id: number, domain: string, signupUrl: string, logoUrl: string, isEnabled: boolean): Promise<EmailWhitelistEntry | null> {
  const result = await fetchApi<{ item: EmailWhitelistEntry }>(`/admin/api/email-whitelist/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ domain, signup_url: signupUrl, logo_url: logoUrl, is_enabled: isEnabled }),
  });
  return result.success && result.data ? result.data.item || null : null;
}

async function deleteEntry(id: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/email-whitelist/${id}`, {
    method: 'DELETE',
  });
  return result.success;
}

async function toggleEntry(id: number, isEnabled: boolean): Promise<boolean> {
  const result = await fetchApi(`/admin/api/email-whitelist/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ is_enabled: isEnabled }),
  });
  return result.success;
}

// ==================== 渲染 ====================

function renderWhitelistRow(entry: EmailWhitelistEntry): string {
  const logoCell = entry.logo_url
    ? `<img src="${escapeHtml(entry.logo_url)}" class="whitelist-logo-thumb" alt="" width="24" height="24">`
    : '<span class="whitelist-no-logo">-</span>';
  return `
    <tr data-id="${entry.id}">
      <td>${escapeHtml(entry.domain)}</td>
      <td>${logoCell}</td>
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
  
  animateTableRow({
    tableBody: whitelistTableBody,
    action: 'remove',
    rowId: entryId,
    rowIdAttr: 'data-id',
    cache: whitelistCache as DataCache<unknown>,
    cacheKey: entryId,
    colspan: 6
  });
}

async function loadWhitelist(): Promise<void> {
  console.log('[ADMIN][WHITELIST] loadWhitelist called');

  if (!whitelistTableBody) {
    console.error('[ADMIN][WHITELIST] whitelistTableBody element not found');
    return;
  }

  const items = await renderList({
    tableBody: whitelistTableBody,
    pagination: whitelistPagination,
    fetchData: async () => {
      const data = await getWhitelist(currentPage);
      if (!data || data === 'forbidden') return data;
      return { items: data.whitelist, total: data.total, page: data.page, totalPages: data.totalPages };
    },
    renderRow: renderWhitelistRow,
    bindEvents: bindWhitelistRowEvents,
    cache: whitelistCache,
    getCacheKey: (entry) => entry.id,
    colspan: 6,
    onPageChange: (newPage) => {
      currentPage = newPage;
      loadWhitelist();
    }
  });

  if (items) {
    currentEntries = items;
  }
}

const whitelistDetailSkeleton = `
  <div class="detail">
    <div class="detail-row">
      <span class="detail-label">域名</span>
      <span class="detail-value skeleton-text"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">徽标 URL</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
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
  const logoDisplay = entry.logo_url
    ? `<img src="${escapeHtml(entry.logo_url)}" class="detail-logo" alt="" style="max-width:48px;max-height:48px;">`
    : '<span class="text-muted">未设置</span>';
  return `
    <div class="detail">
      <div class="detail-row">
        <span class="detail-label">域名</span>
        <span class="detail-value">${escapeHtml(entry.domain)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">徽标 URL</span>
        <span class="detail-value">${entry.logo_url ? escapeHtml(entry.logo_url) : '<span class="text-muted">未设置</span>'}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">徽标预览</span>
        <span class="detail-value">${logoDisplay}</span>
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
  const { toggleClass, toggleText } = getToggleProps(entry.is_enabled);
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
      const success = await toggleEntry(entry.id, !entry.is_enabled);
      if (success) {
        showToast(entry.is_enabled ? '已禁用' : '已启用', 'success');
        hideModal(modal);
        updateWhitelistRow(entry.id);
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('edit-whitelist')?.addEventListener('click', () => {
    hideModal(modal);
    openFormModal(entry);
  });

  document.getElementById('delete-whitelist')?.addEventListener('click', async () => {
    showConfirm('删除白名单', `确定要删除域名 "${entry.domain}" 吗？`, async () => {
      const success = await deleteEntry(entry.id);
      if (success) {
        showToast('删除成功', 'success');
        hideModal(modal);
        removeWhitelistRow(entry.id);
      } else {
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

function openFormModal(entry?: EmailWhitelistEntry): void {
  const formTitle = document.getElementById('whitelist-form-title') as HTMLElement | null;
  const formSubmit = document.getElementById('whitelist-form-submit') as HTMLButtonElement | null;
  const domainInput = document.getElementById('whitelist-domain') as HTMLInputElement | null;
  const urlInput = document.getElementById('whitelist-signup-url') as HTMLInputElement | null;
  const logoInput = document.getElementById('whitelist-logo-url') as HTMLInputElement | null;
  const formModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;

  editingEntryId = entry ? entry.id : null;
  if (formTitle) formTitle.textContent = entry ? '编辑白名单' : '添加白名单';
  if (formSubmit) formSubmit.textContent = entry ? '保存' : '添加';
  if (domainInput) domainInput.value = entry ? entry.domain : '';
  if (urlInput) urlInput.value = entry ? entry.signup_url : '';
  if (logoInput) logoInput.value = entry ? (entry.logo_url || '') : '';
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
  const logoInput = document.getElementById('whitelist-logo-url') as HTMLInputElement | null;
  const submitBtn = document.getElementById('whitelist-form-submit') as HTMLButtonElement | null;

  if (!domainInput || !urlInput || !submitBtn) return;

  const domain = domainInput.value.trim();
  const signupUrl = urlInput.value.trim();
  const logoUrl = logoInput ? logoInput.value.trim() : '';

  if (!domain) {
    showToast('请输入域名', 'error');
    return;
  }

  if (!signupUrl) {
    showToast('请输入注册页面 URL', 'error');
    return;
  }

  submitBtn.disabled = true;

  try {
    if (editingEntryId) {
      const entryId = editingEntryId;
      const entry = currentEntries.find(e => e.id === entryId);
      await updateEntry(entryId, domain, signupUrl, logoUrl, entry?.is_enabled ?? true);
      showToast('更新成功', 'success');
      closeFormModal();
      await updateWhitelistRow(entryId);
    } else {
      const newEntry = await createEntry(domain, signupUrl, logoUrl);
      if (newEntry) {
        showToast('添加成功', 'success');
        closeFormModal();
        animateTableRow({
          tableBody: whitelistTableBody!,
          action: 'insert',
          item: newEntry,
          renderRow: renderWhitelistRow,
          bindEvents: bindWhitelistRowEvents,
          cache: whitelistCache,
          getCacheKey: (e) => e.id
        });
      } else {
        showToast('创建失败', 'error');
      }
    }
  } catch {
    showToast('操作失败', 'error');
  } finally {
    submitBtn.disabled = false;
  }
}

// ==================== 初始化 ====================

export function initWhitelistPage(): void {
  const createBtn = document.getElementById('create-whitelist-btn');
  const form = document.getElementById('whitelist-form') as HTMLFormElement | null;
  const formCancel = document.getElementById('whitelist-form-cancel');
  const formClose = document.getElementById('whitelist-form-close');
  const formModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;

  initModalCloseEvents(whitelistModal, whitelistModalClose);
  initModalCloseEvents(formModal, null);

  if (createBtn) {
    createBtn.addEventListener('click', () => openFormModal());
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
