/**
 * modules/admin/assets/js/oauth.ts
 * 管理后台 OAuth 应用管理模块
 *
 * 功能：
 * - OAuth 客户端列表（分页、搜索）
 * - 客户端详情弹窗
 * - 客户端 CRUD 操作
 * - 密钥重新生成
 * - 启用/禁用切换
 */

import {
  fetchApi,
  showToast,
  showModal,
  hideModal,
  showConfirm,
  formatDate,
  escapeHtml
} from './common';

// ==================== 类型定义 ====================

/** OAuth 客户端 */
export interface OAuthClient {
  id: number;
  client_id: string;
  name: string;
  description: string;
  redirect_uri: string;
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

/** 客户端列表响应 */
interface OAuthClientListResponse {
  clients: OAuthClient[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

/** 创建客户端响应 */
interface CreateClientResponse {
  client: OAuthClient;
  client_secret: string;
}

/** 重新生成密钥响应 */
interface RegenerateSecretResponse {
  client_secret: string;
}

// ==================== 状态 ====================

let currentPage = 1;
let currentSearch = '';
let editingClientId: number | null = null;

// ==================== DOM 元素 ====================

const oauthSearch = document.getElementById('oauth-search') as HTMLInputElement;
const oauthSearchBtn = document.getElementById('oauth-search-btn') as HTMLButtonElement;
const createOAuthBtn = document.getElementById('create-oauth-btn') as HTMLButtonElement;
const oauthTableBody = document.getElementById('oauth-table-body') as HTMLTableSectionElement;
const oauthPagination = document.getElementById('oauth-pagination') as HTMLElement;

// 弹窗元素
const oauthModal = document.getElementById('oauth-modal') as HTMLElement;
const oauthModalTitle = document.getElementById('oauth-modal-title') as HTMLElement;
const oauthModalBody = document.getElementById('oauth-modal-body') as HTMLElement;
const oauthModalFooter = document.getElementById('oauth-modal-footer') as HTMLElement;
const oauthModalClose = document.getElementById('oauth-modal-close') as HTMLButtonElement;

const oauthFormModal = document.getElementById('oauth-form-modal') as HTMLElement;
const oauthFormTitle = document.getElementById('oauth-form-title') as HTMLElement;
const oauthForm = document.getElementById('oauth-form') as HTMLFormElement;
const oauthNameInput = document.getElementById('oauth-name') as HTMLInputElement;
const oauthDescInput = document.getElementById('oauth-description') as HTMLTextAreaElement;
const oauthRedirectInput = document.getElementById('oauth-redirect-uri') as HTMLInputElement;
const oauthFormCancel = document.getElementById('oauth-form-cancel') as HTMLButtonElement;
const oauthFormSubmit = document.getElementById('oauth-form-submit') as HTMLButtonElement;
const oauthFormClose = document.getElementById('oauth-form-close') as HTMLButtonElement;

const oauthSecretModal = document.getElementById('oauth-secret-modal') as HTMLElement;
const oauthSecretValue = document.getElementById('oauth-secret-value') as HTMLElement;
const copySecretBtn = document.getElementById('copy-secret-btn') as HTMLButtonElement;
const oauthSecretOk = document.getElementById('oauth-secret-ok') as HTMLButtonElement;
const oauthSecretClose = document.getElementById('oauth-secret-close') as HTMLButtonElement;

// ==================== API ====================

async function getClients(page: number, search: string): Promise<OAuthClientListResponse | null> {
  const params = new URLSearchParams({ page: String(page), pageSize: '20' });
  if (search) params.set('search', search);

  const result = await fetchApi<OAuthClientListResponse>(`/admin/api/oauth/clients?${params}`);
  return result.success ? result.data! : null;
}

async function getClient(id: number): Promise<OAuthClient | null> {
  const result = await fetchApi<OAuthClient>(`/admin/api/oauth/clients/${id}`);
  return result.success ? result.data! : null;
}

async function createClient(name: string, description: string, redirectUri: string): Promise<CreateClientResponse | null> {
  const result = await fetchApi<CreateClientResponse>('/admin/api/oauth/clients', {
    method: 'POST',
    body: JSON.stringify({ name, description, redirect_uri: redirectUri })
  });
  return result.success ? result.data! : null;
}

async function updateClient(id: number, name: string, description: string, redirectUri: string): Promise<boolean> {
  const result = await fetchApi(`/admin/api/oauth/clients/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name, description, redirect_uri: redirectUri })
  });
  return result.success;
}

async function deleteClient(id: number): Promise<boolean> {
  const result = await fetchApi(`/admin/api/oauth/clients/${id}`, {
    method: 'DELETE'
  });
  return result.success;
}

async function regenerateSecret(id: number): Promise<string | null> {
  const result = await fetchApi<RegenerateSecretResponse>(`/admin/api/oauth/clients/${id}/regenerate-secret`, {
    method: 'POST'
  });
  return result.success ? result.data!.client_secret : null;
}

async function toggleClient(id: number, enabled: boolean): Promise<boolean> {
  const result = await fetchApi(`/admin/api/oauth/clients/${id}/toggle`, {
    method: 'POST',
    body: JSON.stringify({ enabled })
  });
  return result.success;
}


// ==================== 客户端列表 ====================

/**
 * 渲染客户端表格行
 */
function renderClientRow(client: OAuthClient): string {
  const statusClass = client.is_enabled ? 'enabled' : 'disabled';
  const statusText = client.is_enabled ? '已启用' : '已禁用';
  const toggleText = client.is_enabled ? '禁用' : '启用';
  const toggleClass = client.is_enabled ? '' : 'off';

  return `
    <tr data-client-id="${client.id}">
      <td>
        <div class="client-name">${escapeHtml(client.name)}</div>
        ${client.description ? `<div class="client-desc">${escapeHtml(client.description)}</div>` : ''}
      </td>
      <td><code class="client-id">${escapeHtml(client.client_id)}</code></td>
      <td><span class="status-badge ${statusClass}">${statusText}</span></td>
      <td>${formatDate(client.created_at)}</td>
      <td>
        <div class="action-btns">
          <button class="action-btn view" data-client-id="${client.id}">查看</button>
          <button class="action-btn toggle ${toggleClass}" data-client-id="${client.id}" data-enabled="${client.is_enabled}">${toggleText}</button>
        </div>
      </td>
    </tr>
  `;
}

/**
 * 绑定客户端行事件
 */
function bindClientRowEvents(row: HTMLTableRowElement): void {
  const viewBtn = row.querySelector('.action-btn.view') as HTMLButtonElement;
  const toggleBtn = row.querySelector('.action-btn.toggle') as HTMLButtonElement;

  viewBtn?.addEventListener('click', () => {
    const clientId = Number(viewBtn.dataset.clientId);
    showClientDetail(clientId);
  });

  toggleBtn?.addEventListener('click', async () => {
    const clientId = Number(toggleBtn.dataset.clientId);
    const currentEnabled = toggleBtn.dataset.enabled === 'true';
    const newEnabled = !currentEnabled;
    const action = newEnabled ? '启用' : '禁用';

    showConfirm('确认操作', `确定要${action}此应用吗？`, async () => {
      const success = await toggleClient(clientId, newEnabled);
      if (success) {
        showToast(`应用已${action}`, 'success');
        loadOAuthClients();
      } else {
        showToast('操作失败', 'error');
      }
    });
  });
}

/**
 * 加载客户端列表
 */
export async function loadOAuthClients(): Promise<void> {
  oauthTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载中...</td></tr>';

  const data = await getClients(currentPage, currentSearch);
  if (!data) {
    oauthTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">加载失败</td></tr>';
    return;
  }

  if (data.clients.length === 0) {
    oauthTableBody.innerHTML = '<tr><td colspan="5" class="loading-cell">暂无数据</td></tr>';
    oauthPagination.innerHTML = '';
    return;
  }

  oauthTableBody.innerHTML = data.clients.map(client => renderClientRow(client)).join('');

  oauthTableBody.querySelectorAll('tr[data-client-id]').forEach(row => {
    bindClientRowEvents(row as HTMLTableRowElement);
  });

  renderPagination(data.page, data.totalPages);
}

/**
 * 渲染分页
 */
function renderPagination(current: number, total: number): void {
  if (total <= 1) {
    oauthPagination.innerHTML = '';
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

  oauthPagination.innerHTML = html;

  oauthPagination.querySelectorAll('button[data-page]').forEach(btn => {
    btn.addEventListener('click', () => {
      const page = Number((btn as HTMLElement).dataset.page);
      if (page && page !== currentPage) {
        currentPage = page;
        loadOAuthClients();
      }
    });
  });
}

// ==================== 客户端详情 ====================

/**
 * 显示客户端详情弹窗
 */
async function showClientDetail(clientId: number): Promise<void> {
  oauthModalTitle.textContent = '应用详情';
  oauthModalBody.innerHTML = '<div class="loading-cell">加载中...</div>';
  oauthModalFooter.innerHTML = '<button class="btn btn-secondary" id="close-oauth-modal">关闭</button>';
  document.getElementById('close-oauth-modal')?.addEventListener('click', () => hideModal(oauthModal));
  showModal(oauthModal);

  const client = await getClient(clientId);
  if (!client) {
    hideModal(oauthModal);
    showToast('获取应用信息失败', 'error');
    return;
  }

  renderClientDetail(client);
}

/**
 * 渲染客户端详情
 */
function renderClientDetail(client: OAuthClient): void {
  const statusClass = client.is_enabled ? 'enabled' : 'disabled';
  const statusText = client.is_enabled ? '已启用' : '已禁用';

  oauthModalBody.innerHTML = `
    <div class="oauth-detail">
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">应用名称</span>
        <span class="oauth-detail-value">${escapeHtml(client.name)}</span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">应用描述</span>
        <span class="oauth-detail-value">${client.description ? escapeHtml(client.description) : '-'}</span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">Client ID</span>
        <span class="oauth-detail-value mono">${escapeHtml(client.client_id)}</span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">回调地址</span>
        <span class="oauth-detail-value mono">${escapeHtml(client.redirect_uri)}</span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">状态</span>
        <span class="oauth-detail-value"><span class="status-badge ${statusClass}">${statusText}</span></span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">创建时间</span>
        <span class="oauth-detail-value">${formatDate(client.created_at)}</span>
      </div>
      <div class="oauth-detail-row">
        <span class="oauth-detail-label">更新时间</span>
        <span class="oauth-detail-value">${formatDate(client.updated_at)}</span>
      </div>
    </div>
  `;

  oauthModalFooter.innerHTML = `
    <button class="btn btn-secondary" id="close-oauth-modal">关闭</button>
    <button class="btn btn-secondary" id="edit-oauth-btn" data-client-id="${client.id}">编辑</button>
    <button class="btn btn-warning" id="regenerate-secret-btn" data-client-id="${client.id}">重新生成密钥</button>
    <button class="btn btn-danger" id="delete-oauth-btn" data-client-id="${client.id}">删除</button>
  `;

  document.getElementById('close-oauth-modal')?.addEventListener('click', () => hideModal(oauthModal));

  document.getElementById('edit-oauth-btn')?.addEventListener('click', () => {
    hideModal(oauthModal);
    showEditForm(client);
  });

  document.getElementById('regenerate-secret-btn')?.addEventListener('click', () => {
    showConfirm('确认操作', '重新生成密钥后，使用旧密钥的应用将无法正常工作。确定要继续吗？', async () => {
      const newSecret = await regenerateSecret(client.id);
      if (newSecret) {
        hideModal(oauthModal);
        showSecretModal(newSecret);
        showToast('密钥已重新生成', 'success');
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('delete-oauth-btn')?.addEventListener('click', () => {
    showConfirm('确认删除', `确定要删除应用「${client.name}」吗？此操作不可恢复！`, async () => {
      const success = await deleteClient(client.id);
      if (success) {
        showToast('应用已删除', 'success');
        hideModal(oauthModal);
        loadOAuthClients();
      } else {
        showToast('删除失败', 'error');
      }
    });
  });
}


// ==================== 创建/编辑表单 ====================

/**
 * 显示创建表单
 */
function showCreateForm(): void {
  editingClientId = null;
  oauthFormTitle.textContent = '创建应用';
  oauthFormSubmit.textContent = '创建';
  oauthForm.reset();
  showModal(oauthFormModal);
}

/**
 * 显示编辑表单
 */
function showEditForm(client: OAuthClient): void {
  editingClientId = client.id;
  oauthFormTitle.textContent = '编辑应用';
  oauthFormSubmit.textContent = '保存';
  oauthNameInput.value = client.name;
  oauthDescInput.value = client.description || '';
  oauthRedirectInput.value = client.redirect_uri;
  showModal(oauthFormModal);
}

/**
 * 处理表单提交
 */
async function handleFormSubmit(): Promise<void> {
  const name = oauthNameInput.value.trim();
  const description = oauthDescInput.value.trim();
  const redirectUri = oauthRedirectInput.value.trim();

  if (!name) {
    showToast('请输入应用名称', 'error');
    return;
  }

  if (!redirectUri) {
    showToast('请输入回调地址', 'error');
    return;
  }

  // 验证 URL 格式
  try {
    new URL(redirectUri);
  } catch {
    showToast('回调地址格式不正确', 'error');
    return;
  }

  oauthFormSubmit.disabled = true;

  if (editingClientId) {
    // 编辑模式
    const success = await updateClient(editingClientId, name, description, redirectUri);
    if (success) {
      showToast('应用已更新', 'success');
      hideModal(oauthFormModal);
      loadOAuthClients();
    } else {
      showToast('更新失败', 'error');
    }
  } else {
    // 创建模式
    const result = await createClient(name, description, redirectUri);
    if (result) {
      hideModal(oauthFormModal);
      showSecretModal(result.client_secret);
      showToast('应用创建成功', 'success');
      loadOAuthClients();
    } else {
      showToast('创建失败', 'error');
    }
  }

  oauthFormSubmit.disabled = false;
}

// ==================== 密钥显示弹窗 ====================

/**
 * 显示密钥弹窗
 */
function showSecretModal(secret: string): void {
  oauthSecretValue.textContent = secret;
  showModal(oauthSecretModal);
}

/**
 * 复制密钥到剪贴板
 */
async function copySecret(): Promise<void> {
  const secret = oauthSecretValue.textContent || '';
  try {
    await navigator.clipboard.writeText(secret);
    showToast('已复制到剪贴板', 'success');
  } catch {
    // 降级方案
    const textarea = document.createElement('textarea');
    textarea.value = secret;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    textarea.select();
    document.execCommand('copy');
    document.body.removeChild(textarea);
    showToast('已复制到剪贴板', 'success');
  }
}

// ==================== 初始化 ====================

/**
 * 初始化 OAuth 管理页面
 */
export function initOAuthPage(): void {
  // 搜索
  oauthSearchBtn.addEventListener('click', () => {
    currentSearch = oauthSearch.value.trim();
    currentPage = 1;
    loadOAuthClients();
  });

  oauthSearch.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
      currentSearch = oauthSearch.value.trim();
      currentPage = 1;
      loadOAuthClients();
    }
  });

  // 创建按钮
  createOAuthBtn.addEventListener('click', showCreateForm);

  // 详情弹窗关闭
  oauthModalClose.addEventListener('click', () => hideModal(oauthModal));
  oauthModal.addEventListener('click', (e) => {
    if (e.target === oauthModal) hideModal(oauthModal);
  });

  // 表单弹窗
  oauthFormClose.addEventListener('click', () => hideModal(oauthFormModal));
  oauthFormCancel.addEventListener('click', () => hideModal(oauthFormModal));
  oauthFormModal.addEventListener('click', (e) => {
    if (e.target === oauthFormModal) hideModal(oauthFormModal);
  });

  oauthForm.addEventListener('submit', (e) => {
    e.preventDefault();
    handleFormSubmit();
  });

  oauthFormSubmit.addEventListener('click', (e) => {
    e.preventDefault();
    handleFormSubmit();
  });

  // 密钥弹窗
  oauthSecretClose.addEventListener('click', () => hideModal(oauthSecretModal));
  oauthSecretOk.addEventListener('click', () => hideModal(oauthSecretModal));
  copySecretBtn.addEventListener('click', copySecret);
}
