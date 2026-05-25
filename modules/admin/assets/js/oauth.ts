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
  formatRelativeTime,
  escapeHtml,
  renderList,
  initSearch,
  copyToClipboard,
  renderStatusBadge,
  DataCache,
  updateTableRow,
  animateTableRow,
  showDetailWithCache,
  initModalCloseEvents
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
const clientsCache = new DataCache<OAuthClient>();

// ==================== DOM 元素 ====================

const oauthSearch = document.getElementById('oauth-search') as HTMLInputElement | null;
const oauthSearchBtn = document.getElementById('oauth-search-btn') as HTMLButtonElement | null;
const createOAuthBtn = document.getElementById('create-oauth-btn') as HTMLButtonElement | null;
const oauthTableBody = document.getElementById('oauth-table-body') as HTMLTableSectionElement | null;
const oauthPagination = document.getElementById('oauth-pagination') as HTMLElement | null;

// 弹窗元素
const oauthModal = document.getElementById('oauth-modal') as HTMLElement | null;
const oauthModalTitle = document.getElementById('oauth-modal-title') as HTMLElement | null;
const oauthModalBody = document.getElementById('oauth-modal-body') as HTMLElement | null;
const oauthModalFooter = document.getElementById('oauth-modal-footer') as HTMLElement | null;
const oauthModalClose = document.getElementById('oauth-modal-close') as HTMLButtonElement | null;

const oauthFormModal = document.getElementById('oauth-form-modal') as HTMLElement | null;
const oauthFormTitle = document.getElementById('oauth-form-title') as HTMLElement | null;
const oauthForm = document.getElementById('oauth-form') as HTMLFormElement | null;
const oauthNameInput = document.getElementById('oauth-name') as HTMLInputElement | null;
const oauthDescInput = document.getElementById('oauth-description') as HTMLTextAreaElement | null;
const oauthRedirectInput = document.getElementById('oauth-redirect-uri') as HTMLInputElement | null;
const oauthFormCancel = document.getElementById('oauth-form-cancel') as HTMLButtonElement | null;
const oauthFormSubmit = document.getElementById('oauth-form-submit') as HTMLButtonElement | null;
const oauthFormClose = document.getElementById('oauth-form-close') as HTMLButtonElement | null;

const oauthSecretModal = document.getElementById('oauth-secret-modal') as HTMLElement | null;
const oauthSecretValue = document.getElementById('oauth-secret-value') as HTMLElement | null;
const copySecretBtn = document.getElementById('copy-secret-btn') as HTMLButtonElement | null;
const oauthSecretOk = document.getElementById('oauth-secret-ok') as HTMLButtonElement | null;
const oauthSecretClose = document.getElementById('oauth-secret-close') as HTMLButtonElement | null;

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
  return `
    <tr data-client-id="${client.id}">
      <td>
        <div class="client-name">${escapeHtml(client.name)}</div>
        ${client.description ? `<div class="client-desc">${escapeHtml(client.description)}</div>` : ''}
      </td>
      <td><code class="client-id">${escapeHtml(client.client_id)}</code></td>
      <td>${renderStatusBadge(client.is_enabled)}</td>
      <td>${formatDate(client.created_at)}</td>
      <td>
        <div class="action-btns">
          <button class="action-btn view" data-client-id="${client.id}">查看</button>
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

  viewBtn?.addEventListener('click', () => {
    const clientId = Number(viewBtn.dataset.clientId);
    showClientDetail(clientId);
  });
}

/**
 * 更新指定客户端的表格行（重新获取数据并刷新显示）
 * @param clientId - 客户端 ID
 */
async function updateClientRow(clientId: number): Promise<void> {
  if (!oauthTableBody) return;

  await updateTableRow({
    tableBody: oauthTableBody,
    rowId: clientId,
    rowIdAttr: 'data-client-id',
    fetchData: () => getClient(clientId),
    renderRow: renderClientRow,
    bindEvents: bindClientRowEvents,
    cache: clientsCache,
    cacheKey: clientId
  });
}

async function removeClientRow(clientId: number): Promise<void> {
  if (!oauthTableBody) return;
  animateTableRow({
    tableBody: oauthTableBody,
    action: 'remove',
    rowId: clientId,
    rowIdAttr: 'data-client-id',
    cache: clientsCache as DataCache<unknown>,
    cacheKey: clientId,
    colspan: 5
  });
}

/**
 * 加载客户端列表
 */
export async function loadOAuthClients(): Promise<void> {
  console.log('[ADMIN][OAUTH] loadOAuthClients called');

  if (!oauthTableBody) {
    console.error('[ADMIN][OAUTH] oauthTableBody element not found');
    return;
  }

  await renderList({
    tableBody: oauthTableBody,
    pagination: oauthPagination,
    fetchData: async () => {
      const data = await getClients(currentPage, currentSearch);
      if (!data) return null;
      return { items: data.clients, total: data.total, page: data.page, totalPages: data.totalPages };
    },
    renderRow: renderClientRow,
    bindEvents: bindClientRowEvents,
    cache: clientsCache,
    getCacheKey: (client) => client.id,
    colspan: 5,
    onPageChange: (page) => {
      currentPage = page;
      loadOAuthClients();
    }
  });
}

// ==================== 客户端详情 ====================

const clientDetailSkeleton = `
  <div class="detail">
    <div class="detail-row">
      <span class="detail-label">应用名称</span>
      <span class="detail-value skeleton-text"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">应用描述</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">Client ID</span>
      <span class="detail-value skeleton-text skeleton-wide"></span>
    </div>
    <div class="detail-row">
      <span class="detail-label">回调地址</span>
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

function renderClientDetailContent(client: OAuthClient, cachedAt?: number, isRefreshing?: boolean): string {
  return `
    <div class="detail">
      <div class="detail-row">
        <span class="detail-label">应用名称</span>
        <span class="detail-value">${escapeHtml(client.name)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">应用描述</span>
        <span class="detail-value">${client.description ? escapeHtml(client.description) : '-'}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">Client ID</span>
        <span class="detail-value mono">${escapeHtml(client.client_id)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">回调地址</span>
        <span class="detail-value mono">${escapeHtml(client.redirect_uri)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">状态</span>
        <span class="detail-value">${renderStatusBadge(client.is_enabled)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">创建时间</span>
        <span class="detail-value">${formatDate(client.created_at)}</span>
      </div>
      <div class="detail-row">
        <span class="detail-label">更新时间</span>
        <span class="detail-value">${formatDate(client.updated_at)}</span>
      </div>
    </div>
    <div class="detail-meta" id="oauth-detail-meta">
      ${cachedAt ? `数据更新于 ${formatRelativeTime(cachedAt)}` : ''}${isRefreshing ? ' · 刷新中...' : ''}
    </div>
  `;
}

function renderClientDetailFooter(client: OAuthClient): string {
  const toggleClass = client.is_enabled ? 'btn-warning' : 'btn-success';
  const toggleText = client.is_enabled ? '禁用' : '启用';
  return `
    <button class="btn btn-secondary" data-close-modal>关闭</button>
    <button class="btn ${toggleClass}" id="toggle-oauth-btn" data-id="${client.id}">${toggleText}</button>
    <button class="btn btn-secondary" id="edit-oauth-btn" data-client-id="${client.id}">编辑</button>
    <button class="btn btn-warning" id="regenerate-secret-btn" data-client-id="${client.id}">重新生成密钥</button>
    <button class="btn btn-danger" id="delete-oauth-btn" data-client-id="${client.id}">删除</button>
  `;
}

function bindClientDetailEvents(client: OAuthClient, modal: HTMLElement): void {
  modal.querySelector('[data-close-modal]')?.addEventListener('click', () => hideModal(modal));

  document.getElementById('toggle-oauth-btn')?.addEventListener('click', async () => {
    showConfirm(client.is_enabled ? '禁用应用' : '启用应用', `确定要${client.is_enabled ? '禁用' : '启用'}应用「${client.name}」吗？`, async () => {
      const success = await toggleClient(client.id, !client.is_enabled);
      if (success) {
        showToast(client.is_enabled ? '应用已禁用' : '应用已启用', 'success');
        hideModal(modal);
        updateClientRow(client.id);
      } else {
        showToast('操作失败', 'error');
      }
    });
  });

  document.getElementById('edit-oauth-btn')?.addEventListener('click', () => {
    hideModal(modal);
    showEditForm(client);
  });

  document.getElementById('regenerate-secret-btn')?.addEventListener('click', () => {
    showConfirm('确认操作', '重新生成密钥后，使用旧密钥的应用将无法正常工作。确定要继续吗？', async () => {
      const newSecret = await regenerateSecret(client.id);
      if (newSecret) {
        hideModal(modal);
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
        hideModal(modal);
        removeClientRow(client.id);
      } else {
        showToast('删除失败', 'error');
      }
    });
  });
}

function showClientDetail(clientId: number): void {
  console.log('[ADMIN][OAUTH] showClientDetail called');

  if (oauthModalTitle) {
    oauthModalTitle.textContent = '应用详情';
  }

  showDetailWithCache<OAuthClient>({
    modal: oauthModal,
    modalBody: oauthModalBody,
    modalFooter: oauthModalFooter,
    cache: clientsCache,
    cacheKey: clientId,
    fetchData: () => getClient(clientId),
    skeletonHtml: clientDetailSkeleton,
    renderContent: renderClientDetailContent,
    renderFooter: renderClientDetailFooter,
    bindFooterEvents: bindClientDetailEvents
  });
}


// ==================== 创建/编辑表单 ====================

/**
 * 显示创建表单
 */
function showCreateForm(): void {
  console.log('[ADMIN][OAUTH] showCreateForm called');
  
  const localOauthFormTitle = oauthFormTitle;
  const localOauthFormSubmit = oauthFormSubmit;
  const localOauthForm = oauthForm;
  const localOauthFormModal = oauthFormModal;
  
  if (!localOauthFormTitle || !localOauthFormSubmit || !localOauthForm || !localOauthFormModal) {
    console.error('[ADMIN][OAUTH] Form elements not found for showCreateForm');
    return;
  }
  
  editingClientId = null;
  localOauthFormTitle.textContent = '创建应用';
  localOauthFormSubmit.textContent = '创建';
  localOauthForm.reset();
  showModal(localOauthFormModal);
}

/**
 * 显示编辑表单
 */
function showEditForm(client: OAuthClient): void {
  console.log('[ADMIN][OAUTH] showEditForm called');
  
  const localOauthFormTitle = oauthFormTitle;
  const localOauthFormSubmit = oauthFormSubmit;
  const localOauthNameInput = oauthNameInput;
  const localOauthDescInput = oauthDescInput;
  const localOauthRedirectInput = oauthRedirectInput;
  const localOauthFormModal = oauthFormModal;
  
  if (!localOauthFormTitle || !localOauthFormSubmit || !localOauthNameInput || !localOauthDescInput || !localOauthRedirectInput || !localOauthFormModal) {
    console.error('[ADMIN][OAUTH] Form elements not found for showEditForm');
    return;
  }
  
  editingClientId = client.id;
  localOauthFormTitle.textContent = '编辑应用';
  localOauthFormSubmit.textContent = '保存';
  localOauthNameInput.value = client.name;
  localOauthDescInput.value = client.description || '';
  localOauthRedirectInput.value = client.redirect_uri;
  showModal(localOauthFormModal);
}

/**
 * 处理表单提交
 */
async function handleFormSubmit(): Promise<void> {
  console.log('[ADMIN][OAUTH] handleFormSubmit called');
  
  const localOauthNameInput = oauthNameInput;
  const localOauthDescInput = oauthDescInput;
  const localOauthRedirectInput = oauthRedirectInput;
  const localOauthFormSubmit = oauthFormSubmit;
  
  if (!localOauthNameInput || !localOauthDescInput || !localOauthRedirectInput || !localOauthFormSubmit) {
    console.error('[ADMIN][OAUTH] Form elements not found for handleFormSubmit');
    return;
  }
  
  const name = localOauthNameInput.value.trim();
  const description = localOauthDescInput.value.trim();
  const redirectUri = localOauthRedirectInput.value.trim();

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

  localOauthFormSubmit.disabled = true;

  if (editingClientId) {
    // 编辑模式
    const success = await updateClient(editingClientId, name, description, redirectUri);
    if (success) {
      showToast('应用已更新', 'success');
      hideModal(oauthFormModal);
      await updateClientRow(editingClientId);
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
      animateTableRow({
        tableBody: oauthTableBody!,
        action: 'insert',
        item: result.client,
        renderRow: renderClientRow,
        bindEvents: bindClientRowEvents,
        cache: clientsCache as DataCache<unknown>,
        getCacheKey: (c) => c.id
      });
    } else {
      showToast('创建失败', 'error');
    }
  }

  localOauthFormSubmit.disabled = false;
}

// ==================== 密钥显示弹窗 ====================

/**
 * 显示密钥弹窗
 */
function showSecretModal(secret: string): void {
  console.log('[ADMIN][OAUTH] showSecretModal called');
  
  const localOauthSecretValue = oauthSecretValue;
  const localOauthSecretModal = oauthSecretModal;
  
  if (!localOauthSecretValue || !localOauthSecretModal) {
    console.error('[ADMIN][OAUTH] Secret modal elements not found');
    return;
  }
  
  localOauthSecretValue.textContent = secret;
  showModal(localOauthSecretModal);
}

/**
 * 复制密钥到剪贴板
 */
async function copySecret(): Promise<void> {
  if (!oauthSecretValue) return;
  
  const secret = oauthSecretValue.textContent || '';
  const success = await copyToClipboard(secret);
  if (success) {
    showToast('已复制到剪贴板', 'success');
  }
}

// ==================== 初始化 ====================

/**
 * 初始化 OAuth 管理页面
 */
export function initOAuthPage(): void {
  console.log('[ADMIN][OAUTH] initOAuthPage called');

  if (oauthSearchBtn && oauthSearch) {
    initSearch(oauthSearch, oauthSearchBtn, (query) => {
      currentSearch = query;
      currentPage = 1;
      loadOAuthClients();
    });
  }

  if (createOAuthBtn) {
    createOAuthBtn.addEventListener('click', showCreateForm);
  }

  initModalCloseEvents(oauthModal, oauthModalClose);
  initModalCloseEvents(oauthFormModal, null);

  if (oauthFormClose) {
    oauthFormClose.addEventListener('click', () => hideModal(oauthFormModal));
  }
  if (oauthFormCancel) {
    oauthFormCancel.addEventListener('click', () => hideModal(oauthFormModal));
  }

  if (oauthForm && oauthFormSubmit) {
    oauthForm.addEventListener('submit', (e) => {
      e.preventDefault();
      handleFormSubmit();
    });
    oauthFormSubmit.addEventListener('click', (e) => {
      e.preventDefault();
      handleFormSubmit();
    });
  }

  if (oauthSecretClose && oauthSecretOk && copySecretBtn && oauthSecretModal) {
    oauthSecretClose.addEventListener('click', () => hideModal(oauthSecretModal));
    oauthSecretOk.addEventListener('click', () => hideModal(oauthSecretModal));
    copySecretBtn.addEventListener('click', copySecret);
  }
}
