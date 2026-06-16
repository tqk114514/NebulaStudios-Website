/**
 * modules/admin/assets/js/data.ts
 * 管理后台 - 数据导入导出
 *
 * 权限：仅限超级管理员（role >= 2）
 */

import { showModal, hideModal, showToast } from './common';

let exportRequestId = '';
let exportTimer: ReturnType<typeof setInterval> | null = null;
let exportExpiresAt = 0;
let eventsBound = false;

export function initDataPage(): void {
  const pageEl = document.getElementById('page-data');
  if (!pageEl) {
    console.warn('[ADMIN][DATA] page-data element not found');
    return;
  }

  pageEl.innerHTML = renderDataPage();
  bindEvents();
}

function renderDataPage(): string {
  return `
    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-card-header">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
            <path d="M5 20h14v-2H5v2zm0-10h4v6h6v-6h4l-7-7-7 7z"/>
          </svg>
        </div>
        <p class="stat-card-desc">导出 users 表和 user_logs 表为加密备份文件</p>
        <button type="button" id="data-export-btn" class="btn btn-primary">导出数据</button>
      </div>
      <div class="stat-card">
        <div class="stat-card-header">
          <svg viewBox="0 0 24 24" width="24" height="24" fill="currentColor">
            <path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/>
          </svg>
        </div>
        <p class="stat-card-desc">从 .enc 加密备份文件恢复数据</p>
        <button type="button" id="data-import-btn" class="btn btn-secondary">选择备份文件</button>
      </div>
    </div>
  `;
}

function bindEvents(): void {
  const exportBtn = document.getElementById('data-export-btn');
  const importBtn = document.getElementById('data-import-btn');

  // 导出
  exportBtn?.addEventListener('click', async () => {
    if (exportBtn instanceof HTMLButtonElement) exportBtn.disabled = true;

    try {
      const resp = await fetch('/admin/api/data/export/request', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' }
      });

      const data = await resp.json();
      if (!data.success) {
        showToast('导出请求失败', 'error');
        return;
      }

      exportRequestId = data.requestId;
      exportExpiresAt = Date.now() + data.expiresIn * 1000;

      showExportAuthModal();
    } catch {
      showToast('网络错误', 'error');
    } finally {
      if (exportBtn instanceof HTMLButtonElement) exportBtn.disabled = false;
    }
  });

  // 导入
  importBtn?.addEventListener('click', () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.enc';
    input.addEventListener('change', () => {
      const file = input.files?.[0];
      if (!file) return;

      if (!file.name.endsWith('.enc')) {
        showToast('仅支持 .enc 格式的备份文件', 'error');
        return;
      }

      handleImportPreview(file);
    });
    input.click();
  });

  // 弹窗事件仅绑定一次，避免导航切换时重复叠加监听器
  if (eventsBound) return;
  eventsBound = true;

  // OTAC 授权弹窗
  bindExportAuthEvents();
  // 导入预览弹窗
  bindImportPreviewEvents();
}

// ==================== 导出授权弹窗 ====================

function handleOatcInput(input: HTMLInputElement, downloadBtn: HTMLButtonElement | null): () => void {
  return () => {
    if (downloadBtn) {
      downloadBtn.disabled = (input.value.length < 16);
    }
  };
}

function showExportAuthModal(): void {
  const modal = document.getElementById('export-auth-modal');
  const input = document.getElementById('export-otac-input') as HTMLInputElement | null;
  const timer = document.getElementById('export-otac-timer');
  const downloadBtn = document.getElementById('export-auth-download') as HTMLButtonElement | null;

  if (!modal) return;
  if (input) { input.value = ''; input.oninput = null; }
  if (downloadBtn) downloadBtn.disabled = true;

  showModal(modal);

  input?.focus();

  const updateTimer = () => {
    const remaining = Math.max(0, Math.floor((exportExpiresAt - Date.now()) / 1000));
    if (timer) {
      const mins = Math.floor(remaining / 60);
      const secs = remaining % 60;
      timer.textContent = `剩余时间: ${mins}:${String(secs).padStart(2, '0')}`;
    }
    if (remaining <= 0) {
      stopTimer();
      hideModal(modal);
      showToast('授权码已过期，请重新生成', 'error');
    }
  };

  updateTimer();
  exportTimer = setInterval(updateTimer, 1000);

  if (input) {
    input.oninput = handleOatcInput(input, downloadBtn);
  }
}

function stopTimer(): void {
  if (exportTimer) {
    clearInterval(exportTimer);
    exportTimer = null;
  }
}

function bindExportAuthEvents(): void {
  const modal = document.getElementById('export-auth-modal');
  const closeBtn = document.getElementById('export-auth-close');
  const cancelBtn = document.getElementById('export-auth-cancel');
  const downloadBtn = document.getElementById('export-auth-download') as HTMLButtonElement | null;
  const input = document.getElementById('export-otac-input') as HTMLInputElement | null;

  closeBtn?.addEventListener('click', () => {
    stopTimer();
    hideModal(modal!);
    fetch('/admin/api/data/one-time-access-code', { method: 'DELETE', credentials: 'include' }).catch(() => {});
  });

  cancelBtn?.addEventListener('click', () => {
    stopTimer();
    hideModal(modal!);
    fetch('/admin/api/data/one-time-access-code', { method: 'DELETE', credentials: 'include' }).catch(() => {});
  });

  downloadBtn?.addEventListener('click', async () => {
    const otac = input?.value.trim() || '';
    if (otac.length < 16) return;

    if (downloadBtn instanceof HTMLButtonElement) downloadBtn.disabled = true;

    try {
      const resp = await fetch(`/admin/api/data/export/${encodeURIComponent(exportRequestId)}/download?otac=${encodeURIComponent(otac)}`, {
        credentials: 'include'
      });

      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        const errorCode = data.errorCode || 'UNKNOWN';
        if (errorCode === 'OTAC_MAX_TRIES') {
          showToast('授权码错误次数过多，已失效，请重新生成', 'error');
          stopTimer();
          hideModal(modal!);
        } else if (errorCode.startsWith('OTAC')) {
          showToast('授权码错误', 'error');
          if (input) { input.value = ''; input.focus(); }
          if (downloadBtn instanceof HTMLButtonElement) downloadBtn.disabled = true;
        } else {
          showToast(`导出失败: ${data.message || errorCode}`, 'error');
          stopTimer();
          hideModal(modal!);
        }
        return;
      }

      const blob = await resp.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;

      const disposition = resp.headers.get('content-disposition') || '';
      const match = disposition.match(/filename="([^"]+)"/);
      a.download = match ? match[1] : 'nebula-backup.enc';

      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);

      showToast('导出成功', 'success');
      stopTimer();
      hideModal(modal!);
    } catch {
      showToast('下载失败', 'error');
    } finally {
      if (downloadBtn instanceof HTMLButtonElement) downloadBtn.disabled = false;
    }
  });
}

// ==================== 导入预览弹窗 ====================

let importFileToken = '';

async function handleImportPreview(file: File): Promise<void> {
  const formData = new FormData();
  formData.append('file', file);

  try {
    const resp = await fetch('/admin/api/data/import/preview', {
      method: 'POST',
      credentials: 'include',
      body: formData
    });

    const data = await resp.json();
    if (!data.success) {
      showToast('文件格式不正确', 'error');
      return;
    }

    importFileToken = data.fileToken;

    const usersEl = document.getElementById('import-preview-users');
    const logsEl = document.getElementById('import-preview-logs');
    const timeEl = document.getElementById('import-preview-time');

    if (usersEl) usersEl.textContent = String(data.usersCount);
    if (logsEl) logsEl.textContent = String(data.logsCount);
    if (timeEl) timeEl.textContent = data.exportedAt;

    showModal(document.getElementById('import-preview-modal')!);
  } catch {
    showToast('网络错误', 'error');
  }
}

function bindImportPreviewEvents(): void {
  const modal = document.getElementById('import-preview-modal');
  const closeBtn = document.getElementById('import-preview-close');
  const cancelBtn = document.getElementById('import-preview-cancel');
  const confirmBtn = document.getElementById('import-preview-confirm') as HTMLButtonElement | null;

  closeBtn?.addEventListener('click', () => hideModal(modal!));
  cancelBtn?.addEventListener('click', () => hideModal(modal!));

  confirmBtn?.addEventListener('click', async () => {
    if (confirmBtn instanceof HTMLButtonElement) confirmBtn.disabled = true;

    try {
      const strategyRadio = document.querySelector<HTMLInputElement>('input[name="import-strategy"]:checked');
      const strategy = strategyRadio?.value || 'merge';

      const resp = await fetch('/admin/api/data/import/execute', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fileToken: importFileToken, strategy })
      });

      const data = await resp.json();
      if (!data.success) {
        showToast('导入失败: 文件已损坏或被篡改', 'error');
        hideModal(modal!);
        return;
      }

      const passwordSkipped = Number(data.usersPasswordSkipped) || 0;
      const roleDowngraded = Number(data.usersRoleDowngraded) || 0;
      if (passwordSkipped > 0 || roleDowngraded > 0) {
        const anomalies: string[] = [];
        if (passwordSkipped > 0) anomalies.push(`${passwordSkipped} 个用户因密码哈希不合法被跳过`);
        if (roleDowngraded > 0) anomalies.push(`${roleDowngraded} 个用户因 role 非法被降级为普通用户`);
        showToast(`导入完成: 用户 ${data.usersImported} 条, 日志 ${data.logsImported} 条；${anomalies.join('，')}（疑似备份篡改）`, 'warning');
      } else {
        showToast(`导入成功: 用户 ${data.usersImported} 条, 日志 ${data.logsImported} 条`, 'success');
      }
      hideModal(modal!);
    } catch {
      showToast('网络错误', 'error');
    } finally {
      if (confirmBtn instanceof HTMLButtonElement) confirmBtn.disabled = false;
    }
  });
}