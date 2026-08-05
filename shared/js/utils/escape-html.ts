/**
 * shared/js/utils/escape-html.ts
 * HTML 转义工具，防止 XSS
 */

/**
 * 转义 HTML 特殊字符，防止 XSS
 *
 * 同时转义引号（" '），因此既可用于文本节点上下文，也可用于属性值上下文
 * （如 href="...${escapeHtml(v)}"）。
 */
export function escapeHtml(str: string): string {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
