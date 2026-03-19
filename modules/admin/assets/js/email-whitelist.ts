/**
 * modules/admin/assets/js/email-whitelist.ts
 * 管理后台邮箱白名单管理模块
 */

import {
  fetchApi,
  showToast,
  showConfirm,
  formatDate,
  escapeHtml,
  renderPagination
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

  tableBody.innerHTML = entries.map(entry => `
    <tr data-id="${entry.id}">
      <td>${escapeHtml(entry.domain)}</td>
      <td class="url-cell" title="${escapeHtml(entry.signup_url)}">${escapeHtml(entry.signup_url)}</td>
      <td>
        <span class="status-badge ${entry.is_enabled ? 'status-enabled' : 'status-disabled'}">
          ${entry.is_enabled ? '已启用' : '已禁用'}
        </span>
      </td>
      <td>${formatDate(entry.created_at)}</td>
      <td class="action-cell">
        <button class="btn btn-sm btn-secondary toggle-btn" data-id="${entry.id}" data-enabled="${entry.is_enabled}">
          ${entry.is_enabled ? '禁用' : '启用'}
        </button>
        <button class="btn btn-sm btn-primary edit-btn" data-id="${entry.id}">编辑</button>
        <button class="btn btn-sm btn-danger delete-btn" data-id="${entry.id}">删除</button>
      </td>
    </tr>
  `).join('');

  bindTableEvents(tableBody);

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

function bindTableEvents(tableBody: HTMLTableSectionElement): void {
  tableBody.querySelectorAll('.edit-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      const entry = currentEntries.find(e => e.id === id);
      if (entry) openEditModal(entry);
    });
  });

  tableBody.querySelectorAll('.delete-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      confirmDelete(id);
    });
  });

  tableBody.querySelectorAll('.toggle-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      const enabled = (btn as HTMLButtonElement).dataset.enabled === 'true';
      handleToggle(id, !enabled);
    });
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

async function handleToggle(id: number, isEnabled: boolean): Promise<void> {
  try {
    await toggleEntry(id, isEnabled);
    showToast(isEnabled ? '已启用' : '已禁用', 'success');
    await loadWhitelist();
  } catch {
    showToast('操作失败', 'error');
  }
}

async function confirmDelete(id: number): Promise<void> {
  const entry = currentEntries.find(e => e.id === id);
  if (!entry) return;

  showConfirm(
    '删除白名单',
    `确定要删除域名 "${entry.domain}" 吗？`,
    async () => {
      try {
        await deleteEntry(id);
        showToast('删除成功', 'success');
        await loadWhitelist();
      } catch {
        showToast('删除失败', 'error');
      }
    }
  );
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
