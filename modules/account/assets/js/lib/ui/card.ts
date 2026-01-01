/**
 * 卡片 UI 模块
 *
 * 功能：
 * - 卡片高度动态调整
 * - 卡片内容自动高度监听
 * - 错误提示高度调整
 */

// ==================== 类型定义 ====================

/** Observer 存储结构 */
interface CardObservers {
  resizeObserver: ResizeObserver;
  mutationObserver: MutationObserver;
}

// ==================== 卡片高度调整 ====================

/**
 * 动态调整卡片高度（带动画）
 */
export function adjustCardHeight(
  card: HTMLElement | null,
  loginView?: HTMLElement | null,
  registerView?: HTMLElement | null
): void {
  if (!card) return;

  if (!loginView && !registerView) {
    // 单视图模式：自动计算高度
    const currentHeight = card.offsetHeight;
    card.style.transition = 'none';
    card.style.height = 'auto';
    const targetHeight = card.scrollHeight;
    card.style.height = `${currentHeight}px`;
    card.offsetHeight; // 强制重绘
    card.style.transition = 'height 0.3s ease';
    card.style.height = `${targetHeight}px`;
  } else {
    // 双视图模式：根据可见视图计算
    if (!loginView || !registerView) return;
    const visibleView = registerView.classList.contains('is-hidden') ? loginView : registerView;
    card.style.transition = 'height 0.3s ease';
    card.style.height = `${visibleView.scrollHeight}px`;
  }
}

/** ResizeObserver 实例存储 */
const cardObservers = new WeakMap<HTMLElement, CardObservers>();

/**
 * 启用卡片内容自动高度调整
 */
export function enableCardAutoResize(card: HTMLElement | null): () => void {
  if (!card || cardObservers.has(card)) return () => {};

  if (typeof ResizeObserver === 'undefined' || typeof MutationObserver === 'undefined') {
    console.warn('[UI] ResizeObserver or MutationObserver not supported');
    return () => {};
  }

  let resizeTimeout: ReturnType<typeof setTimeout> | null = null;
  const debouncedAdjust = (): void => {
    if (resizeTimeout) clearTimeout(resizeTimeout);
    resizeTimeout = setTimeout(() => {
      adjustCardHeight(card);
    }, 50);
  };

  const observer = new ResizeObserver((entries) => {
    for (const entry of entries) {
      if (entry.target !== card) {
        debouncedAdjust();
      }
    }
  });

  const observeChildren = (): void => {
    card.querySelectorAll('*').forEach(child => {
      observer.observe(child);
    });
  };

  observeChildren();

  const mutationObserver = new MutationObserver((mutations) => {
    let shouldReobserve = false;
    for (const mutation of mutations) {
      if (mutation.type === 'childList' && mutation.addedNodes.length > 0) {
        shouldReobserve = true;
        break;
      }
    }
    if (shouldReobserve) {
      observeChildren();
      debouncedAdjust();
    }
  });

  mutationObserver.observe(card, { childList: true, subtree: true });

  cardObservers.set(card, { resizeObserver: observer, mutationObserver });

  return () => disableCardAutoResize(card);
}

/**
 * 禁用卡片内容自动高度调整
 */
export function disableCardAutoResize(card: HTMLElement | null): void {
  if (!card) return;

  const observers = cardObservers.get(card);
  if (observers) {
    observers.resizeObserver?.disconnect();
    observers.mutationObserver?.disconnect();
    cardObservers.delete(card);
  }
}

/**
 * 显示/隐藏错误提示并调整卡片高度
 */
export function toggleErrorMessage(
  show: boolean,
  baseHeight: number,
  card: HTMLElement | null
): void {
  const ERROR_HEIGHT = 38;
  if (card) {
    card.style.transition = 'height 0.3s ease';
    card.style.height = show ? `${baseHeight + ERROR_HEIGHT}px` : `${baseHeight}px`;
  }
}

/**
 * 平滑调整元素高度
 */
export function smoothAdjustHeight(element: HTMLElement, targetHeight: number): void {
  element.style.transition = 'height 0.3s ease';
  element.style.height = `${targetHeight}px`;
}

/**
 * 延迟执行函数（确保 DOM 渲染完成）
 */
export function delayedExecution(callback: () => void): void {
  requestAnimationFrame(() => requestAnimationFrame(callback));
}

/**
 * 带条件的延迟执行
 */
export function conditionalDelayedExecution(
  callback: () => void,
  condition?: () => boolean
): void {
  requestAnimationFrame(() => {
    if (!condition || condition()) callback();
  });
}
