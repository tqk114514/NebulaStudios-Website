/**
 * modules/policy/assets/js/ai-chat.ts
 * AI 聊天组件
 * 
 * 功能：
 * - 右下角浮动气泡
 * - 聊天面板展开/收起动画
 * - 与后端 AI API 通信
 * - 工具解析与执行（高亮、跳转、邮件）
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

interface AITool {
  type: 'highlight' | 'goto' | 'mail';
  value: string;
  policy?: string; // highlight 工具的政策类型
}

// ==================== 常量 ====================

const COUNTDOWN_SECONDS = 3;
const HIGHLIGHT_DURATION = 2500; // 高亮闪烁持续时间 (ms)

// 工具正则匹配
const TOOL_PATTERNS = {
  highlight: /<highlight:([^,>]+),([^>]+)>/g, // <highlight:section_id,policy>
  goto: /<goto:([^>]+)>/g,
  mail: /<mail:([^>]+)>/g,
};

// ==================== 状态 ====================

let isOpen = false;
let isLoading = false;
let messages: ChatMessage[] = [];
let currentCountdown: { timer: number; element: HTMLElement } | null = null;

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
  
  // 添加欢迎消息（带特殊标记，方便语言切换时更新）
  addWelcomeMessage();
}

// 添加欢迎消息
function addWelcomeMessage(): void {
  if (!messagesContainer) return;
  
  const welcomeMsg = window.t?.('ai.welcome') || '您好！我是政策助手，可以帮您解答关于隐私政策、服务条款等问题。';
  
  const msgEl = document.createElement('div');
  msgEl.className = 'ai-chat-message assistant ai-chat-welcome';
  msgEl.textContent = welcomeMsg;
  
  messagesContainer.appendChild(msgEl);
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

// ==================== 工具解析 ====================

function parseTools(content: string): { cleanContent: string; tools: AITool[] } {
  const tools: AITool[] = [];
  let cleanContent = content;

  // 解析 highlight（带政策类型）
  let match;
  while ((match = TOOL_PATTERNS.highlight.exec(content)) !== null) {
    tools.push({ type: 'highlight', value: match[1], policy: match[2] });
  }
  cleanContent = cleanContent.replace(TOOL_PATTERNS.highlight, '');

  // 解析 goto
  TOOL_PATTERNS.goto.lastIndex = 0;
  while ((match = TOOL_PATTERNS.goto.exec(content)) !== null) {
    tools.push({ type: 'goto', value: match[1] });
  }
  cleanContent = cleanContent.replace(TOOL_PATTERNS.goto, '');

  // 解析 mail
  TOOL_PATTERNS.mail.lastIndex = 0;
  while ((match = TOOL_PATTERNS.mail.exec(content)) !== null) {
    tools.push({ type: 'mail', value: match[1] });
  }
  cleanContent = cleanContent.replace(TOOL_PATTERNS.mail, '');

  // 清理多余空格
  cleanContent = cleanContent.replace(/\s+/g, ' ').trim();

  return { cleanContent, tools };
}

// ==================== 工具执行 ====================

async function executeTools(tools: AITool[]): Promise<void> {
  for (const tool of tools) {
    switch (tool.type) {
      case 'highlight':
        await executeHighlight(tool.value, tool.policy);
        break;
      case 'goto':
        await executeGoto(tool.value);
        break;
      case 'mail':
        await executeMail(tool.value);
        break;
    }
  }
}

function executeHighlight(sectionId: string, policy?: string): Promise<void> {
  return new Promise((resolve) => {
    // 检查是否需要跳转到其他政策页
    const currentHash = window.location.hash.slice(1) || 'privacy';
    const targetPolicy = policy || 'privacy';
    
    if (currentHash !== targetPolicy) {
      // 需要跳转到其他政策页，先改变 hash，等页面渲染后再滚动
      window.location.hash = targetPolicy;
      // 等待页面渲染完成后再滚动高亮
      setTimeout(() => {
        scrollAndHighlight(sectionId, resolve);
      }, 300);
    } else {
      // 已在当前政策页，直接滚动高亮
      scrollAndHighlight(sectionId, resolve);
    }
  });
}

function scrollAndHighlight(sectionId: string, resolve: () => void): void {
  const section = document.getElementById(sectionId);
  if (!section) {
    console.warn(`[AI-CHAT] Section not found: ${sectionId}`);
    resolve();
    return;
  }

  // 滚动到章节
  section.scrollIntoView({ behavior: 'smooth', block: 'center' });

  // 等待滚动完成后高亮
  setTimeout(() => {
    section.classList.add('ai-highlight');
    setTimeout(() => {
      section.classList.remove('ai-highlight');
      resolve();
    }, HIGHLIGHT_DURATION);
  }, 500);
}

function executeGoto(url: string): Promise<void> {
  return new Promise((resolve) => {
    // 验证 URL 是否为本域名
    try {
      const urlObj = new URL(url, window.location.origin);
      if (urlObj.origin !== window.location.origin) {
        console.warn(`[AI-CHAT] Invalid goto URL (not same origin): ${url}`);
        resolve();
        return;
      }
    } catch {
      console.warn(`[AI-CHAT] Invalid goto URL: ${url}`);
      resolve();
      return;
    }

    showCountdown(
      window.t?.('ai.countdown.goto') || '即将跳转',
      () => { window.location.href = url; },
      resolve
    );
  });
}

function executeMail(email: string): Promise<void> {
  return new Promise((resolve) => {
    // 简单验证邮箱格式
    if (!email.includes('@')) {
      console.warn(`[AI-CHAT] Invalid email: ${email}`);
      resolve();
      return;
    }

    showCountdown(
      window.t?.('ai.countdown.mail') || '即将打开邮箱',
      () => { window.location.href = `mailto:${email}`; },
      resolve
    );
  });
}


// ==================== 倒计时提示 ====================

function showCountdown(message: string, onComplete: () => void, onFinish: () => void): void {
  // 取消之前的倒计时
  cancelCountdown();

  // 创建倒计时元素
  const countdownEl = document.createElement('div');
  countdownEl.className = 'ai-chat-countdown';
  
  let seconds = COUNTDOWN_SECONDS;
  
  const updateContent = () => {
    countdownEl.innerHTML = `
      <span class="ai-chat-countdown-text">${message} (${seconds}秒)</span>
      <button class="ai-chat-countdown-cancel">${window.t?.('ai.countdown.cancel') || '取消'}</button>
    `;
  };
  
  updateContent();
  messagesContainer?.appendChild(countdownEl);
  messagesContainer!.scrollTop = messagesContainer!.scrollHeight;

  // 绑定取消按钮
  countdownEl.querySelector('.ai-chat-countdown-cancel')?.addEventListener('click', () => {
    cancelCountdown();
    onFinish();
  });

  // 开始倒计时
  const timer = window.setInterval(() => {
    seconds--;
    if (seconds <= 0) {
      cancelCountdown();
      onComplete();
      onFinish();
    } else {
      updateContent();
      // 重新绑定取消按钮
      countdownEl.querySelector('.ai-chat-countdown-cancel')?.addEventListener('click', () => {
        cancelCountdown();
        onFinish();
      });
    }
  }, 1000);

  currentCountdown = { timer, element: countdownEl };
}

function cancelCountdown(): void {
  if (currentCountdown) {
    clearInterval(currentCountdown.timer);
    currentCountdown.element.remove();
    currentCountdown = null;
  }
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
    // 解析工具并显示干净内容
    const { cleanContent, tools } = parseTools(content);
    msgEl.textContent = cleanContent;
    
    // 异步执行工具
    if (tools.length > 0) {
      executeTools(tools);
    }
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

  // 更新欢迎消息
  const welcomeEl = messagesContainer?.querySelector('.ai-chat-welcome');
  if (welcomeEl) {
    welcomeEl.textContent = window.t?.('ai.welcome') || '您好！我是政策助手，可以帮您解答关于隐私政策、服务条款等问题。';
  }
}
