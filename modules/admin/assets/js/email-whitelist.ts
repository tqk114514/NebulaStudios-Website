/**
 * modules/admin/assets/js/email-whitelist.ts
 * 管理后台邮箱白名单管理模块
 *
 * 功能：
 * - 邮箱白名单列表展示
 * - 白名单 CRUD 操作
 * - 启用/禁用切换
 */

import {
  fetchApi,
  showToast,
  showConfirm,
  formatDate,
  escapeHtml,
} from './common';

// ==================== 类型定义 ====================

/** 邮箱白名单条目 */
export interface EmailWhitelistEntry {
  id: number;
  domain: string;
  signup_url: string;
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

/** 白名单列表响应 */
interface WhitelistResponse {
  whitelist: EmailWhitelistEntry[];
}

// ==================== DOM 元素 ====================

const whitelistTableBody = document.getElementById('whitelist-table-body') as HTMLTableSectionElement | null;

// 弹窗元素
const whitelistModal = document.getElementById('whitelist-modal') as HTMLElement | null;
const whitelistModalTitle = document.getElementById('whitelist-modal-title') as HTMLElement | null;
const whitelistModalBody = document.getElementById('whitelist-modal-body') as HTMLElement | null;
const whitelistModalFooter = document.getElementById('whitelist-modal-footer') as HTMLElement | null;
const whitelistModalClose = document.getElementById('whitelist-modal-close') as HTMLButtonElement | null;

const whitelistFormModal = document.getElementById('whitelist-form-modal') as HTMLElement | null;
const whitelistFormTitle = document.getElementById('whitelist-form-title') as HTMLElement | null;
const whitelistForm = document.getElementById('whitelist-form') as HTMLFormElement | null;
const whitelistDomainInput = document.getElementById('whitelist-domain') as HTMLInputElement | null;
const whitelistUrlInput = document.getElementById('whitelist-signup-url') as HTMLInputElement | null;
const whitelistFormCancel = document.getElementById('whitelist-form-cancel') as HTMLButtonElement | null;
const whitelistFormSubmit = document.getElementById('whitelist-form-submit') as HTMLButtonElement | null;
const whitelistFormClose = document.getElementById('whitelist-form-close') as HTMLButtonElement | null;

// ==================== 状态 ====================

let editingEntryId: number | null = null;

// ==================== API ====================

async function getWhitelist(): Promise<EmailWhitelistEntry[] | null> {
  try {
    const result = await fetchApi<WhitelistResponse>('/admin/api/email-whitelist');
    if (result.success && result.data) {
      return result.data.whitelist || [];
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

function renderWhitelist(entries: EmailWhitelistEntry[]): void {
  if (!whitelistTableBody) return;

  if (entries.length === 0) {
    whitelistTableBody.innerHTML = '<tr><td colspan="5" class="empty-cell">暂无白名单条目</td></tr>';
    return;
  }

  whitelistTableBody.innerHTML = entries.map(entry => `
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

  // 绑定事件
  bindTableEvents();
}

function bindTableEvents(): void {
  if (!whitelistTableBody) return;

  // 编辑
  whitelistTableBody.querySelectorAll('.edit-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      const entry = getEntryById(id);
      if (entry) openEditModal(entry);
    });
  });

  // 删除
  whitelistTableBody.querySelectorAll('.delete-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      confirmDelete(id);
    });
  });

  // 切换启用状态
  whitelistTableBody.querySelectorAll('.toggle-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number((btn as HTMLButtonElement).dataset.id);
      const enabled = (btn as HTMLButtonElement).dataset.enabled === 'true';
      handleToggle(id, !enabled);
    });
  });
}

let currentEntries: EmailWhitelistEntry[] = [];

function getEntryById(id: number): EmailWhitelistEntry | undefined {
  return currentEntries.find(e => e.id === id);
}

// ==================== 弹窗操作 ====================

function openCreateModal(): void {
  editingEntryId = null;
  if (whitelistFormTitle) whitelistFormTitle.textContent = '添加白名单';
  if (whitelistFormSubmit) whitelistFormSubmit.textContent = '添加';
  if (whitelistDomainInput) whitelistDomainInput.value = '';
  if (whitelistUrlInput) whitelistUrlInput.value = '';
  if (whitelistFormModal) whitelistFormModal.classList.remove('is-hidden');
}

function openEditModal(entry: EmailWhitelistEntry): void {
  editingEntryId = entry.id;
  if (whitelistFormTitle) whitelistFormTitle.textContent = '编辑白名单';
  if (whitelistFormSubmit) whitelistFormSubmit.textContent = '保存';
  if (whitelistDomainInput) whitelistDomainInput.value = entry.domain;
  if (whitelistUrlInput) whitelistUrlInput.value = entry.signup_url;
  if (whitelistFormModal) whitelistFormModal.classList.remove('is-hidden');
}

function closeFormModal(): void {
  editingEntryId = null;
  if (whitelistFormModal) whitelistFormModal.classList.add('is-hidden');
}

async function handleSubmit(e: Event): Promise<void> {
  e.preventDefault();
  if (!whitelistDomainInput || !whitelistUrlInput) return;

  const domain = whitelistDomainInput.value.trim();
  const signupUrl = whitelistUrlInput.value.trim();

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
      // 编辑模式：获取当前状态
      const entry = getEntryById(editingEntryId);
      await updateEntry(editingEntryId, domain, signupUrl, entry?.is_enabled ?? true);
      showToast('更新成功', 'success');
    } else {
      // 创建模式
      await createEntry(domain, signupUrl);
      showToast('添加成功', 'success');
    }
    closeFormModal();
    await loadWhitelist();
  } catch (e) {
    showToast('操作失败', 'error');
  }
}

async function handleToggle(id: number, isEnabled: boolean): Promise<void> {
  try {
    await toggleEntry(id, isEnabled);
    showToast(isEnabled ? '已启用' : '已禁用', 'success');
    await loadWhitelist();
  } catch (e) {
    showToast('操作失败', 'error');
  }
}

async function confirmDelete(id: number): Promise<void> {
  const entry = getEntryById(id);
  if (!entry) return;

  showConfirm(
    '删除白名单',
    `确定要删除域名 "${entry.domain}" 吗？`,
    async () => {
      try {
        await deleteEntry(id);
        showToast('删除成功', 'success');
        await loadWhitelist();
      } catch (e) {
        showToast('删除失败', 'error');
      }
    }
  );
}

// ==================== 初始化 ====================

async function loadWhitelist(): Promise<void> {
  const entries = await getWhitelist();
  if (entries !== null) {
    currentEntries = entries;
    renderWhitelist(entries);
  } else {
    if (whitelistTableBody) {
      whitelistTableBody.innerHTML = '<tr><td colspan="5" class="error-cell">加载失败</td></tr>';
    }
  }
}

export function initWhitelistPage(): void {
  console.log('[WHITELIST] Initializing whitelist page...');

  // 绑定创建按钮
  const createBtn = document.getElementById('create-whitelist-btn');
  if (createBtn) {
    createBtn.addEventListener('click', openCreateModal);
  }

  // 绑定表单提交
  if (whitelistForm) {
    whitelistForm.addEventListener('submit', handleSubmit);
  }

  // 绑定取消按钮
  if (whitelistFormCancel) {
    whitelistFormCancel.addEventListener('click', closeFormModal);
  }
  if (whitelistFormClose) {
    whitelistFormClose.addEventListener('click', closeFormModal);
  }

  // 加载数据
  loadWhitelist();
}

export { loadWhitelist };
