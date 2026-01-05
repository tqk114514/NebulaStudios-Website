/**
 * modules/policy/assets/js/ai-chat.ts
 * AI 聊天组件
 * 
 * 功能：
 * - 右下角浮动气泡
 * - 聊天面板展开/收起动画
 * - 与后端 AI API 通信
 * - 多语言支持
 */

// ==================== 类型定义 ====================

interface ChatMessage {
  role: 'user' | 'assistant';
  content: string;
}

interface AIChatResponse {
  content?: string;
  error?: string;
}

// ==================== 状态 ====================

let isOpen = false;
let isLoading = false;
let messages: ChatMessage[] = [];

// ==================== DOM 元素 ====================

let bubble: HTMLButtonElement | null = null;
let panel: HTMLDivElement | null = null;
let messagesContainer: HTMLDivElement | null = null;
let input: HTMLInputElement | null = null;
let sendBtn: HTMLButtonElement | null = null;

// ==================== 初始化 ====================

export function initAIChat(): void {
  createChatUI();
  bindEvents();
  
  // 添加欢迎消息
  const welcomeMsg = window.t?.('ai.welcome') || '您好！我是政策助手，可以帮您解答关于隐私政策、服务条款等问题。';
  addMessage('assistant', welcomeMsg);
}

// ==================== UI 创建 ====================

function createChatUI(): void {
  // 创建气泡按钮
  bubble = document.createElement('button');
  bubble.className = 'ai-chat-bubble';
  bubble.setAttribute('aria-label', window.t?.('ai.title') || 'AI 助手');
  bubble.innerHTML = `
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
    </svg>
  `;
  document.body.appendChild(bubble);

  // 创建聊天面板
  panel = document.createElement('div');
  panel.className = 'ai-chat-panel';
  panel.innerHTML = `
    <div class="ai-chat-header">
      <h3>${window.t?.('ai.title') || 'AI 助手'}</h3>
      <button class="ai-chat-close" aria-label="${window.t?.('ai.close') || '关闭'}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/>
          <line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>
    <div class="ai-chat-disclaimer">${window.t?.('ai.disclaimer') || 'AI 回答仅供参考，不构成法律建议。'}</div>
    <div class="ai-chat-messages"></div>
    <div class="ai-chat-input-area">
      <input type="text" class="ai-chat-input" placeholder="${window.t?.('ai.placeholder') || '输入您的问题...'}" />
      <button class="ai-chat-send" aria-label="${window.t?.('ai.send') || '发送'}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="22" y1="2" x2="11" y2="13"/>
          <polygon points="22 2 15 22 11 13 2 9 22 2"/>
        </svg>
      </button>
    </div>
  `;
  document.body.appendChild(panel);

  // 获取子元素引用
  messagesContainer = panel.querySelector('.ai-chat-messages');
  input = panel.querySelector('.ai-chat-input');
  sendBtn = panel.querySelector('.ai-chat-send');
}

// ==================== 事件绑定 ====================

function bindEvents(): void {
  // 气泡点击
  bubble?.addEventListener('click', togglePanel);

  // 关闭按钮
  panel?.querySelector('.ai-chat-close')?.addEventListener('click', closePanel);

  // 发送按钮
  sendBtn?.addEventListener('click', sendMessage);

  // 输入框回车
  input?.addEventListener('keypress', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });

  // 点击面板外部关闭
  document.addEventListener('click', (e) => {
    if (isOpen && panel && bubble) {
      const target = e.target as Node;
      if (!panel.contains(target) && !bubble.contains(target)) {
        closePanel();
      }
    }
  });
}

// ==================== 面板控制 ====================

function togglePanel(): void {
  if (isOpen) {
    closePanel();
  } else {
    openPanel();
  }
}

function openPanel(): void {
  isOpen = true;
  panel?.classList.add('open');
  bubble?.classList.add('hidden');
  input?.focus();
}

function closePanel(): void {
  isOpen = false;
  panel?.classList.remove('open');
  bubble?.classList.remove('hidden');
}

// ==================== 消息处理 ====================

function addMessage(role: 'user' | 'assistant', content: string, isThinking = false, isError = false): HTMLDivElement | null {
  if (!messagesContainer) return null;

  const msgEl = document.createElement('div');
  msgEl.className = `ai-chat-message ${role}`;
  
  if (isThinking) {
    msgEl.classList.add('thinking');
    msgEl.innerHTML = `
      <span>${window.t?.('ai.thinking') || '正在思考'}</span>
      <div class="ai-chat-thinking-dots">
        <span></span><span></span><span></span>
      </div>
    `;
  } else if (isError) {
    msgEl.classList.add('error');
    msgEl.textContent = content;
  } else {
    msgEl.textContent = content;
  }

  messagesContainer.appendChild(msgEl);
  messagesContainer.scrollTop = messagesContainer.scrollHeight;

  if (!isThinking) {
    messages.push({ role, content });
  }

  return msgEl;
}

function removeThinkingMessage(): void {
  const thinking = messagesContainer?.querySelector('.ai-chat-message.thinking');
  thinking?.remove();
}

// ==================== API 通信 ====================

async function sendMessage(): Promise<void> {
  if (!input || isLoading) return;

  const content = input.value.trim();
  if (!content) return;

  // 清空输入框
  input.value = '';

  // 添加用户消息
  addMessage('user', content);

  // 显示思考状态
  isLoading = true;
  updateSendButton();
  addMessage('assistant', '', true);

  try {
    const response = await fetch('/api/ai/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        messages: messages.map(m => ({ role: m.role, content: m.content }))
      })
    });

    const data: AIChatResponse = await response.json();

    removeThinkingMessage();

    if (data.error) {
      addMessage('assistant', data.error, false, true);
    } else if (data.content) {
      addMessage('assistant', data.content);
    } else {
      addMessage('assistant', window.t?.('ai.error') || '抱歉，出现了一些问题。', false, true);
    }
  } catch {
    removeThinkingMessage();
    addMessage('assistant', window.t?.('ai.error') || '抱歉，出现了一些问题。', false, true);
  } finally {
    isLoading = false;
    updateSendButton();
  }
}

function updateSendButton(): void {
  if (sendBtn) {
    sendBtn.disabled = isLoading;
  }
}

// ==================== 语言更新 ====================

export function updateAIChatLanguage(): void {
  if (!panel || !bubble) return;

  // 更新气泡
  bubble.setAttribute('aria-label', window.t?.('ai.title') || 'AI 助手');

  // 更新面板标题
  const header = panel.querySelector('.ai-chat-header h3');
  if (header) header.textContent = window.t?.('ai.title') || 'AI 助手';

  // 更新关闭按钮
  const closeBtn = panel.querySelector('.ai-chat-close');
  if (closeBtn) closeBtn.setAttribute('aria-label', window.t?.('ai.close') || '关闭');

  // 更新免责声明
  const disclaimer = panel.querySelector('.ai-chat-disclaimer');
  if (disclaimer) disclaimer.textContent = window.t?.('ai.disclaimer') || 'AI 回答仅供参考，不构成法律建议。';

  // 更新输入框
  if (input) input.placeholder = window.t?.('ai.placeholder') || '输入您的问题...';

  // 更新发送按钮
  if (sendBtn) sendBtn.setAttribute('aria-label', window.t?.('ai.send') || '发送');
}
