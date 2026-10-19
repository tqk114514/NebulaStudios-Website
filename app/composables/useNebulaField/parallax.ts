export interface Parallax {
  onPointerMove: (event: PointerEvent) => void
  /** 向目标值缓动一步，返回本帧应使用的视差角 */
  advance: () => { x: number; y: number }
}

/**
 * 鼠标视差。画布不接管指针事件，故由调用方把 window 上的事件转给 onPointerMove；
 * 落在 hero 之外的移动不更新目标值，雕塑因此停在最后指向的姿态。
 */
export function createParallax(getRect: () => DOMRect | null): Parallax {
  let targetX = 0
  let targetY = 0
  let currentX = 0
  let currentY = 0

  return {
    onPointerMove(event) {
      const rect = getRect()
      if (!rect) return
      if (event.clientX < rect.left || event.clientX > rect.right) return
      if (event.clientY < rect.top || event.clientY > rect.bottom) return
      targetX = ((event.clientX - (rect.left + rect.width / 2)) / rect.width) * 0.5
      targetY = ((event.clientY - (rect.top + rect.height / 2)) / rect.height) * 0.3
    },
    advance() {
      currentX += (targetX - currentX) * 0.05
      currentY += (targetY - currentY) * 0.05
      return { x: currentX, y: currentY }
    },
  }
}
